package fs

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	stdpath "path"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/pkg/errors"
	"github.com/xhofe/tache"
)

type FileOperation string

const (
	FileOperationCopy FileOperation = "copy"
	FileOperationMove FileOperation = "move"
)

func normalizeOperation(op FileOperation) FileOperation {
	if op == FileOperationMove {
		return FileOperationMove
	}
	return FileOperationCopy
}

type CopyTask struct {
	task.TaskWithCreator
	Status       string        `json:"-"` // don't save status to save space
	Operation    FileOperation `json:"operation"`
	SrcObjPath   string        `json:"src_path"`
	DstDirPath   string        `json:"dst_path"`
	DstObjPath   string        `json:"dst_obj_path"`
	srcStorage   driver.Driver `json:"-"`
	dstStorage   driver.Driver `json:"-"`
	SrcStorageMp string        `json:"src_storage_mp"`
	DstStorageMp string        `json:"dst_storage_mp"`
	TotalBytes   int64         `json:"total_bytes"`
	DoneBytes    int64         `json:"done_bytes"`
	CreatedAt    int64         `json:"created_at"`
	UpdatedAt    int64         `json:"updated_at"`
	FinishedAt   int64         `json:"finished_at"`
	progressMu   sync.Mutex    `json:"-"`
	fileSize     int64         `json:"-"`
	fileDone     int64         `json:"-"`
}

func (t *CopyTask) operation() FileOperation {
	return normalizeOperation(t.Operation)
}

func (t *CopyTask) isMove() bool {
	return t.operation() == FileOperationMove
}

func (t *CopyTask) GetName() string {
	return fmt.Sprintf("%s [%s](%s) to [%s](%s)", t.operation(), t.SrcStorageMp, t.SrcObjPath, t.DstStorageMp, t.DstDirPath)
}

func (t *CopyTask) GetStatus() string {
	return t.Status
}

func (t *CopyTask) Run() (err error) {
	defer t.releaseRuntimeRefs()
	defer func() {
		if utils.IsCanceled(t.Ctx()) || stderrors.Is(err, context.Canceled) {
			t.OnCanceled()
		}
	}()
	defer t.markFinished()
	t.markUpdated()
	t.initProgress(0)
	if t.srcStorage == nil {
		t.srcStorage, err = op.GetStorageByMountPath(t.SrcStorageMp)
	}
	if t.dstStorage == nil {
		t.dstStorage, err = op.GetStorageByMountPath(t.DstStorageMp)
	}
	if err != nil {
		return errors.WithMessage(err, "failed get storage")
	}
	if err := copyBetween2Storages(t, t.srcStorage, t.dstStorage, t.SrcObjPath, t.DstDirPath); err != nil {
		return err
	}
	t.SetProgress(100)
	op.ClearCache(t.dstStorage, t.DstDirPath)
	if t.isMove() {
		op.ClearCache(t.srcStorage, stdpath.Dir(t.SrcObjPath))
	}
	return nil
}

func (t *CopyTask) releaseRuntimeRefs() {
	t.srcStorage = nil
	t.dstStorage = nil
}

func (t *CopyTask) OnCanceled() {
	if t.DstObjPath == "" {
		return
	}
	dstStorage := t.dstStorage
	if dstStorage == nil {
		var err error
		dstStorage, err = op.GetStorageByMountPath(t.DstStorageMp)
		if err != nil {
			return
		}
	}
	_ = op.Remove(context.Background(), dstStorage, t.DstObjPath)
}

func (t *CopyTask) OnBeforeRetry() {
	t.OnCanceled()
}

func (t *CopyTask) initProgress(total int64) {
	t.progressMu.Lock()
	if total < 0 {
		total = 0
	}
	t.TotalBytes = total
	t.DoneBytes = 0
	t.fileSize = 0
	t.fileDone = 0
	t.SetProgress(0)
	t.progressMu.Unlock()
	t.markUpdated()
}

func (t *CopyTask) beginFileProgress(size int64) {
	t.progressMu.Lock()
	if size < 0 {
		size = 0
	}
	t.fileSize = size
	t.fileDone = 0
	if t.TotalBytes <= 0 && size > 0 {
		// Fallback for storages that cannot provide aggregate size in advance.
		// Keep progress visible per-file instead of staying at 0%.
		t.TotalBytes = t.DoneBytes + size
	}
	t.refreshProgressLocked()
	t.progressMu.Unlock()
	t.markUpdated()
}

