package handles

import (
	"context"
	"fmt"
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

func (t *localBrowserExtractTask) GetName() string {
	return fmt.Sprintf("extract [%s] -> [%s]", t.ArchivePath, t.DstDirPath)
}

func (t *localBrowserExtractTask) GetStatus() string {
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

func (t *localBrowserExtractTask) OnCanceled() {}

func (t *localBrowserExtractTask) markCreated() {
	if t.CreatedAt > 0 {
		return
	}
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	t.Persist()
}

func (t *localBrowserExtractTask) markUpdated() {
	t.UpdatedAt = time.Now().Unix()
	t.Persist()
}

func (t *localBrowserExtractTask) markFinished() {
	now := time.Now().Unix()
	t.UpdatedAt = now
	t.FinishedAt = now
	t.Persist()
	task.TriggerDoneTaskCleanup()
}

func (t *localBrowserExtractTask) GetFinishedAtUnix() int64 {
	return t.FinishedAt
}

func (t *localBrowserExtractTask) releaseSensitiveData() {
	t.Password = ""
}

var (
	localBrowserExtractTaskManagerOnce sync.Once
	localBrowserExtractTaskManagerInst *tache.Manager[*localBrowserExtractTask]
)

const (
	localBrowserExtractWorkers       = 2
	localBrowserExtractMaxRetry      = 3
	localBrowserExtractTaskPersisted = true
)

func localBrowserExtractTaskManager() *tache.Manager[*localBrowserExtractTask] {
	localBrowserExtractTaskManagerOnce.Do(func() {
		localBrowserExtractTaskManagerInst = tache.NewManager[*localBrowserExtractTask](
			tache.WithWorks(localBrowserExtractWorkers),
			tache.WithPersistFunction(
				db.GetTaskDataFunc("local_browser_extract", localBrowserExtractTaskPersisted),
				db.UpdateTaskDataFunc("local_browser_extract", localBrowserExtractTaskPersisted),
			),
			tache.WithMaxRetry(localBrowserExtractMaxRetry),
		)
		task.StartDoneTaskCleanup(localBrowserExtractTaskManagerInst, task.DoneTaskCleanupConfig{
			Name:      "local_browser_extract",
			Interval:  5 * time.Minute,
			Retention: 6 * time.Hour,
			MaxDone:   100,
		}, func(t *localBrowserExtractTask) int64 {
			return t.GetFinishedAtUnix()
		})
	})
	return localBrowserExtractTaskManagerInst
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
