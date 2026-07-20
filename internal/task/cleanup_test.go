package task

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/xhofe/tache"
)

type mockCleanupTask struct {
	tache.Base
	finishedAt int64
	updatedAt  int64
	createdAt  int64
	groupID    string
}

func (m *mockCleanupTask) Run() error              { return nil }
func (m *mockCleanupTask) GetUpdatedAtUnix() int64 { return m.updatedAt }
func (m *mockCleanupTask) GetCreatedAtUnix() int64 { return m.createdAt }
func (m *mockCleanupTask) GetGroupID() string      { return m.groupID }

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

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: time.Hour}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	if _, ok := manager.GetByID("old"); ok {
		t.Fatalf("expect old done task removed by retention")
	}
	if _, ok := manager.GetByID("new"); !ok {
		t.Fatalf("expect recent done task remains")
	}
}

func TestCleanupDoneTasksFallbackToUpdatedAt(t *testing.T) {
	manager := tache.NewManager[*mockCleanupTask](
		tache.WithWorks(1),
		tache.WithRunning(false),
	)
	now := time.Now().Unix()
	oldTask := &mockCleanupTask{
		Base:      tache.Base{ID: "old", State: tache.StateSucceeded},
		updatedAt: now - int64((2*time.Hour)/time.Second),
	}
	manager.Add(oldTask)

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: time.Hour}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	if _, ok := manager.GetByID("old"); ok {
		t.Fatalf("expect old done task removed by updated_at fallback")
	}
}

func TestStartDoneTaskCleanupReturnsDirectionalTrigger(t *testing.T) {
	manager := tache.NewManager[*mockCleanupTask](
		tache.WithWorks(1),
		tache.WithRunning(false),
	)
	now := time.Now().Unix()
	task := &mockCleanupTask{
		Base:      tache.Base{ID: "old", State: tache.StateSucceeded},
		updatedAt: now - int64((2*time.Hour)/time.Second),
	}
	manager.Add(task)

	trigger := StartDoneTaskCleanup(manager, DoneTaskCleanupConfig{
		Name:      "test_directional",
		Interval:  time.Hour,
		Retention: time.Hour,
	}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	task.finishedAt = now - int64((2*time.Hour)/time.Second)
	var removed atomic.Bool
	trigger()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := manager.GetByID("old"); !ok {
			removed.Store(true)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !removed.Load() {
		t.Fatalf("expect directional trigger to remove done task")
	}
}

func TestCleanupDoneTasksKeepsDoneTasksForActiveGroup(t *testing.T) {
	manager := tache.NewManager[*mockCleanupTask](
		tache.WithWorks(1),
		tache.WithRunning(false),
	)
	now := time.Now().Unix()
	done := &mockCleanupTask{
		Base:       tache.Base{ID: "done", State: tache.StateSucceeded},
		finishedAt: now - int64((2*time.Hour)/time.Second),
		groupID:    "group-1",
	}
	running := &mockCleanupTask{
		Base:    tache.Base{ID: "running", State: tache.StateRunning},
		groupID: "group-1",
	}
	manager.Add(done)
	manager.Add(running)

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: time.Hour}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	if _, ok := manager.GetByID("done"); !ok {
		t.Fatalf("expect done task in active group to be retained")
	}
}

func TestCleanupDoneTasksRemovesWholeExpiredGroupTogether(t *testing.T) {
	manager := tache.NewManager[*mockCleanupTask](
		tache.WithWorks(1),
		tache.WithRunning(false),
	)
	now := time.Now().Unix()
	first := &mockCleanupTask{
		Base:       tache.Base{ID: "group-a", State: tache.StateSucceeded},
		finishedAt: now - int64((9*24*time.Hour)/time.Second),
		groupID:    "group-expired",
	}
	second := &mockCleanupTask{
		Base:       tache.Base{ID: "group-b", State: tache.StateSucceeded},
		finishedAt: now - int64((8*24*time.Hour)/time.Second),
		groupID:    "group-expired",
	}
	manager.Add(first)
	manager.Add(second)

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: 7 * 24 * time.Hour}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	if _, ok := manager.GetByID("group-a"); ok {
		t.Fatalf("expect expired group member removed")
	}
	if _, ok := manager.GetByID("group-b"); ok {
		t.Fatalf("expect expired group member removed together")
	}
}

func TestCleanupDoneTasksKeepsWholeGroupUntilLatestMemberExpires(t *testing.T) {
	manager := tache.NewManager[*mockCleanupTask](
		tache.WithWorks(1),
		tache.WithRunning(false),
	)
	now := time.Now().Unix()
	first := &mockCleanupTask{
		Base:       tache.Base{ID: "group-c", State: tache.StateSucceeded},
		finishedAt: now - int64((9*24*time.Hour)/time.Second),
		groupID:    "group-mixed-age",
	}
	second := &mockCleanupTask{
		Base:       tache.Base{ID: "group-d", State: tache.StateSucceeded},
		finishedAt: now - int64((6*24*time.Hour)/time.Second),
		groupID:    "group-mixed-age",
	}
	manager.Add(first)
	manager.Add(second)

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: 7 * 24 * time.Hour}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	if _, ok := manager.GetByID("group-c"); !ok {
		t.Fatalf("expect group retained until latest member expires")
	}
	if _, ok := manager.GetByID("group-d"); !ok {
		t.Fatalf("expect latest group member retained")
	}
}

func TestCleanupDoneTasksDoesNotTrimByDoneCount(t *testing.T) {
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

	cleanupDoneTasks(manager, DoneTaskCleanupConfig{Retention: 24 * time.Hour, MaxDone: 1}, func(t *mockCleanupTask) int64 {
		return t.finishedAt
	})

	done := manager.GetByCondition(func(task *mockCleanupTask) bool {
		return task.GetState() == tache.StateSucceeded
	})
	if len(done) != 5 {
		t.Fatalf("expect done tasks not trimmed by MaxDone, got %d", len(done))
	}
}