func (t *CopyTask) updateFileProgress(progress float64) {
	t.progressMu.Lock()
	if t.fileSize <= 0 {
		t.progressMu.Unlock()
		return
	}
	p := task.NormalizeProgress(progress)
	done := int64(float64(t.fileSize) * p / 100)
	if done < t.fileDone {
		done = t.fileDone
	}
	if done > t.fileSize {
		done = t.fileSize
	}
	t.fileDone = done
	t.refreshProgressLocked()
	t.progressMu.Unlock()
	t.markUpdated()
}

func (t *CopyTask) completeFileProgress() {
	t.progressMu.Lock()
	if t.fileSize > 0 {
		if t.TotalBytes < t.DoneBytes+t.fileSize {
			t.TotalBytes = t.DoneBytes + t.fileSize
		}
		t.DoneBytes += t.fileSize
	}
	t.fileSize = 0
	t.fileDone = 0
	t.refreshProgressLocked()
	t.progressMu.Unlock()
	t.markUpdated()
}

func (t *CopyTask) rollbackFileProgress() {
	t.progressMu.Lock()
	t.fileDone = 0
	t.refreshProgressLocked()
	t.progressMu.Unlock()
	t.markUpdated()
}

func (t *CopyTask) refreshProgressLocked() {
	if t.TotalBytes <= 0 {
		if t.fileSize > 0 {
			done := t.fileDone
			if done < 0 {
				done = 0
			}
			if done > t.fileSize {
				done = t.fileSize
			}
			t.SetProgress(task.NormalizeProgress(float64(done) * 100 / float64(t.fileSize)))
		}
		return
	}
	done := t.DoneBytes + t.fileDone
	if done < 0 {
		done = 0
	}
	if done > t.TotalBytes {
		done = t.TotalBytes
	}
	t.SetProgress(task.NormalizeProgress(float64(done) * 100 / float64(t.TotalBytes)))
}

func (t *CopyTask) markCreated() {
	if t.CreatedAt > 0 {
		return
	}
	now := time.Now().Unix()
	t.CreatedAt = now
	t.UpdatedAt = now
	t.Persist()
}

func (t *CopyTask) markUpdated() {
	t.UpdatedAt = time.Now().Unix()
	t.Persist()
}

func (t *CopyTask) markFinished() {
	now := time.Now().Unix()
	t.UpdatedAt = now
	t.FinishedAt = now
	t.Persist()
}

func (t *CopyTask) GetFinishedAtUnix() int64 {
	return t.FinishedAt
}

var CopyTaskManager *tache.Manager[*CopyTask]
var BaiduCopyTaskManager *tache.Manager[*CopyTask]
var BaiduDownloadCopyTaskManager *tache.Manager[*CopyTask]
var BaiduUploadCopyTaskManager *tache.Manager[*CopyTask]
var baiduCopyTaskLimiter = newNamedTokenLimiter()

func selectCrossStorageTaskManager(srcStorage, dstStorage driver.Driver) *tache.Manager[*CopyTask] {
	srcBaidu := isBaiduStorageDriver(srcStorage)
	dstBaidu := isBaiduStorageDriver(dstStorage)
	if srcBaidu && BaiduDownloadCopyTaskManager != nil {
		return BaiduDownloadCopyTaskManager
	}
	if dstBaidu && BaiduUploadCopyTaskManager != nil {
		return BaiduUploadCopyTaskManager
	}
	if (srcBaidu || dstBaidu) && BaiduCopyTaskManager != nil {
		return BaiduCopyTaskManager
	}
	return CopyTaskManager
}

func selectCrossStorageTaskManagerByMountPath(srcMountPath, dstMountPath string) *tache.Manager[*CopyTask] {
	srcBaidu := isBaiduMountPath(srcMountPath)
	dstBaidu := isBaiduMountPath(dstMountPath)
	if srcBaidu && BaiduDownloadCopyTaskManager != nil {
		return BaiduDownloadCopyTaskManager
	}
	if dstBaidu && BaiduUploadCopyTaskManager != nil {
		return BaiduUploadCopyTaskManager
	}
	if (srcBaidu || dstBaidu) && BaiduCopyTaskManager != nil {
		return BaiduCopyTaskManager
	}
	return CopyTaskManager
}

