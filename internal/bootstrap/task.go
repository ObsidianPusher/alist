package bootstrap

import (
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/setting"
	taskx "github.com/alist-org/alist/v3/internal/task"
	"github.com/xhofe/tache"
)

const (
	defaultUploadTaskWorkers     = 5
	defaultUploadTaskMaxRetry    = 5
	defaultCopyTaskWorkers       = 5
	defaultCopyTaskMaxRetry      = 5
	defaultTaskPersistent        = true
	defaultBaiduTaskWorkers      = 1
	defaultBaiduTaskMaxRetry     = 3
	defaultBaiduDownloadMaxRetry = 3
	defaultTaskPersistDebounce   = time.Second
	doneTaskRetention            = 7 * 24 * time.Hour
)

func InitTaskManager() {
	configuredBaiduRetry := setting.GetInt(conf.BaiduDownloadMaxRetry, defaultBaiduDownloadMaxRetry)
	if configuredBaiduRetry < 0 {
		configuredBaiduRetry = 0
	}
	baiduUploadRetry := defaultBaiduTaskMaxRetry
	baiduUploadWorkers := fs.SuggestedBaiduCopyWorkers(false)
	if baiduUploadWorkers <= 0 {
		baiduUploadWorkers = defaultBaiduTaskWorkers
	}
	baiduDownloadRetry := configuredBaiduRetry
	if baiduDownloadRetry <= 0 {
		baiduDownloadRetry = defaultBaiduTaskMaxRetry
	}
	baiduCopyDownloadWorkers := fs.SuggestedBaiduCopyWorkers(true)
	if baiduCopyDownloadWorkers <= 0 {
		baiduCopyDownloadWorkers = defaultBaiduTaskWorkers
	}
	baiduCopyUploadWorkers := fs.SuggestedBaiduCopyWorkers(false)
	if baiduCopyUploadWorkers <= 0 {
		baiduCopyUploadWorkers = defaultBaiduTaskWorkers
	}

	fs.UploadTaskManager = tache.NewManager[*fs.UploadTask](
		tache.WithWorks(defaultUploadTaskWorkers),
		tache.WithPersistFunction(db.GetTaskDataFunc("upload", defaultTaskPersistent), db.UpdateTaskDataFunc("upload", defaultTaskPersistent)),
		tache.WithPersistDebounce(defaultTaskPersistDebounce),
		tache.WithMaxRetry(defaultUploadTaskMaxRetry),
	)
	fs.BaiduUploadTaskManager = tache.NewManager[*fs.UploadTask](
		tache.WithWorks(baiduUploadWorkers),
		tache.WithPersistFunction(db.GetTaskDataFunc("baidu_upload", defaultTaskPersistent), db.UpdateTaskDataFunc("baidu_upload", defaultTaskPersistent)),
		tache.WithPersistDebounce(defaultTaskPersistDebounce),
		tache.WithMaxRetry(baiduUploadRetry),
	)
	fs.CopyTaskManager = tache.NewManager[*fs.CopyTask](tache.WithWorks(defaultCopyTaskWorkers), tache.WithPersistFunction(db.GetTaskDataFunc("copy", defaultTaskPersistent), db.UpdateTaskDataFunc("copy", defaultTaskPersistent)), tache.WithPersistDebounce(defaultTaskPersistDebounce), tache.WithMaxRetry(defaultCopyTaskMaxRetry))
	fs.BaiduDownloadCopyTaskManager = tache.NewManager[*fs.CopyTask](tache.WithWorks(baiduCopyDownloadWorkers), tache.WithPersistFunction(db.GetTaskDataFunc("baidu_copy_download", defaultTaskPersistent), db.UpdateTaskDataFunc("baidu_copy_download", defaultTaskPersistent)), tache.WithPersistDebounce(defaultTaskPersistDebounce), tache.WithMaxRetry(baiduDownloadRetry))
	fs.BaiduUploadCopyTaskManager = tache.NewManager[*fs.CopyTask](tache.WithWorks(baiduCopyUploadWorkers), tache.WithPersistFunction(db.GetTaskDataFunc("baidu_copy_upload", defaultTaskPersistent), db.UpdateTaskDataFunc("baidu_copy_upload", defaultTaskPersistent)), tache.WithPersistDebounce(defaultTaskPersistDebounce), tache.WithMaxRetry(baiduUploadRetry))
	// Backward compatibility: keep reading legacy baidu_copy tasks.
	fs.BaiduCopyTaskManager = tache.NewManager[*fs.CopyTask](tache.WithWorks(baiduCopyDownloadWorkers), tache.WithPersistFunction(db.GetTaskDataFunc("baidu_copy", defaultTaskPersistent), db.UpdateTaskDataFunc("baidu_copy", defaultTaskPersistent)), tache.WithPersistDebounce(defaultTaskPersistDebounce), tache.WithMaxRetry(baiduDownloadRetry))
	_ = fs.MigratePendingBaiduCopyTasks()

	fs.UploadDoneTaskCleanup = taskx.StartDoneTaskCleanup(fs.UploadTaskManager, taskx.DoneTaskCleanupConfig{
		Name:      "upload",
		Interval:  5 * time.Minute,
		Retention: doneTaskRetention,
	}, func(t *fs.UploadTask) int64 {
		return t.GetFinishedAtUnix()
	})
	fs.BaiduUploadDoneTaskCleanup = taskx.StartDoneTaskCleanup(fs.BaiduUploadTaskManager, taskx.DoneTaskCleanupConfig{
		Name:      "baidu_upload",
		Interval:  5 * time.Minute,
		Retention: doneTaskRetention,
	}, func(t *fs.UploadTask) int64 {
		return t.GetFinishedAtUnix()
	})
	for _, cfg := range []struct {
		name    string
		manager *tache.Manager[*fs.CopyTask]
		assign  func(taskx.DoneTaskCleanupTrigger)
	}{
		{name: "copy", manager: fs.CopyTaskManager, assign: func(trigger taskx.DoneTaskCleanupTrigger) { fs.CopyDoneTaskCleanup = trigger }},
		{name: "baidu_copy_download", manager: fs.BaiduDownloadCopyTaskManager, assign: func(trigger taskx.DoneTaskCleanupTrigger) { fs.BaiduDownloadCopyDoneTaskCleanup = trigger }},
		{name: "baidu_copy_upload", manager: fs.BaiduUploadCopyTaskManager, assign: func(trigger taskx.DoneTaskCleanupTrigger) { fs.BaiduUploadCopyDoneTaskCleanup = trigger }},
		{name: "baidu_copy_legacy", manager: fs.BaiduCopyTaskManager, assign: func(trigger taskx.DoneTaskCleanupTrigger) { fs.BaiduCopyDoneTaskCleanup = trigger }},
	} {
		trigger := taskx.StartDoneTaskCleanup(cfg.manager, taskx.DoneTaskCleanupConfig{
			Name:      cfg.name,
			Interval:  5 * time.Minute,
			Retention: doneTaskRetention,
		}, func(t *fs.CopyTask) int64 {
			return t.GetFinishedAtUnix()
		})
		cfg.assign(trigger)
	}
	registerBaiduTaskManagerRefreshHook()
}
