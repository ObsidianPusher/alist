package task

import (
	"time"

	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/xhofe/tache"
)

type DoneTaskCleanupConfig struct {
	Name      string
	Interval  time.Duration
	Retention time.Duration
	MaxDone   int // Deprecated: no longer used. Done tasks are cleaned only by retention.
}

const (
	defaultDoneTaskCleanupInterval = 5 * time.Minute
	defaultDoneTaskRetention       = 7 * 24 * time.Hour
)

type DoneTaskCleanupTrigger func()

type updatedAtProvider interface {
	GetUpdatedAtUnix() int64
}

type createdAtProvider interface {
	GetCreatedAtUnix() int64
}

type groupIDProvider interface {
	GetGroupID() string
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
	activeGroups := make(map[string]struct{})
	for _, t := range manager.GetByCondition(func(task T) bool {
		return !isDoneState(task.GetState())
	}) {
		if provider, ok := any(t).(groupIDProvider); ok {
			groupID := provider.GetGroupID()
			if groupID != "" {
				activeGroups[groupID] = struct{}{}
			}
		}
	}

	done := manager.GetByCondition(func(task T) bool {
		return isDoneState(task.GetState())
	})
	if len(done) == 0 {
		return
	}

	type doneTaskMeta struct {
		id       string
		finished int64
		groupID  string
	}
	metas := make([]doneTaskMeta, 0, len(done))
	for _, t := range done {
		groupID := ""
		if provider, ok := any(t).(groupIDProvider); ok {
			groupID = provider.GetGroupID()
		}
		metas = append(metas, doneTaskMeta{
			id:       t.GetID(),
			finished: resolveDoneTaskTime(t, finishedAtUnix),
			groupID:  groupID,
		})
	}

	removeIDs := make(map[string]struct{})
	if cfg.Retention > 0 {
		retentionSeconds := int64(cfg.Retention / time.Second)
		groupMetas := make(map[string][]doneTaskMeta)
		groupFinishedAt := make(map[string]int64)
		for _, meta := range metas {
			if _, keep := activeGroups[meta.groupID]; keep {
				continue
			}
			if meta.groupID != "" {
				groupMetas[meta.groupID] = append(groupMetas[meta.groupID], meta)
				if meta.finished > groupFinishedAt[meta.groupID] {
					groupFinishedAt[meta.groupID] = meta.finished
				}
				continue
			}
			if meta.finished <= 0 {
				continue
			}
			if now-meta.finished >= retentionSeconds {
				removeIDs[meta.id] = struct{}{}
			}
		}
		for groupID, members := range groupMetas {
			groupFinished := groupFinishedAt[groupID]
			if groupFinished <= 0 {
				continue
			}
			if now-groupFinished < retentionSeconds {
				continue
			}
			for _, member := range members {
				removeIDs[member.id] = struct{}{}
			}
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