func buildCrossStorageTask(ctx context.Context, srcStorage, dstStorage driver.Driver, srcObjActualPath, dstDirActualPath string, operation FileOperation) *CopyTask {
	taskCreator, _ := ctx.Value("user").(*model.User) // taskCreator is nil when convert failed
	t := &CopyTask{
		TaskWithCreator: task.TaskWithCreator{
			Creator: taskCreator,
		},
		Operation:    normalizeOperation(operation),
		srcStorage:   srcStorage,
		dstStorage:   dstStorage,
		SrcObjPath:   srcObjActualPath,
		DstDirPath:   dstDirActualPath,
		SrcStorageMp: srcStorage.GetStorage().MountPath,
		DstStorageMp: dstStorage.GetStorage().MountPath,
	}
	t.markCreated()
	if groupID, ok := ctx.Value(conf.CopyGroupKey).(string); ok && groupID != "" {
		t.SetGroupID(groupID)
	}
	if isBaiduStorageDriver(srcStorage) || isBaiduStorageDriver(dstStorage) {
		maxRetry := setting.GetInt(conf.BaiduDownloadMaxRetry, 3)
		if maxRetry < 0 {
			maxRetry = 0
		}
		t.SetRetry(0, maxRetry)
	}
	return t
}

func runCrossStorageDirectly(ctx context.Context, srcStorage, dstStorage driver.Driver, srcObjActualPath, dstDirActualPath string, operation FileOperation) error {
	t := buildCrossStorageTask(ctx, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath, operation)
	return copyBetween2Storages(t, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath)
}

func copyOrMoveAsTask(ctx context.Context, srcObjPath, dstDirPath string, operation FileOperation, lazyCache ...bool) (task.TaskInfoWithCreator, error) {
	srcStorage, srcObjActualPath, err := op.GetStorageAndActualPath(srcObjPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get src storage")
	}
	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return nil, errors.WithMessage(err, "failed get dst storage")
	}
	if normalizeOperation(operation) == FileOperationMove {
		if isSameStorage(srcStorage, dstStorage) {
			return nil, op.Move(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)
		}
		switch resolveMoveStrategy(srcStorage, dstStorage) {
		case moveStrategyCrossLocalStorages:
			if ctx.Value(conf.NoTaskKey) == nil {
				t := buildCrossStorageTask(ctx, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath, operation)
				manager := selectCrossStorageTaskManager(srcStorage, dstStorage)
				if manager == nil {
					return nil, errors.New("copy task manager is not initialized")
				}
				manager.Add(t)
				return t, nil
			}
			return nil, moveBetweenLocalStorages(ctx, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath, utils.IsBool(lazyCache...))
		}
		return nil, runCrossStorageDirectly(ctx, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath, operation)
	}
	if ctx.Value(conf.NoTaskKey) == nil && (isLocalStorage(srcStorage) || isLocalStorage(dstStorage)) {
		t := buildCrossStorageTask(ctx, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath, operation)
		manager := selectCrossStorageTaskManager(srcStorage, dstStorage)
		if manager == nil {
			return nil, errors.New("copy task manager is not initialized")
		}
		manager.Add(t)
		return t, nil
	}
	if isSameStorage(srcStorage, dstStorage) {
		return nil, op.Copy(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)
	}
	if ctx.Value(conf.NoTaskKey) != nil {
		if err := runCrossStorageDirectly(ctx, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath, operation); err != nil {
			return nil, err
		}
		return nil, nil
	}
	t := buildCrossStorageTask(ctx, srcStorage, dstStorage, srcObjActualPath, dstDirActualPath, operation)
	manager := selectCrossStorageTaskManager(srcStorage, dstStorage)
	if manager == nil {
		return nil, errors.New("copy task manager is not initialized")
	}
	manager.Add(t)
	return t, nil
}

// Copy or move within a remote storage can still use the native backend method.
// Local storage operations are taskified so large filesystem work never blocks the request path.
func _copy(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskInfoWithCreator, error) {
	return copyOrMoveAsTask(ctx, srcObjPath, dstDirPath, FileOperationCopy, lazyCache...)
}

func _moveAsTask(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskInfoWithCreator, error) {
	return copyOrMoveAsTask(ctx, srcObjPath, dstDirPath, FileOperationMove, lazyCache...)
}

func copyBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver, srcObjPath, dstDirPath string) error {
	if utils.IsCanceled(t.Ctx()) {
		return context.Canceled
	}
	t.Status = fmt.Sprintf("%s: getting src object", t.operation())
	srcObj, err := op.Get(t.Ctx(), srcStorage, srcObjPath)
	if err != nil {
		return errors.WithMessagef(err, "failed get src [%s] file", srcObjPath)
	}
	if total, sizeErr := estimateObjBytes(t.Ctx(), srcStorage, srcObj, srcObjPath); sizeErr == nil {
		t.initProgress(total)
	}
	t.DstObjPath = stdpath.Join(dstDirPath, srcObj.GetName())
	t.Persist()
	if sameBackend(srcStorage, dstStorage) {
		if err := copyWithinBackend(t, srcStorage, dstStorage, srcObj, srcObjPath, dstDirPath); err != nil {
			return err
		}
		if t.isMove() {
			t.Status = "move: removing source object after backend copy"
			if err := op.Remove(t.Ctx(), srcStorage, srcObjPath); err != nil {
				return errors.WithMessage(err, "failed remove source after backend copy")
			}
		}
		return nil
	}
	if srcObj.IsDir() {
		return copyDirBetween2Storages(t, srcStorage, dstStorage, srcObjPath, dstDirPath, srcObj.GetName())
	}
	if err := copyFileObjBetween2Storages(t, srcStorage, dstStorage, srcObj, srcObjPath, dstDirPath); err != nil {
		return err
	}
	if t.isMove() {
		t.Status = "move: removing source file"
		if err := op.Remove(t.Ctx(), srcStorage, srcObjPath); err != nil {
			return errors.WithMessage(err, "failed remove source file after copy")
		}
	}
	return nil
}

func copyDirBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver, srcDirPath, dstParentDirPath, srcDirName string) error {
	if utils.IsCanceled(t.Ctx()) {
		return context.Canceled
	}
	dstDirPath := stdpath.Join(dstParentDirPath, srcDirName)
	t.Status = fmt.Sprintf("%s: preparing destination dir %s", t.operation(), dstDirPath)
	if err := op.MakeDir(t.Ctx(), dstStorage, dstDirPath, true); err != nil {
		return errors.WithMessagef(err, "failed make destination dir [%s]", dstDirPath)
	}
	t.Status = fmt.Sprintf("%s: listing source dir %s", t.operation(), srcDirPath)
	objs, err := op.List(t.Ctx(), srcStorage, srcDirPath, model.ListArgs{})
	if err != nil {
		return errors.WithMessagef(err, "failed list src [%s] objs", srcDirPath)
	}
	for _, obj := range objs {
		if utils.IsCanceled(t.Ctx()) {
			return context.Canceled
		}
		childSrcPath := stdpath.Join(srcDirPath, obj.GetName())
		if obj.IsDir() {
			if err := copyDirBetween2Storages(t, srcStorage, dstStorage, childSrcPath, dstDirPath, obj.GetName()); err != nil {
				return err
			}
			continue
		}
		if err := copyFileObjBetween2Storages(t, srcStorage, dstStorage, obj, childSrcPath, dstDirPath); err != nil {
			return err
		}
		if t.isMove() {
			t.Status = fmt.Sprintf("move: removing copied source file %s", childSrcPath)
			if err := op.Remove(t.Ctx(), srcStorage, childSrcPath); err != nil {
				return errors.WithMessagef(err, "failed remove source file [%s] after copy", childSrcPath)
			}
		}
	}
	if t.isMove() {
		t.Status = fmt.Sprintf("move: removing source dir %s", srcDirPath)
		if err := op.Remove(t.Ctx(), srcStorage, srcDirPath); err != nil {
			return errors.WithMessagef(err, "failed remove source dir [%s] after copy", srcDirPath)
		}
	}
	return nil
}

