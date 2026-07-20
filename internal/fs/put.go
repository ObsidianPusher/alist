package fs

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	stdpath "path"
	"time"

	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/pkg/errors"
	"github.com/xhofe/tache"
)

type UploadTask struct {
	task.TaskWithCreator
	StorageMp        string `json:"storage_mp"`
	DstDirActualPath string `json:"dst_dir_actual_path"`
	FileName         string `json:"file_name"`
	FileSize         int64  `json:"file_size"`
	FileModified     int64  `json:"file_modified"`
	FileCreated      int64  `json:"file_created"`
	FileMimetype     string `json:"file_mimetype"`
	FileForceStream  bool   `json:"file_force_stream"`
	TempFilePath     string `json:"temp_file_path"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	FinishedAt       int64  `json:"finished_at"`

	storage driver.Driver      `json:"-"`
	file    model.FileStreamer `json:"-"`
}

type fdFile interface {
	Fd() uintptr
}

func (t *UploadTask) GetName() string {
	name := t.FileName
	dstPath := t.DstDirActualPath
	mountPath := t.StorageMp
	if t.file != nil {
		name = t.file.GetName()
	}
	if t.storage != nil && t.storage.GetStorage() != nil && t.file != nil {
		return fmt.Sprintf("upload %s to [%s](%s)", name, t.storage.GetStorage().MountPath, dstPath)
	}
	if mountPath == "" && t.storage != nil && t.storage.GetStorage() != nil {
		mountPath = t.storage.GetStorage().MountPath
	}
	if name == "" {
		name = "[released]"
	}
	if dstPath == "" {
		dstPath = "/"
	}
	if mountPath != "" {
		return fmt.Sprintf("upload %s to [%s](%s)", name, mountPath, dstPath)
	}
	return fmt.Sprintf("upload %s to (%s)", name, dstPath)
}

func (t *UploadTask) GetStatus() string {
	return "uploading"
}

func (t *UploadTask) Run() (err error) {
	defer func() {
		// Upload tasks may be retained in manager history for a long time.
		// Drop file reference explicitly to avoid keeping stream state alive.
		t.file = nil
		t.storage = nil
	}()
	defer t.markFinished()
	t.markUpdated()
	if err = t.prepareRuntime(); err != nil {
		return err
	}
	defer func() {
		if utils.IsCanceled(t.Ctx()) || stderrors.Is(err, context.Canceled) {
			t.OnCanceled()
		}
	}()
	task.SetProgressIfChanged(&t.Base, 0, 0)
	err = op.Put(t.Ctx(), t.storage, t.DstDirActualPath, t.file, func(p float64) {
		task.SetProgressIfChanged(&t.Base, p, task.DefaultProgressPersistStep)
	}, true)
	if err == nil {
		task.SetProgressIfChanged(&t.Base, 100, 0)
	}
	return err
}

func (t *UploadTask) prepareRuntime() error {
	if t.storage == nil {
		if t.StorageMp == "" {
			return fmt.Errorf("missing storage mount path for upload task")
		}
		storage, err := op.GetStorageByMountPath(t.StorageMp)
		if err != nil {
			return err
		}
		t.storage = storage
	}
	if t.file != nil {
		return nil
	}
	if t.TempFilePath == "" {
		return fmt.Errorf("missing temp file for upload task")
	}
	tmpFile, err := os.Open(t.TempFilePath)
	if err != nil {
		return err
	}
	fs := &stream.FileStream{
		Obj: &model.Object{
			Name:     t.FileName,
			Size:     t.FileSize,
			Modified: time.Unix(t.FileModified, 0),
			Ctime:    time.Unix(t.FileCreated, 0),
		},
		Reader:            tmpFile,
		Mimetype:          t.FileMimetype,
		WebPutAsTask:      true,
		ForceStreamUpload: t.FileForceStream,
	}
	fs.SetTmpFile(tmpFile)
	fs.SetKeepTempFile(true)
	t.file = fs
	return nil
}

func (t *UploadTask) markCreated() {
	if t.CreatedAt > 0 {
		return
	}
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	t.Persist()
}

func (t *UploadTask) markUpdated() {
	task.PersistOnTimestampChange(&t.UpdatedAt, time.Now().Unix(), t.Persist)
}

func (t *UploadTask) markFinished() {
	now := time.Now().Unix()
	t.UpdatedAt = now
	t.FinishedAt = now
	t.Persist()
	selectUploadDoneTaskCleanupByMountPath(t.StorageMp)()
}

func (t *UploadTask) GetFinishedAtUnix() int64 {
	return t.FinishedAt
}

func (t *UploadTask) GetUpdatedAtUnix() int64 {
	return t.UpdatedAt
}

func (t *UploadTask) GetCreatedAtUnix() int64 {
	return t.CreatedAt
}

func (t *UploadTask) cleanupTempFile() {
	if t.TempFilePath == "" {
		return
	}
	_ = os.RemoveAll(t.TempFilePath)
	t.TempFilePath = ""
}

func (t *UploadTask) cleanupResidualTarget() {
	if t.StorageMp == "" || t.DstDirActualPath == "" || t.FileName == "" {
		return
	}
	storage, err := op.GetStorageByMountPath(t.StorageMp)
	if err != nil {
		return
	}
	_ = op.Remove(context.Background(), storage, stdpath.Join(t.DstDirActualPath, t.FileName))
}

func (t *UploadTask) OnCanceled() {
	if t.storage == nil || t.file == nil {
		t.cleanupResidualTarget()
		return
	}
	dstPath := stdpath.Join(t.DstDirActualPath, t.file.GetName())
	_ = op.Remove(context.Background(), t.storage, dstPath)
}

func (t *UploadTask) OnBeforeRetry() {
	t.cleanupResidualTarget()
}

func (t *UploadTask) OnSucceeded() {
	t.cleanupTempFile()
}

var UploadTaskManager *tache.Manager[*UploadTask]
var BaiduUploadTaskManager *tache.Manager[*UploadTask]
var UploadDoneTaskCleanup task.DoneTaskCleanupTrigger = func() {}
var BaiduUploadDoneTaskCleanup task.DoneTaskCleanupTrigger = func() {}

func selectUploadDoneTaskCleanupByMountPath(mountPath string) task.DoneTaskCleanupTrigger {
	if isBaiduMountPath(mountPath) && BaiduUploadDoneTaskCleanup != nil {
		return BaiduUploadDoneTaskCleanup
	}
	if UploadDoneTaskCleanup != nil {
		return UploadDoneTaskCleanup
	}
	return func() {}
}

// putAsTask add as a put task and return immediately
func putAsTask(ctx context.Context, dstDirPath string, file model.FileStreamer) (task.TaskInfoWithCreator, error) {
	storage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get storage")
	}
	if storage.Config().NoUpload {
		return nil, errors.WithStack(errs.UploadNotSupported)
	}
	tempFile, err := file.CacheFullInTempFile()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create temp file")
	}
	if f, ok := tempFile.(fdFile); ok {
		_ = utils.DropFileCacheByFD(f.Fd(), 0, 0)
	}
	if keepTempFile, ok := file.(interface{ SetKeepTempFile(bool) }); ok {
		keepTempFile.SetKeepTempFile(true)
	}
	tempFilePath := ""
	if namedTempFile, ok := tempFile.(interface{ Name() string }); ok {
		tempFilePath = namedTempFile.Name()
	}
	if tempFilePath == "" {
		return nil, errors.New("failed to resolve temp file path for upload task")
	}
	taskCreator, _ := ctx.Value("user").(*model.User) // taskCreator is nil when convert failed
	t := &UploadTask{
		TaskWithCreator: task.TaskWithCreator{
			Creator: taskCreator,
		},
		StorageMp:        storage.GetStorage().MountPath,
		DstDirActualPath: dstDirActualPath,
		FileName:         file.GetName(),
		FileSize:         file.GetSize(),
		FileModified:     file.ModTime().Unix(),
		FileCreated:      file.CreateTime().Unix(),
		FileMimetype:     file.GetMimetype(),
		FileForceStream:  file.IsForceStreamUpload(),
		TempFilePath:     tempFilePath,
		storage:          storage,
		file:             file,
	}
	t.markCreated()
	manager := UploadTaskManager
	if isBaiduStorageDriver(storage) && BaiduUploadTaskManager != nil {
		manager = BaiduUploadTaskManager
	}
	if manager == nil {
		return nil, errors.New("upload task manager is not initialized")
	}
	manager.Add(t)
	return t, nil
}

// putDirect put the file and return after finish
func putDirectly(ctx context.Context, dstDirPath string, file model.FileStreamer, lazyCache ...bool) error {
	storage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return errors.WithMessage(err, "failed get storage")
	}
	if storage.Config().NoUpload {
		return errors.WithStack(errs.UploadNotSupported)
	}
	return op.Put(ctx, storage, dstDirActualPath, file, nil, lazyCache...)
}
