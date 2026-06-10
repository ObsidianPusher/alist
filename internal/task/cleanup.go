package task

import (
	"sort"
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

type DoneTaskCleanupTrigger func()

type updatedAtProvider interface {
	GetUpdatedAtUnix() int64
}

type createdAtProvider interface {
	GetCreatedAtUnix() int64
}

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

func resolveDoneTaskTime[T tache.Task](task T, finishedAtUnix func(T) int64) int64 {
	finished := finishedAtUnix(task)
	if finished > 0 {
		return finished
	}
	if provider, ok := any(task).(updatedAtProvider); ok {
		if updated := provider.GetUpdatedAtUnix(); updated > 0 {
			return updated
		}
	}
	if provider, ok := any(task).(createdAtProvider); ok {
		return provider.GetCreatedAtUnix()
	}
	return 0
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

	type doneTaskMeta struct {
		id       string
		finished int64
	}
	metas := make([]doneTaskMeta, 0, len(done))
	for _, t := range done {
		metas = append(metas, doneTaskMeta{
			id:       t.GetID(),
			finished: resolveDoneTaskTime(t, finishedAtUnix),
		})
	}

	removeIDs := make(map[string]struct{})
	if cfg.Retention > 0 {
		retentionSeconds := int64(cfg.Retention / time.Second)
		for _, meta := range metas {
			if meta.finished <= 0 {
				continue
			}
			if now-meta.finished >= retentionSeconds {
				removeIDs[meta.id] = struct{}{}
			}
		}
	}

	remaining := metas[:0]
	for _, meta := range metas {
		if _, removed := removeIDs[meta.id]; removed {
			continue
		}
		remaining = append(remaining, meta)
	}
	if cfg.MaxDone > 0 && len(remaining) > cfg.MaxDone {
		sort.Slice(remaining, func(i, j int) bool {
			fi := remaining[i].finished
			fj := remaining[j].finished
			if fi == fj {
				return remaining[i].id < remaining[j].id
			}
			if fi <= 0 {
				return true
			}
			if fj <= 0 {
				return false
			}
			return fi < fj
		})
		removeCount := len(remaining) - cfg.MaxDone
		for i := 0; i < removeCount; i++ {
			removeIDs[remaining[i].id] = struct{}{}
		}
	}
	for id := range removeIDs {
		manager.Remove(id)
	}
}

func newDoneTaskCleanupTrigger(name string, callback func()) DoneTaskCleanupTrigger {
	ch := make(chan struct{}, 1)
	utils.GoSafe("task_cleanup_dispatch_"+name, func() {
		for range ch {
			callback()
			for {
				select {
				case <-ch:
				default:
					goto next
				}
			}
		next:
		}
	})
	return func() {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func StartDoneTaskCleanup[T tache.Task](manager *tache.Manager[T], cfg DoneTaskCleanupConfig, finishedAtUnix func(T) int64) DoneTaskCleanupTrigger {
	if manager == nil || finishedAtUnix == nil {
		return func() {}
	}
	cfg = normalizeDoneTaskCleanupConfig(cfg)
	callback := func() {
		cleanupDoneTasks(manager, cfg, finishedAtUnix)
	}
	trigger := newDoneTaskCleanupTrigger(cfg.Name, callback)

	callback()
	utils.GoSafe("task_cleanup_"+cfg.Name, func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for range ticker.C {
			callback()
		}
	})
	return trigger
}