func copyFileObjBetween2Storages(tsk *CopyTask, srcStorage, dstStorage driver.Driver, srcFile model.Obj, srcFilePath, dstDirPath string) error {
	maxAttempts := 1
	if isBaiduStorageDriver(srcStorage) || isBaiduStorageDriver(dstStorage) {
		maxAttempts = 4
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := copyFileObjBetween2StoragesOnce(tsk, srcStorage, dstStorage, srcFile, srcFilePath, dstDirPath)
		if err == nil {
			return nil
		}
		lastErr = err
		tsk.rollbackFileProgress()
		if attempt == maxAttempts {
			break
		}
		tsk.Status = fmt.Sprintf("%s: retrying file %s (%d/%d)", tsk.operation(), srcFilePath, attempt+1, maxAttempts)
		select {
		case <-tsk.CtxDone():
			return context.Canceled
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
	return lastErr
}

func copyFileObjBetween2StoragesOnce(tsk *CopyTask, srcStorage, dstStorage driver.Driver, srcFile model.Obj, srcFilePath, dstDirPath string) error {
	if utils.IsCanceled(tsk.Ctx()) {
		return context.Canceled
	}
	if srcFile == nil || srcFile.IsDir() {
		return errors.New("invalid source file")
	}
	releaseLimiter, err := acquireBaiduCopyTaskLimiter(tsk.Ctx(), srcStorage, dstStorage)
	if err != nil {
		return err
	}
	defer releaseLimiter()
	tsk.beginFileProgress(srcFile.GetSize())
	tsk.Status = fmt.Sprintf("%s: copying file %s", tsk.operation(), srcFilePath)
	link, _, err := op.Link(tsk.Ctx(), srcStorage, srcFilePath, model.LinkArgs{
		Header: http.Header{},
	})
	if err != nil {
		return errors.WithMessagef(err, "failed get [%s] link", srcFilePath)
	}
	fileStream := stream.FileStream{
		Obj: srcFile,
		Ctx: tsk.Ctx(),
	}
	// Any link provided is seekable.
	ss, err := stream.NewSeekableStream(fileStream, link)
	if err != nil {
		return errors.WithMessagef(err, "failed get [%s] stream", srcFilePath)
	}
	if err := op.Put(tsk.Ctx(), dstStorage, dstDirPath, ss, func(p float64) {
		tsk.updateFileProgress(p)
	}, true); err != nil {
		return errors.WithMessagef(err, "failed put file [%s] to [%s]", srcFilePath, dstDirPath)
	}
	tsk.completeFileProgress()
	return nil
}

func estimateObjBytes(ctx context.Context, storage driver.Driver, obj model.Obj, objPath string) (int64, error) {
	if obj == nil {
		return 0, nil
	}
	if !obj.IsDir() {
		if obj.GetSize() < 0 {
			return 0, nil
		}
		return obj.GetSize(), nil
	}
	children, err := op.List(ctx, storage, objPath, model.ListArgs{})
	if err != nil {
		return 0, err
	}
	var total int64
	for _, child := range children {
		childPath := stdpath.Join(objPath, child.GetName())
		size, err := estimateObjBytes(ctx, storage, child, childPath)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}

func isBaiduStorageDriver(storage driver.Driver) bool {
	return isBaiduStorageWithSeen(storage, map[uint]bool{})
}

type cryptStorageAddition struct {
	RemotePath string `json:"remote_path"`
}

type baiduLimitDirection uint8

const (
	baiduLimitDownload baiduLimitDirection = iota
	baiduLimitUpload
)

const (
	defaultBaiduCopyDownloadConcurrency = 3
	defaultBaiduCopyUploadConcurrency   = 1
)

type namedTokenLimiter struct {
	mu      sync.Mutex
	limitCh map[string]chan struct{}
}

func newNamedTokenLimiter() *namedTokenLimiter {
	return &namedTokenLimiter{
		limitCh: map[string]chan struct{}{},
	}
}

func (l *namedTokenLimiter) acquire(ctx context.Context, key string, n int) error {
	if key == "" {
		return nil
	}
	if n <= 0 {
		n = 1
	}
	l.mu.Lock()
	ch, ok := l.limitCh[key]
	if !ok {
		ch = make(chan struct{}, n)
		l.limitCh[key] = ch
	}
	l.mu.Unlock()
	select {
	case ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *namedTokenLimiter) release(key string) {
	if key == "" {
		return
	}
	l.mu.Lock()
	ch := l.limitCh[key]
	l.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	default:
	}
}

type baiduLimitEntry struct {
	key         string
	concurrency int
}

func acquireBaiduCopyTaskLimiter(ctx context.Context, srcStorage, dstStorage driver.Driver) (func(), error) {
	entries := make([]baiduLimitEntry, 0, 2)
	if key, concurrency, ok := resolveBaiduLimiter(srcStorage, baiduLimitDownload, map[uint]bool{}); ok {
		entries = append(entries, baiduLimitEntry{key: key, concurrency: concurrency})
	}
	if key, concurrency, ok := resolveBaiduLimiter(dstStorage, baiduLimitUpload, map[uint]bool{}); ok {
		entries = append(entries, baiduLimitEntry{key: key, concurrency: concurrency})
	}
	if len(entries) == 0 {
		return func() {}, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})
	acquired := make([]string, 0, len(entries))
	for _, entry := range entries {
		if err := baiduCopyTaskLimiter.acquire(ctx, entry.key, entry.concurrency); err != nil {
			for i := len(acquired) - 1; i >= 0; i-- {
				baiduCopyTaskLimiter.release(acquired[i])
			}
			return nil, err
		}
		acquired = append(acquired, entry.key)
	}
	return func() {
		for i := len(acquired) - 1; i >= 0; i-- {
			baiduCopyTaskLimiter.release(acquired[i])
		}
	}, nil
}

func resolveBaiduLimiter(storage driver.Driver, direction baiduLimitDirection, seen map[uint]bool) (string, int, bool) {
	if storage == nil || storage.GetStorage() == nil {
		return "", 0, false
	}
	st := storage.GetStorage()
	if st.Driver == "BaiduNetdisk" {
		concurrency := readBaiduTaskConcurrency(storage.GetAddition(), direction)
		key := baiduLimiterKey(storage, direction)
		return key, concurrency, true
	}
	if st.Driver != "Crypt" {
		return "", 0, false
	}
	if seen[st.ID] {
		return "", 0, false
	}
	seen[st.ID] = true
	if st.Addition == "" {
		return "", 0, false
	}
	var add cryptStorageAddition
	if err := json.Unmarshal([]byte(st.Addition), &add); err != nil {
		return "", 0, false
	}
	if add.RemotePath == "" {
		return "", 0, false
	}
	remoteStorage, _, err := op.GetStorageAndActualPath(add.RemotePath)
	if err != nil {
		return "", 0, false
	}
	return resolveBaiduLimiter(remoteStorage, direction, seen)
}

func baiduLimiterKey(storage driver.Driver, direction baiduLimitDirection) string {
	suffix := ":download"
	if direction == baiduLimitUpload {
		suffix = ":upload"
	}
	if ider, ok := storage.(driver.BackendIdentifier); ok {
		return ider.BackendID() + suffix
	}
	if st := storage.GetStorage(); st != nil {
		return fmt.Sprintf("baidu:storage:%d%s", st.ID, suffix)
	}
	return ""
}

func readBaiduTaskConcurrency(addition driver.Additional, direction baiduLimitDirection) int {
	defaultConcurrency := defaultBaiduCopyDownloadConcurrency
	fieldName := "DownloadTaskConcurrency"
	if direction == baiduLimitUpload {
		defaultConcurrency = defaultBaiduCopyUploadConcurrency
		fieldName = "UploadTaskConcurrency"
	}
	if addition == nil {
		return defaultConcurrency
	}
	v := reflect.ValueOf(addition)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return defaultConcurrency
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return defaultConcurrency
	}
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return defaultConcurrency
	}
	var concurrency int
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		concurrency = int(field.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		concurrency = int(field.Uint())
	default:
		return defaultConcurrency
	}
	if concurrency <= 0 {
		return defaultConcurrency
	}
	return concurrency
}

