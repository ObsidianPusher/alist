package task

import (
	"sort"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/xhofe/tache"
)

type DoneTaskCleanupConfig struct {
	Name      string
	Interval  time.Duration
	Retention time.Duration
	MaxDone   int
}

const (
	defaultDoneTaskCleanupInterval = 5 * time.Minute
	defaultDoneTaskRetention       = 6 * time.Hour
	defaultDoneTaskMaxCount        = 2000
)

var (
	doneTaskCleanupMu        sync.Mutex
	doneTaskCleanupCallbacks []func()
	doneTaskCleanupOnce      sync.Once
	doneTaskCleanupCh        = make(chan struct{}, 1)
)

func normalizeDoneTaskCleanupConfig(cfg DoneTaskCleanupConfig) DoneTaskCleanupConfig {
	if cfg.Name == "" {
		cfg.Name = "task"
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultDoneTaskCleanupInterval
	}
	if cfg.Retention <= 0 {
		cfg.Retention = defaultDoneTaskRetention
	}
	if cfg.MaxDone <= 0 {
		cfg.MaxDone = defaultDoneTaskMaxCount
	}
	return cfg
}

func isDoneState(state tache.State) bool {
	return state == tache.StateSucceeded || state == tache.StateCanceled || state == tache.StateFailed
}

func cleanupDoneTasks[T tache.Task](manager *tache.Manager[T], cfg DoneTaskCleanupConfig, finishedAtUnix func(T) int64) {
	if manager == nil {
		return
	}
	now := time.Now().Unix()

	done := manager.GetByCondition(func(task T) bool {
		return isDoneState(task.GetState())
	})
	if len(done) == 0 {
		return
	}

	if cfg.Retention > 0 {
		retentionSeconds := int64(cfg.Retention / time.Second)
		for _, t := range done {
			finished := finishedAtUnix(t)
			if finished <= 0 {
				continue
			}
			if now-finished >= retentionSeconds {
				manager.Remove(t.GetID())
			}
		}
	}

	done = manager.GetByCondition(func(task T) bool {
		return isDoneState(task.GetState())
	})
	if cfg.MaxDone <= 0 || len(done) <= cfg.MaxDone {
		return
	}

	sort.Slice(done, func(i, j int) bool {
		fi := finishedAtUnix(done[i])
		fj := finishedAtUnix(done[j])
		if fi == fj {
			return done[i].GetID() < done[j].GetID()
		}
		if fi <= 0 {
			return true
		}
		if fj <= 0 {
			return false
		}
		return fi < fj
	})

	removeCount := len(done) - cfg.MaxDone
	for i := 0; i < removeCount; i++ {
		manager.Remove(done[i].GetID())
	}
}

func registerDoneTaskCleanup(callback func()) {
	if callback == nil {
		return
	}
	doneTaskCleanupMu.Lock()
	doneTaskCleanupCallbacks = append(doneTaskCleanupCallbacks, callback)
	doneTaskCleanupMu.Unlock()
}

func TriggerDoneTaskCleanup() {
	doneTaskCleanupOnce.Do(func() {
		utils.GoSafe("task_cleanup_dispatch", func() {
			for range doneTaskCleanupCh {
				runDoneTaskCleanupCallbacks()
				drainDoneTaskCleanupSignal()
			}
		})
	})
	select {
	case doneTaskCleanupCh <- struct{}{}:
	default:
	}
}

func drainDoneTaskCleanupSignal() {
	for {
		select {
		case <-doneTaskCleanupCh:
		default:
			return
		}
	}
}

func runDoneTaskCleanupCallbacks() {
	doneTaskCleanupMu.Lock()
	callbacks := append([]func(){}, doneTaskCleanupCallbacks...)
	doneTaskCleanupMu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

func StartDoneTaskCleanup[T tache.Task](manager *tache.Manager[T], cfg DoneTaskCleanupConfig, finishedAtUnix func(T) int64) {
	if manager == nil || finishedAtUnix == nil {
		return
	}
	cfg = normalizeDoneTaskCleanupConfig(cfg)

	registerDoneTaskCleanup(func() {
		cleanupDoneTasks(manager, cfg, finishedAtUnix)
	})
	cleanupDoneTasks(manager, cfg, finishedAtUnix)
	utils.GoSafe("task_cleanup_"+cfg.Name, func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupDoneTasks(manager, cfg, finishedAtUnix)
		}
	})
}
