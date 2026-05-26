package bootstrap

import (
	"sync"
	"testing"
	"time"

	"github.com/xhofe/tache"
)

type resizeTestTask struct {
	tache.Base
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (t *resizeTestTask) Run() error {
	t.once.Do(func() {
		close(t.started)
	})
	<-t.release
	return nil
}

func waitForClosed(t *testing.T, ch <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-ch:
		return
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for channel close")
	}
}

func TestResizeTaskManagerWorkers(t *testing.T) {
	manager := tache.NewManager[*resizeTestTask](tache.WithWorks(1))

	release1 := make(chan struct{})
	started1 := make(chan struct{})
	manager.Add(&resizeTestTask{
		started: started1,
		release: release1,
	})

	waitForClosed(t, started1, time.Second)

	resizeDone := make(chan struct{})
	go func() {
		resizeTaskManagerWorkers(manager, 2)
		close(resizeDone)
	}()

	time.Sleep(100 * time.Millisecond)
	close(release1)

	waitForClosed(t, resizeDone, 5*time.Second)

	works, ok := taskManagerWorkerCount(manager)
	if !ok {
		t.Fatal("failed to read task manager worker count")
	}
	if works != 2 {
		t.Fatalf("worker count = %d, want 2", works)
	}

	release2 := make(chan struct{})
	started2 := make(chan struct{})
	release3 := make(chan struct{})
	started3 := make(chan struct{})
	manager.Add(&resizeTestTask{
		started: started2,
		release: release2,
	})
	manager.Add(&resizeTestTask{
		started: started3,
		release: release3,
	})

	waitForClosed(t, started2, time.Second)
	waitForClosed(t, started3, time.Second)

	close(release2)
	close(release3)
	manager.Wait()
}