func SuggestedBaiduCopyWorkers(downloadDirection bool) int {
	direction := baiduLimitUpload
	if downloadDirection {
		direction = baiduLimitDownload
	}
	storages := op.GetAllStorages()
	seenKeys := map[string]struct{}{}
	total := 0
	for _, storage := range storages {
		key, concurrency, ok := resolveBaiduLimiter(storage, direction, map[uint]bool{})
		if !ok || key == "" || concurrency <= 0 {
			continue
		}
		if _, exists := seenKeys[key]; exists {
			continue
		}
		seenKeys[key] = struct{}{}
		total += concurrency
	}
	return total
}

func isBaiduMountPath(mountPath string) bool {
	if mountPath == "" {
		return false
	}
	storage, err := op.GetStorageByMountPath(mountPath)
	if err != nil {
		return false
	}
	return isBaiduStorageDriver(storage)
}

func IsBaiduCopyTask(t *CopyTask) bool {
	if t == nil {
		return false
	}
	return isBaiduMountPath(t.SrcStorageMp) || isBaiduMountPath(t.DstStorageMp)
}

func MigratePendingBaiduCopyTasks() int {
	if CopyTaskManager == nil {
		return 0
	}
	tasks := CopyTaskManager.GetByCondition(func(task *CopyTask) bool {
		if task == nil || !IsBaiduCopyTask(task) {
			return false
		}
		return task.GetState() == tache.StatePending ||
			task.GetState() == tache.StateWaitingRetry ||
			task.GetState() == tache.StateBeforeRetry
	})
	moved := 0
	for _, task := range tasks {
		target := selectCrossStorageTaskManagerByMountPath(task.SrcStorageMp, task.DstStorageMp)
		if target == nil || target == CopyTaskManager {
			continue
		}
		CopyTaskManager.Remove(task.GetID())
		if task.GetState() == tache.StateWaitingRetry || task.GetState() == tache.StateBeforeRetry {
			task.SetState(tache.StatePending)
		}
		target.Add(task)
		moved++
	}
	return moved
}

