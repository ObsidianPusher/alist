package task

import (
	"testing"
	"time"

	"github.com/xhofe/tache"
)

type mockCleanupTask struct {
	tache.Base
	finishedAt int64
}

func (m *mockCleanupTask) Run() error { return nil }

func TestCleanupDoneTasksByRetention(t *testing.T) {
	manager := tache.NewManager[*mockCleanupTask](
		tache.WithWorks(1),
		tache.WithRunning(false),
	)
	now := time.Now().Unix()
	oldTask := &mockCleanupTask{Base: tache.Base{ID: "old", State: tache.StateSucceeded}, finishedAt: now - int64((2*time.Hour)/time.Second)}
	newTask := &mockCleanupTask{Base: tache.Base{ID: "new", State: tache.StateSucceeded}, finishedAt: now}
	manager.Add(oldTask)
	manager.Add(newTask)

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: time.Hour, MaxDone: 10}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	if _, ok := manager.GetByID("old"); ok {
		t.Fatalf("expect old done task removed by retention")
	}
	if _, ok := manager.GetByID("new"); !ok {
		t.Fatalf("expect recent done task remains")
	}
}

func TestCleanupDoneTasksByMaxCount(t *testing.T) {
	manager := tache.NewManager[*mockCleanupTask](
		tache.WithWorks(1),
		tache.WithRunning(false),
	)
	now := time.Now().Unix()
	for i := 0; i < 5; i++ {
		task := &mockCleanupTask{
			Base:       tache.Base{ID: string(rune('a' + i)), State: tache.StateSucceeded},
			finishedAt: now - int64((5-i)*60),
		}
		manager.Add(task)
	}

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: 24 * time.Hour, MaxDone: 2}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	done := manager.GetByCondition(func(task *mockCleanupTask) bool {
		return task.GetState() == tache.StateSucceeded
	})
	if len(done) != 2 {
		t.Fatalf("expect 2 done tasks retained, got %d", len(done))
	}
}
