package handles

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/xhofe/tache"
)

type localBrowserExtractTask struct {
	task.TaskWithCreator
	Status        string `json:"-"`
	ArchivePath   string `json:"archive_path"`
	ArchiveAbs    string `json:"archive_abs"`
	DstDirPath    string `json:"dst_dir_path"`
	DstAbs        string `json:"dst_abs"`
	Password      string `json:"password,omitempty"`
	ArchiveFormat string `json:"archive_format,omitempty"`
	ArchiveGroup  string `json:"archive_group,omitempty"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	FinishedAt    int64  `json:"finished_at"`
}

type localBrowserCompressTask struct {
	task.TaskWithCreator
	Status     string   `json:"-"`
	DirPath    string   `json:"dir_path"`
	DstPath    string   `json:"dst_path"`
	DstAbs     string   `json:"dst_abs"`
	SrcAbs     []string `json:"src_abs"`
	SrcNames   []string `json:"src_names"`
	Level      int      `json:"level"`
	Password   string   `json:"password,omitempty"`
	CreatedAt  int64    `json:"created_at"`
	UpdatedAt  int64    `json:"updated_at"`
	FinishedAt int64    `json:"finished_at"`
}

func (t *localBrowserExtractTask) GetName() string {
	return fmt.Sprintf("extract [%s] -> [%s]", t.ArchivePath, t.DstDirPath)
}

func (t *localBrowserCompressTask) GetName() string {
	return fmt.Sprintf("compress [%s] -> [%s]", t.DirPath, t.DstPath)
}

func (t *localBrowserExtractTask) GetStatus() string {
	return t.Status
}

func (t *localBrowserCompressTask) GetStatus() string {
	return t.Status
}

func (t *localBrowserExtractTask) Run() (retErr error) {
	defer t.markFinished()
	defer t.releaseSensitiveData()
	t.markUpdated()
	defer func() {
		if r := recover(); r != nil {
			t.Status = "extract: failed (panic)"
			retErr = fmt.Errorf("extract panic: %v\n%s", r, debug.Stack())
		}
	}()
	t.Status = "extract: resolving archive"
	t.SetProgress(1)

	meta, warnings, err := extractLocalArchive(t.Ctx(), t.ArchiveAbs, t.DstAbs, t.Password)
	if err != nil {
		return err
	}
	t.ArchiveFormat = meta.Format
	t.ArchiveGroup = meta.Group
	if len(warnings) > 0 {
		t.Status = fmt.Sprintf("extract: done with warnings (%d)", len(warnings))
	} else {
		t.Status = "extract: done"
	}
	t.SetProgress(100)
	return nil
}

func (t *localBrowserCompressTask) Run() (retErr error) {
	defer t.markFinished()
	defer t.releaseSensitiveData()
	t.markUpdated()
	defer func() {
		if retErr != nil && t.DstAbs != "" {
			_ = os.Remove(t.DstAbs)
		}
		if r := recover(); r != nil {
			t.Status = "compress: failed (panic)"
			retErr = fmt.Errorf("compress panic: %v\n%s", r, debug.Stack())
		}
	}()
	t.Status = "compress: creating archive"
	t.SetProgress(1)

	warnings, err := createLocalZipArchiveWithWarnings(t.Ctx(), t.SrcAbs, t.SrcNames, t.DstAbs, t.Level, t.Password)
	if err != nil {
		return err
	}
	if len(warnings) > 0 {
		t.Status = fmt.Sprintf("compress: done with warnings (%d)", len(warnings))
	} else {
		t.Status = "compress: done"
	}
	t.SetProgress(100)
	return nil
}

func (t *localBrowserExtractTask) OnCanceled() {}

func (t *localBrowserCompressTask) OnCanceled() {}

func (t *localBrowserExtractTask) markCreated() {
	if t.CreatedAt > 0 {
		return
	}
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	t.Persist()
}

func (t *localBrowserCompressTask) markCreated() {
	if t.CreatedAt > 0 {
		return
	}
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	t.Persist()
}

func (t *localBrowserExtractTask) markUpdated() {
	task.PersistOnTimestampChange(&t.UpdatedAt, time.Now().Unix(), t.Persist)
}

func (t *localBrowserCompressTask) markUpdated() {
	task.PersistOnTimestampChange(&t.UpdatedAt, time.Now().Unix(), t.Persist)
}

func (t *localBrowserExtractTask) markFinished() {
	now := time.Now().Unix()
	t.UpdatedAt = now
	t.FinishedAt = now
	t.Persist()
	localBrowserExtractDoneTaskCleanup()
}

func (t *localBrowserCompressTask) markFinished() {
	now := time.Now().Unix()
	t.UpdatedAt = now
	t.FinishedAt = now
	t.Persist()
	localBrowserCompressDoneTaskCleanup()
}

func (t *localBrowserExtractTask) GetFinishedAtUnix() int64 {
	return t.FinishedAt
}

func (t *localBrowserCompressTask) GetFinishedAtUnix() int64 {
	return t.FinishedAt
}

func (t *localBrowserExtractTask) GetUpdatedAtUnix() int64 {
	return t.UpdatedAt
}

func (t *localBrowserCompressTask) GetUpdatedAtUnix() int64 {
	return t.UpdatedAt
}

func (t *localBrowserExtractTask) GetCreatedAtUnix() int64 {
	return t.CreatedAt
}

func (t *localBrowserCompressTask) GetCreatedAtUnix() int64 {
	return t.CreatedAt
}

func (t *localBrowserExtractTask) releaseSensitiveData() {
	t.Password = ""
}

func (t *localBrowserCompressTask) releaseSensitiveData() {
	t.Password = ""
}

var (
	localBrowserExtractTaskManagerOnce  sync.Once
	localBrowserExtractTaskManagerInst  *tache.Manager[*localBrowserExtractTask]
	localBrowserCompressTaskManagerOnce sync.Once
	localBrowserCompressTaskManagerInst *tache.Manager[*localBrowserCompressTask]
	localBrowserExtractDoneTaskCleanup  task.DoneTaskCleanupTrigger = func() {}
	localBrowserCompressDoneTaskCleanup task.DoneTaskCleanupTrigger = func() {}
)

const (
	localBrowserExtractWorkers       = 2
	localBrowserExtractMaxRetry      = 3
	localBrowserExtractTaskPersisted = true
	localBrowserPersistDebounce      = time.Second
)

func localBrowserExtractTaskManager() *tache.Manager[*localBrowserExtractTask] {
	localBrowserExtractTaskManagerOnce.Do(func() {
		localBrowserExtractTaskManagerInst = tache.NewManager[*localBrowserExtractTask](
			tache.WithWorks(localBrowserExtractWorkers),
			tache.WithPersistFunction(
				db.GetTaskDataFunc("local_browser_extract", localBrowserExtractTaskPersisted),
				db.UpdateTaskDataFunc("local_browser_extract", localBrowserExtractTaskPersisted),
			),
			tache.WithPersistDebounce(localBrowserPersistDebounce),
			tache.WithMaxRetry(localBrowserExtractMaxRetry),
		)
		localBrowserExtractDoneTaskCleanup = task.StartDoneTaskCleanup(localBrowserExtractTaskManagerInst, task.DoneTaskCleanupConfig{
			Name:      "local_browser_extract",
			Interval:  5 * time.Minute,
			Retention: 7 * 24 * time.Hour,
		}, func(t *localBrowserExtractTask) int64 {
			return t.GetFinishedAtUnix()
		})
	})
	return localBrowserExtractTaskManagerInst
}

func localBrowserCompressTaskManager() *tache.Manager[*localBrowserCompressTask] {
	localBrowserCompressTaskManagerOnce.Do(func() {
		localBrowserCompressTaskManagerInst = tache.NewManager[*localBrowserCompressTask](
			tache.WithWorks(localBrowserExtractWorkers),
			tache.WithPersistFunction(
				db.GetTaskDataFunc("local_browser_compress", localBrowserExtractTaskPersisted),
				db.UpdateTaskDataFunc("local_browser_compress", localBrowserExtractTaskPersisted),
			),
			tache.WithPersistDebounce(localBrowserPersistDebounce),
			tache.WithMaxRetry(localBrowserExtractMaxRetry),
		)
		localBrowserCompressDoneTaskCleanup = task.StartDoneTaskCleanup(localBrowserCompressTaskManagerInst, task.DoneTaskCleanupConfig{
			Name:      "local_browser_compress",
			Interval:  5 * time.Minute,
			Retention: 7 * 24 * time.Hour,
		}, func(t *localBrowserCompressTask) int64 {
			return t.GetFinishedAtUnix()
		})
	})
	return localBrowserCompressTaskManagerInst
}

func enqueueLocalBrowserExtractTask(
	ctx context.Context,
	user *model.User,
	archivePath string,
	archiveAbs string,
	dstDirPath string,
	dstAbs string,
	password string,
	meta localBrowserArchiveMeta,
) *localBrowserExtractTask {
	t := &localBrowserExtractTask{
		TaskWithCreator: task.TaskWithCreator{
			Creator: user,
		},
		Status:        "extract: queued",
		ArchivePath:   archivePath,
		ArchiveAbs:    archiveAbs,
		DstDirPath:    dstDirPath,
		DstAbs:        dstAbs,
		Password:      password,
		ArchiveFormat: meta.Format,
		ArchiveGroup:  meta.Group,
	}
	t.markCreated()
	if groupID, ok := ctx.Value(conf.CopyGroupKey).(string); ok && groupID != "" {
		t.SetGroupID(groupID)
	}
	localBrowserExtractTaskManager().Add(t)
	return t
}

func enqueueLocalBrowserCompressTask(
	ctx context.Context,
	user *model.User,
	dirPath string,
	dstPath string,
	dstAbs string,
	srcAbs []string,
	srcNames []string,
	level int,
	password string,
) *localBrowserCompressTask {
	t := &localBrowserCompressTask{
		TaskWithCreator: task.TaskWithCreator{
			Creator: user,
		},
		Status:   "compress: queued",
		DirPath:  dirPath,
		DstPath:  dstPath,
		DstAbs:   dstAbs,
		SrcAbs:   append([]string(nil), srcAbs...),
		SrcNames: append([]string(nil), srcNames...),
		Level:    level,
		Password: password,
	}
	t.markCreated()
	if groupID, ok := ctx.Value(conf.CopyGroupKey).(string); ok && groupID != "" {
		t.SetGroupID(groupID)
	}
	localBrowserCompressTaskManager().Add(t)
	return t
}