func isBaiduStorageWithSeen(storage driver.Driver, seen map[uint]bool) bool {
	if storage == nil || storage.GetStorage() == nil {
		return false
	}
	if ider, ok := storage.(driver.BackendIdentifier); ok {
		if backendID := ider.BackendID(); len(backendID) >= 6 && backendID[:6] == "baidu:" {
			return true
		}
	}
	st := storage.GetStorage()
	if st.Driver == "BaiduNetdisk" {
		return true
	}
	if st.Driver != "Crypt" {
		return false
	}
	if seen[st.ID] {
		return false
	}
	seen[st.ID] = true
	if st.Addition == "" {
		return false
	}
	var add cryptStorageAddition
	if err := json.Unmarshal([]byte(st.Addition), &add); err != nil {
		return false
	}
	if add.RemotePath == "" {
		return false
	}
	remoteStorage, _, err := op.GetStorageAndActualPath(add.RemotePath)
	if err != nil {
		return false
	}
	return isBaiduStorageWithSeen(remoteStorage, seen)
}

func copyFileBetween2Storages(tsk *CopyTask, srcStorage, dstStorage driver.Driver, srcFilePath, dstDirPath string) error {
	srcFile, err := op.Get(tsk.Ctx(), srcStorage, srcFilePath)
	if err != nil {
		return errors.WithMessagef(err, "failed get src [%s] file", srcFilePath)
	}
	return copyFileObjBetween2Storages(tsk, srcStorage, dstStorage, srcFile, srcFilePath, dstDirPath)
}

func sameBackend(srcStorage, dstStorage driver.Driver) bool {
	srcID, ok := backendID(srcStorage)
	if !ok || srcID == "" {
		return false
	}
	dstID, ok := backendID(dstStorage)
	if !ok || dstID == "" {
		return false
	}
	return srcID == dstID
}

func backendID(storage driver.Driver) (string, bool) {
	if storage == nil || storage.GetStorage() == nil {
		return "", false
	}
	if ider, ok := storage.(driver.BackendIdentifier); ok {
		if id := ider.BackendID(); id != "" {
			return id, true
		}
	}
	return fmt.Sprintf("storage:%d", storage.GetStorage().ID), true
}

func backendPath(storage driver.Driver, path string) string {
	if resolver, ok := storage.(driver.BackendPathResolver); ok {
		return resolver.BackendPath(path)
	}
	return path
}

func copyWithinBackend(
	t *CopyTask,
	srcStorage driver.Driver,
	dstStorage driver.Driver,
	srcObj model.Obj,
	srcObjPath string,
	dstDirPath string,
) error {
	if utils.IsCanceled(t.Ctx()) {
		return context.Canceled
	}
	t.Status = fmt.Sprintf("%s: copying within backend", t.operation())
	srcPath := backendPath(srcStorage, srcObjPath)
	dstPath := backendPath(dstStorage, dstDirPath)
	srcObjRef := &model.Object{
		Path:     srcPath,
		Name:     srcObj.GetName(),
		IsFolder: srcObj.IsDir(),
	}
	dstDirRef := &model.Object{
		Path:     dstPath,
		Name:     stdpath.Base(dstPath),
		IsFolder: true,
	}
	var err error
	switch s := srcStorage.(type) {
	case driver.CopyResult:
		_, err = s.Copy(t.Ctx(), srcObjRef, dstDirRef)
	case driver.Copy:
		err = s.Copy(t.Ctx(), srcObjRef, dstDirRef)
	default:
		return errs.NotImplement
	}
	if err == nil {
		op.ClearCache(dstStorage, dstDirPath)
	}
	return errors.WithStack(err)
}
