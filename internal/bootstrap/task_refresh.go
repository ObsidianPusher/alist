package bootstrap

import (
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/op"
	log "github.com/sirupsen/logrus"
	"github.com/xhofe/tache"
)

const baiduTaskManagerResizeWait = 5 * time.Minute

var baiduTaskManagerResizeMu sync.Mutex
var baiduTaskManagerRefreshHookOnce sync.Once

func registerBaiduTaskManagerRefreshHook() {
	baiduTaskManagerRefreshHookOnce.Do(func() {
		op.RegisterStorageHook(func(_ string, _ driver.Driver) {
			refreshBaiduTaskManagers()
		})
	})
}

func refreshBaiduTaskManagers() {
	uploadWorkers := fs.SuggestedBaiduCopyWorkers(false)
	if uploadWorkers <= 0 {
		uploadWorkers = defaultBaiduTaskWorkers
	}
	downloadWorkers := fs.SuggestedBaiduCopyWorkers(true)
	if downloadWorkers <= 0 {
		downloadWorkers = defaultBaiduTaskWorkers
	}
	resizeTaskManagerWorkers(fs.BaiduUploadTaskManager, uploadWorkers)
	resizeTaskManagerWorkers(fs.BaiduUploadCopyTaskManager, uploadWorkers)
	resizeTaskManagerWorkers(fs.BaiduDownloadCopyTaskManager, downloadWorkers)
	resizeTaskManagerWorkers(fs.BaiduCopyTaskManager, downloadWorkers)
}

func resizeTaskManagerWorkers[T tache.Task](manager *tache.Manager[T], desired int) {
	if manager == nil || desired <= 0 {
		return
	}
	current, ok := taskManagerWorkerCount(manager)
	if !ok || current == desired {
		return
	}
	baiduTaskManagerResizeMu.Lock()
	defer baiduTaskManagerResizeMu.Unlock()

	current, ok = taskManagerWorkerCount(manager)
	if !ok || current == desired {
		return
	}
	manager.Pause()

	deadline := time.Now().Add(baiduTaskManagerResizeWait)
	for taskManagerRunningCount(manager) > 0 {
		if time.Now().After(deadline) {
			log.Warnf("[bootstrap] skip resizing baidu task manager: still running tasks current=%d desired=%d", current, desired)
			manager.Start()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := setTaskManagerWorkerCount(manager, desired); err != nil {
		log.Warnf("[bootstrap] failed to resize baidu task manager: current=%d desired=%d err=%v", current, desired, err)
		manager.Start()
		return
	}
	for i := 0; i < desired; i++ {
		manager.Start()
	}
}

func taskManagerRunningCount[T tache.Task](manager *tache.Manager[T]) int {
	if manager == nil {
		return 0
	}
	return len(manager.GetByCondition(func(task T) bool {
		return task.GetState() == tache.StateRunning
	}))
}

func taskManagerWorkerCount[T tache.Task](manager *tache.Manager[T]) (int, bool) {
	if manager == nil {
		return 0, false
	}
	v := reflect.ValueOf(manager)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return 0, false
	}
	v = v.Elem()
	field := v.FieldByName("workers")
	if !field.IsValid() {
		return 0, false
	}
	pool := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	if !pool.IsValid() || pool.IsNil() {
		return 0, false
	}
	poolValue := pool.Elem()
	if !poolValue.IsValid() {
		return 0, false
	}
	workersField := poolValue.FieldByName("workers")
	if !workersField.IsValid() {
		return 0, false
	}
	return workersField.Cap(), true
}

func setTaskManagerWorkerCount[T tache.Task](manager *tache.Manager[T], works int) error {
	v := reflect.ValueOf(manager)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil
	}
	v = v.Elem()
	field := v.FieldByName("workers")
	if !field.IsValid() {
		return nil
	}
	newPool := tache.NewWorkerPool[T](works)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(newPool))
	return nil
}
