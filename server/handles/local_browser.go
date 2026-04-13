package handles

import (
	stdzip "archive/zip"
	"compress/flate"
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	stdpath "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/disk"
	zip "github.com/yeka/zip"
)

const (
	localBrowserMaxScanEntries  = 50000
	localBrowserDefaultPerPage  = 100
	localBrowserMaxPerPage      = 500
	localBrowserDirSizeCacheTTL = 10 * time.Minute
	localBrowserDirSizeErrTTL   = time.Minute
	localBrowserDirSizeTimeout  = 10 * time.Minute
	localBrowserDirSizeMaxBatch = 200
	localBrowserDirSizeWorkers  = 2
	localBrowserDirSizeMaxCache = 4096
)

type LocalBrowserStorageItem struct {
	MountPath string `json:"mount_path"`
	Name      string `json:"name"`
}

type LocalBrowserEntry struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	IsDir         bool      `json:"is_dir"`
	Size          int64     `json:"size"`
	Modified      time.Time `json:"modified"`
	Kind          string    `json:"kind"`
	Extractable   bool      `json:"extractable"`
	ArchiveFormat string    `json:"archive_format,omitempty"`
	ArchiveGroup  string    `json:"archive_group,omitempty"`
}

type LocalBrowserListReq struct {
	Path      string `json:"path" form:"path"`
	Keyword   string `json:"keyword" form:"keyword"`
	Kind      string `json:"kind" form:"kind"`
	Recursive bool   `json:"recursive" form:"recursive"`
	Page      int    `json:"page" form:"page"`
	PerPage   int    `json:"per_page" form:"per_page"`
}

type LocalBrowserListResp struct {
	StorageMount string              `json:"storage_mount"`
	CurrentPath  string              `json:"current_path"`
	Content      []LocalBrowserEntry `json:"content"`
	Total        int64               `json:"total"`
	Page         int                 `json:"page"`
	PerPage      int                 `json:"per_page"`
}

type LocalBrowserUsageReq struct {
	Path string `json:"path" form:"path"`
}

type LocalBrowserUsageResp struct {
	Path        string  `json:"path"`
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"used_percent"`
}

type LocalBrowserDirSizesReq struct {
	Paths []string `json:"paths" form:"paths"`
}

type LocalBrowserDirSizeItem struct {
	Path      string    `json:"path"`
	Status    string    `json:"status"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type LocalBrowserDirSizesResp struct {
	Items []LocalBrowserDirSizeItem `json:"items"`
}

type localBrowserDirSizeState struct {
	status    string
	size      int64
	updatedAt time.Time
	expireAt  time.Time
}

var localBrowserDirSizeStore = struct {
	mu       sync.Mutex
	data     map[string]localBrowserDirSizeState
	inflight map[string]struct{}
	workerCh chan struct{}
}{
	data:     map[string]localBrowserDirSizeState{},
	inflight: map[string]struct{}{},
	workerCh: make(chan struct{}, localBrowserDirSizeWorkers),
}

type LocalBrowserCompressReq struct {
	Dir         string   `json:"dir" form:"dir"`
	Names       []string `json:"names" form:"names"`
	ArchiveName string   `json:"archive_name" form:"archive_name"`
	Password    string   `json:"password" form:"password"`
}

type LocalBrowserExtractReq struct {
	ArchivePath  string   `json:"archive_path" form:"archive_path"`
	ArchivePaths []string `json:"archive_paths" form:"archive_paths"`
	DstDir       string   `json:"dst_dir" form:"dst_dir"`
	Password     string   `json:"password" form:"password"`
}

type LocalBrowserRemoveReq struct {
	Paths []string `json:"paths" form:"paths"`
}

type LocalBrowserMoveCopyReq struct {
	Paths  []string `json:"paths" form:"paths"`
	DstDir string   `json:"dst_dir" form:"dst_dir"`
}

func localBrowserAccessibleLocalStorages(user *model.User) []LocalBrowserStorageItem {
	base := utils.FixAndCleanPath(user.BasePath)
	items := make([]LocalBrowserStorageItem, 0, 8)
	for _, storage := range op.GetAllStorages() {
		st := storage.GetStorage()
		if st == nil || st.Driver != "Local" {
			continue
		}
		mp := utils.FixAndCleanPath(st.MountPath)
		if !(utils.IsSubPath(base, mp) || utils.IsSubPath(mp, base)) {
			continue
		}
		items = append(items, LocalBrowserStorageItem{
			MountPath: mp,
			Name:      st.MountPath,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].MountPath < items[j].MountPath })
	return items
}

func LocalBrowserStorages(c *gin.Context) {
	user := c.MustGet("user").(*model.User)
	common.SuccessResp(c, localBrowserAccessibleLocalStorages(user))
}

func LocalBrowserList(c *gin.Context) {
	var req LocalBrowserListReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	if req.Path == "" {
		req.Path = user.BasePath
	}
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PerPage <= 0 {
		req.PerPage = localBrowserDefaultPerPage
	}
	if req.PerPage > localBrowserMaxPerPage {
		req.PerPage = localBrowserMaxPerPage
	}
	reqPath, storage, err := resolveLocalBrowserPath(user, req.Path)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
	kind := normalizeLocalBrowserKind(req.Kind)

	var (
		content []LocalBrowserEntry
		total   int64
	)
	if req.Recursive {
		content, total, err = localBrowserRecursiveList(c, reqPath, keyword, kind, req.Page, req.PerPage)
	} else {
		content, total, err = localBrowserShallowList(c, reqPath, keyword, kind, req.Page, req.PerPage)
	}
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, LocalBrowserListResp{
		StorageMount: storage.GetStorage().MountPath,
		CurrentPath:  reqPath,
		Content:      content,
		Total:        total,
		Page:         req.Page,
		PerPage:      req.PerPage,
	})
}

func LocalBrowserUsage(c *gin.Context) {
	var req LocalBrowserUsageReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	targetPath := req.Path
	if strings.TrimSpace(targetPath) == "" {
		targetPath = user.BasePath
	}
	_, _, obj, err := localBrowserResolveObj(c, user, targetPath)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	usagePath := obj.GetPath()
	if !obj.IsDir() {
		usagePath = filepath.Dir(usagePath)
	}
	usage, err := disk.Usage(usagePath)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, LocalBrowserUsageResp{
		Path:        targetPath,
		Total:       usage.Total,
		Used:        usage.Used,
		Free:        usage.Free,
		UsedPercent: usage.UsedPercent,
	})
}

func LocalBrowserDirSizes(c *gin.Context) {
	var req LocalBrowserDirSizesReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if len(req.Paths) == 0 {
		common.SuccessResp(c, LocalBrowserDirSizesResp{Items: []LocalBrowserDirSizeItem{}})
		return
	}
	if len(req.Paths) > localBrowserDirSizeMaxBatch {
		common.ErrorStrResp(c, "too many paths in one request", 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	items := make([]LocalBrowserDirSizeItem, 0, len(req.Paths))
	seen := map[string]struct{}{}
	for _, raw := range req.Paths {
		normalized := utils.FixAndCleanPath(strings.TrimSpace(raw))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		_, _, obj, err := localBrowserResolveObj(c, user, normalized)
		if err != nil || obj == nil || !obj.IsDir() {
			items = append(items, LocalBrowserDirSizeItem{
				Path:   normalized,
				Status: "error",
				Size:   0,
			})
			continue
		}
		absPath := filepath.Clean(obj.GetPath())
		items = append(items, localBrowserGetDirSizeItem(normalized, absPath))
	}
	common.SuccessResp(c, LocalBrowserDirSizesResp{Items: items})
}

func LocalBrowserCompress(c *gin.Context) {
	var req LocalBrowserCompressReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if len(req.Names) == 0 {
		common.ErrorStrResp(c, "no files selected", 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	if !user.CanWrite() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}
	dirPath, storage, err := resolveLocalBrowserPath(user, req.Dir)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	_, dirActual, err := op.GetStorageAndActualPath(dirPath)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	dirObj, err := op.Get(c, storage, dirActual)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if !dirObj.IsDir() {
		common.ErrorStrResp(c, "target dir invalid", 400)
		return
	}
	archiveName := strings.TrimSpace(req.ArchiveName)
	if archiveName == "" {
		archiveName = fmt.Sprintf("archive-%s.zip", time.Now().Format("20060102-150405"))
	}
	if !strings.HasSuffix(strings.ToLower(archiveName), ".zip") {
		archiveName += ".zip"
	}
	level := clampZipLevel(setting.GetInt(conf.LocalBrowserZipLevel, 5))
	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = strings.TrimSpace(setting.GetStr(conf.LocalBrowserZipPassword))
	}
	baseAbs := dirObj.GetPath()
	dstAbs := filepath.Join(baseAbs, archiveName)
	if _, statErr := os.Stat(dstAbs); statErr == nil {
		common.ErrorStrResp(c, "archive already exists", 409)
		return
	}
	srcAbs := make([]string, 0, len(req.Names))
	srcNames := make([]string, 0, len(req.Names))
	for _, name := range req.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		srcPath := stdpath.Join(dirPath, name)
		srcFullPath, srcStorage, err := resolveLocalBrowserPath(user, srcPath)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		if srcStorage.GetStorage().MountPath != storage.GetStorage().MountPath {
			common.ErrorStrResp(c, "cross-storage compress is not allowed", 400)
			return
		}
		_, srcActual, err := op.GetStorageAndActualPath(srcFullPath)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		obj, err := op.Get(c, storage, srcActual)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		srcAbs = append(srcAbs, obj.GetPath())
		srcNames = append(srcNames, obj.GetName())
	}
	if len(srcAbs) == 0 {
		common.ErrorStrResp(c, "no valid files selected", 400)
		return
	}
	if err := createLocalZipArchive(c, srcAbs, srcNames, dstAbs, level, password); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, gin.H{
		"archive_path": stdpath.Join(dirPath, archiveName),
	})
}

func LocalBrowserExtract(c *gin.Context) {
	var req LocalBrowserExtractReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	archivePaths := normalizeLocalBrowserExtractPaths(req.ArchivePath, req.ArchivePaths)
	if len(archivePaths) == 0 {
		common.ErrorStrResp(c, "no archives selected", 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	if !user.CanWrite() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}
	firstArchivePath, firstStorage, err := resolveLocalBrowserPath(user, archivePaths[0])
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	dstDir := strings.TrimSpace(req.DstDir)
	if dstDir == "" {
		dstDir = stdpath.Dir(firstArchivePath)
	}
	dstPath, dstStorage, err := resolveLocalBrowserPath(user, dstDir)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if dstStorage.GetStorage().MountPath != firstStorage.GetStorage().MountPath {
		common.ErrorStrResp(c, "extract to another storage is not allowed", 400)
		return
	}
	_, dstActual, err := op.GetStorageAndActualPath(dstPath)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	dstObj, err := op.Get(c, dstStorage, dstActual)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if !dstObj.IsDir() {
		common.ErrorStrResp(c, "destination is not a directory", 400)
		return
	}
	password := strings.TrimSpace(req.Password)
	if password == "" {
		password = strings.TrimSpace(setting.GetStr(conf.LocalBrowserZipPassword))
	}

	type localBrowserExtractJob struct {
		archivePath  string
		archiveInput string
		meta         localBrowserArchiveMeta
	}
	jobs := make([]localBrowserExtractJob, 0, len(archivePaths))
	isBatch := len(archivePaths) > 1
	var firstMeta localBrowserArchiveMeta
	for _, rawArchivePath := range archivePaths {
		archivePath, storage, err := resolveLocalBrowserPath(user, rawArchivePath)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		if storage.GetStorage().MountPath != firstStorage.GetStorage().MountPath {
			common.ErrorStrResp(c, "batch extract across storages is not allowed", 400)
			return
		}
		_, archiveActual, err := op.GetStorageAndActualPath(archivePath)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		archiveObj, err := op.Get(c, storage, archiveActual)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		archiveMeta := detectLocalBrowserArchiveMeta(archiveObj.GetName())
		if archiveObj.IsDir() || !archiveMeta.Extractable {
			common.ErrorStrResp(c, "only zip/rar/7z archives are supported", 400)
			return
		}
		if isBatch && archiveMeta.MultiPart {
			common.ErrorStrResp(c, "batch extract does not support multipart archives, select the first volume only", 400)
			return
		}
		archiveInput := archiveObj.GetPath()
		resolvedMeta := archiveMeta
		if archiveMeta.MultiPart {
			archiveInput, resolvedMeta, err = resolveLocalBrowserArchiveInputWithMode(archiveObj.GetPath(), false)
			if err != nil {
				var reqErr *localBrowserRequestError
				if stderrors.As(err, &reqErr) {
					common.ErrorStrResp(c, reqErr.Error(), 400)
					return
				}
				common.ErrorResp(c, err, 500)
				return
			}
		}
		if err := precheckLocalArchivePassword(c, archiveInput, password); err != nil {
			var reqErr *localBrowserRequestError
			if stderrors.As(err, &reqErr) {
				common.ErrorStrResp(c, reqErr.Error(), 400)
				return
			}
			common.ErrorResp(c, err, 500)
			return
		}
		if len(jobs) == 0 {
			firstMeta = resolvedMeta
		}
		jobs = append(jobs, localBrowserExtractJob{
			archivePath:  archivePath,
			archiveInput: archiveInput,
			meta:         resolvedMeta,
		})
	}
	if len(jobs) == 0 {
		common.ErrorStrResp(c, "no valid archives selected", 400)
		return
	}
	groupID := ""
	if len(jobs) > 1 {
		groupID = task.NewGroupID()
	}
	var enqueueCtx context.Context = c
	if groupID != "" {
		enqueueCtx = context.WithValue(enqueueCtx, conf.CopyGroupKey, groupID)
	}

	addedTasks := make([]task.TaskInfoWithCreator, 0, len(jobs))
	for _, job := range jobs {
		addedTask := enqueueLocalBrowserExtractTask(
			enqueueCtx,
			user,
			job.archivePath,
			job.archiveInput,
			dstPath,
			dstObj.GetPath(),
			password,
			job.meta,
		)
		addedTasks = append(addedTasks, addedTask)
	}
	if groupID != "" && len(addedTasks) > 1 {
		common.SuccessResp(c, gin.H{
			"task":    buildGroupTaskInfo[task.TaskInfoWithCreator](groupID, addedTasks, "local browser extract batch"),
			"tasks":   getTaskInfos(addedTasks),
			"dst_dir": dstPath,
		})
		return
	}
	common.SuccessResp(c, gin.H{
		"task":           getTaskInfo(addedTasks[0]),
		"dst_dir":        dstPath,
		"archive_format": firstMeta.Format,
		"archive_group":  firstMeta.Group,
	})
}

func LocalBrowserRemove(c *gin.Context) {
	var req LocalBrowserRemoveReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if len(req.Paths) == 0 {
		common.ErrorStrResp(c, "no files selected", 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	if !user.CanRemove() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}
	for _, rawPath := range req.Paths {
		resolvedPath, _, err := resolveLocalBrowserPath(user, rawPath)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		if err := fs.Remove(c, resolvedPath); err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
	}
	common.SuccessResp(c)
}

func LocalBrowserCopy(c *gin.Context) {
	localBrowserMoveCopy(c, false)
}

func LocalBrowserMove(c *gin.Context) {
	localBrowserMoveCopy(c, true)
}

func localBrowserMoveCopy(c *gin.Context, isMove bool) {
	var req LocalBrowserMoveCopyReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if len(req.Paths) == 0 {
		common.ErrorStrResp(c, "no files selected", 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	if isMove {
		if !user.CanMove() {
			common.ErrorResp(c, errs.PermissionDenied, 403)
			return
		}
	} else if !user.CanCopy() {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return
	}
	dstDir, _, err := resolveLocalBrowserPath(user, req.DstDir)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	groupID := ""
	if len(req.Paths) > 1 {
		groupID = task.NewGroupID()
	}
	var runCtx context.Context = c
	if groupID != "" {
		runCtx = context.WithValue(runCtx, conf.CopyGroupKey, groupID)
	}
	addedTasks := make([]task.TaskInfoWithCreator, 0, len(req.Paths))
	for i, rawPath := range req.Paths {
		srcPath, _, err := resolveLocalBrowserPath(user, rawPath)
		if err != nil {
			common.ErrorResp(c, err, 400)
			return
		}
		if shouldSkipLocalBrowserTransfer(srcPath, dstDir) {
			continue
		}
		if isMove {
			t, moveErr := fs.MoveAsTask(runCtx, srcPath, dstDir, i < len(req.Paths)-1)
			if t != nil {
				if groupID != "" {
					t.SetGroupID(groupID)
				}
				addedTasks = append(addedTasks, t)
			}
			if moveErr != nil {
				common.ErrorResp(c, moveErr, 500)
				return
			}
			continue
		}
		t, copyErr := fs.Copy(runCtx, srcPath, dstDir, i < len(req.Paths)-1)
		if t != nil {
			if groupID != "" {
				t.SetGroupID(groupID)
			}
			addedTasks = append(addedTasks, t)
		}
		if copyErr != nil {
			common.ErrorResp(c, copyErr, 500)
			return
		}
	}
	if groupID != "" && len(addedTasks) > 0 {
		common.SuccessResp(c, gin.H{
			"task": buildGroupTaskInfo[task.TaskInfoWithCreator](groupID, addedTasks, "local browser batch"),
		})
		return
	}
	if len(addedTasks) > 0 {
		common.SuccessResp(c, gin.H{
			"tasks": getTaskInfos(addedTasks),
		})
		return
	}
	common.SuccessResp(c)
}

func shouldSkipLocalBrowserTransfer(srcPath, dstDir string) bool {
	srcPath = utils.FixAndCleanPath(srcPath)
	dstDir = utils.FixAndCleanPath(dstDir)
	if utils.PathEqual(stdpath.Dir(srcPath), dstDir) {
		return true
	}
	if utils.PathEqual(srcPath, dstDir) {
		return true
	}
	return utils.IsSubPath(dstDir, srcPath)
}

func normalizeLocalBrowserExtractPaths(single string, batch []string) []string {
	raw := make([]string, 0, len(batch)+1)
	raw = append(raw, batch...)
	if len(raw) == 0 && strings.TrimSpace(single) != "" {
		raw = append(raw, single)
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		clean := utils.FixAndCleanPath(strings.TrimSpace(item))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func resolveLocalBrowserPath(user *model.User, rawPath string) (string, driver.Driver, error) {
	reqPath, err := user.JoinPath(rawPath)
	if err != nil {
		return "", nil, err
	}
	storage, _, err := op.GetStorageAndActualPath(reqPath)
	if err != nil {
		return "", nil, err
	}
	if storage.GetStorage() == nil || storage.GetStorage().Driver != "Local" {
		return "", nil, errors.New("local browser only supports local storage")
	}
	return reqPath, storage, nil
}

func localBrowserResolveObj(ctx context.Context, user *model.User, rawPath string) (string, driver.Driver, model.Obj, error) {
	reqPath, storage, err := resolveLocalBrowserPath(user, rawPath)
	if err != nil {
		return "", nil, nil, err
	}
	_, actualPath, err := op.GetStorageAndActualPath(reqPath)
	if err != nil {
		return "", nil, nil, err
	}
	obj, err := op.Get(ctx, storage, actualPath)
	if err != nil {
		return "", nil, nil, err
	}
	return reqPath, storage, obj, nil
}

func localBrowserGetDirSizeItem(reqPath, absPath string) LocalBrowserDirSizeItem {
	now := time.Now()
	localBrowserDirSizeStore.mu.Lock()
	localBrowserPruneDirSizeCacheLocked(now)
	state, ok := localBrowserDirSizeStore.data[absPath]
	if ok {
		if state.status == "ready" && now.After(state.expireAt) {
			ok = false
			delete(localBrowserDirSizeStore.data, absPath)
		}
		if state.status == "error" && now.After(state.expireAt) {
			ok = false
			delete(localBrowserDirSizeStore.data, absPath)
		}
	}
	if ok {
		item := LocalBrowserDirSizeItem{
			Path:      reqPath,
			Status:    state.status,
			Size:      state.size,
			UpdatedAt: state.updatedAt,
		}
		if state.status == "pending" {
			localBrowserScheduleDirSizeLocked(absPath)
		}
		localBrowserDirSizeStore.mu.Unlock()
		return item
	}
	localBrowserDirSizeStore.data[absPath] = localBrowserDirSizeState{
		status:    "pending",
		size:      0,
		updatedAt: now,
	}
	localBrowserScheduleDirSizeLocked(absPath)
	localBrowserDirSizeStore.mu.Unlock()
	return LocalBrowserDirSizeItem{
		Path:      reqPath,
		Status:    "pending",
		Size:      0,
		UpdatedAt: now,
	}
}

func localBrowserPruneDirSizeCacheLocked(now time.Time) {
	for key, state := range localBrowserDirSizeStore.data {
		switch state.status {
		case "ready", "error":
			if !state.expireAt.IsZero() && now.After(state.expireAt) {
				delete(localBrowserDirSizeStore.data, key)
			}
		case "pending":
			if now.Sub(state.updatedAt) > localBrowserDirSizeTimeout {
				delete(localBrowserDirSizeStore.data, key)
				delete(localBrowserDirSizeStore.inflight, key)
			}
		}
	}
	localBrowserTrimDirSizeCacheLocked()
}

func localBrowserTrimDirSizeCacheLocked() {
	if localBrowserDirSizeMaxCache <= 0 || len(localBrowserDirSizeStore.data) <= localBrowserDirSizeMaxCache {
		return
	}
	type cacheEntry struct {
		key       string
		updatedAt time.Time
	}
	all := make([]cacheEntry, 0, len(localBrowserDirSizeStore.data))
	for key, state := range localBrowserDirSizeStore.data {
		all = append(all, cacheEntry{key: key, updatedAt: state.updatedAt})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].updatedAt.Before(all[j].updatedAt)
	})
	trimCount := len(localBrowserDirSizeStore.data) - localBrowserDirSizeMaxCache
	for i := 0; i < trimCount; i++ {
		delete(localBrowserDirSizeStore.data, all[i].key)
		delete(localBrowserDirSizeStore.inflight, all[i].key)
	}
}

func localBrowserScheduleDirSizeLocked(absPath string) {
	if _, running := localBrowserDirSizeStore.inflight[absPath]; running {
		return
	}
	localBrowserDirSizeStore.inflight[absPath] = struct{}{}
	go localBrowserComputeAndStoreDirSize(absPath)
}

func localBrowserComputeAndStoreDirSize(absPath string) {
	localBrowserDirSizeStore.workerCh <- struct{}{}
	defer func() {
		<-localBrowserDirSizeStore.workerCh
	}()
	ctx, cancel := context.WithTimeout(context.Background(), localBrowserDirSizeTimeout)
	defer cancel()
	size, err := localBrowserComputeDirSize(ctx, absPath)
	now := time.Now()

	localBrowserDirSizeStore.mu.Lock()
	defer localBrowserDirSizeStore.mu.Unlock()
	delete(localBrowserDirSizeStore.inflight, absPath)
	if err != nil {
		localBrowserDirSizeStore.data[absPath] = localBrowserDirSizeState{
			status:    "error",
			size:      0,
			updatedAt: now,
			expireAt:  now.Add(localBrowserDirSizeErrTTL),
		}
		return
	}
	localBrowserDirSizeStore.data[absPath] = localBrowserDirSizeState{
		status:    "ready",
		size:      size,
		updatedAt: now,
		expireAt:  now.Add(localBrowserDirSizeCacheTTL),
	}
}

func localBrowserComputeDirSize(ctx context.Context, root string) (int64, error) {
	info, err := os.Stat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return info.Size(), nil
	}
	var total int64
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		// Skip symlink targets to keep accounting deterministic and avoid cycles.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		fileInfo, err := d.Info()
		if err != nil {
			return err
		}
		total += fileInfo.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

func localBrowserShallowList(ctx context.Context, reqPath, keyword, kind string, page, perPage int) ([]LocalBrowserEntry, int64, error) {
	objs, err := fs.List(ctx, reqPath, &fs.ListArgs{})
	if err != nil {
		return nil, 0, err
	}
	matched := make([]LocalBrowserEntry, 0, len(objs))
	for _, obj := range objs {
		entry := toLocalBrowserEntry(reqPath, obj)
		if !localBrowserMatch(entry, keyword, kind) {
			continue
		}
		matched = append(matched, entry)
	}
	total := int64(len(matched))
	start := (page - 1) * perPage
	if start >= len(matched) {
		return []LocalBrowserEntry{}, total, nil
	}
	end := start + perPage
	if end > len(matched) {
		end = len(matched)
	}
	return matched[start:end], total, nil
}

func localBrowserRecursiveList(ctx context.Context, rootPath, keyword, kind string, page, perPage int) ([]LocalBrowserEntry, int64, error) {
	queue := []string{rootPath}
	entries := make([]LocalBrowserEntry, 0, perPage)
	var (
		total   int64
		scanned int
	)
	offset := (page - 1) * perPage
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		objs, err := fs.List(ctx, cur, &fs.ListArgs{})
		if err != nil {
			return nil, 0, err
		}
		for _, obj := range objs {
			scanned++
			if scanned > localBrowserMaxScanEntries {
				return nil, 0, errors.New("too many files, narrow your query")
			}
			entry := toLocalBrowserEntry(cur, obj)
			if entry.IsDir {
				queue = append(queue, entry.Path)
			}
			if !localBrowserMatch(entry, keyword, kind) {
				continue
			}
			if int(total) >= offset && len(entries) < perPage {
				entries = append(entries, entry)
			}
			total++
		}
	}
	return entries, total, nil
}

func toLocalBrowserEntry(parent string, obj model.Obj) LocalBrowserEntry {
	path := stdpath.Join(parent, obj.GetName())
	archiveMeta := detectLocalBrowserArchiveMeta(obj.GetName())
	extractable := !obj.IsDir() && archiveMeta.Extractable
	archiveFormat := ""
	archiveGroup := ""
	if extractable {
		archiveFormat = archiveMeta.Format
		archiveGroup = archiveMeta.Group
	}
	return LocalBrowserEntry{
		Name:          obj.GetName(),
		Path:          path,
		IsDir:         obj.IsDir(),
		Size:          obj.GetSize(),
		Modified:      obj.ModTime(),
		Kind:          inferLocalBrowserKind(obj.GetName(), obj.IsDir()),
		Extractable:   extractable,
		ArchiveFormat: archiveFormat,
		ArchiveGroup:  archiveGroup,
	}
}

func normalizeLocalBrowserKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "", "all", "dir", "video", "image", "audio", "text", "archive", "document", "package", "other":
		if kind == "" {
			return "all"
		}
		return kind
	default:
		return "all"
	}
}

func localBrowserMatch(entry LocalBrowserEntry, keyword, kind string) bool {
	if keyword != "" && !strings.Contains(strings.ToLower(entry.Name), keyword) {
		return false
	}
	if kind == "all" {
		return true
	}
	return entry.Kind == kind
}

func inferLocalBrowserKind(name string, isDir bool) string {
	if isDir {
		return "dir"
	}
	if detectLocalBrowserArchiveMeta(name).Extractable {
		return "archive"
	}
	switch utils.GetFileType(name) {
	case conf.VIDEO:
		return "video"
	case conf.IMAGE:
		return "image"
	case conf.AUDIO:
		return "audio"
	case conf.TEXT:
		return "text"
	}
	ext := strings.ToLower(utils.Ext(name))
	if localBrowserArchiveExts[ext] {
		return "archive"
	}
	if localBrowserDocumentExts[ext] {
		return "document"
	}
	if localBrowserPackageExts[ext] {
		return "package"
	}
	return "other"
}

var localBrowserArchiveExts = map[string]bool{
	"zip": true, "rar": true, "7z": true, "tar": true, "gz": true, "bz2": true, "xz": true, "jar": true,
}

var localBrowserDocumentExts = map[string]bool{
	"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true, "pptx": true,
}

var localBrowserPackageExts = map[string]bool{
	"apk": true, "ipa": true, "dmg": true, "pkg": true, "iso": true, "exe": true, "msi": true,
}

func clampZipLevel(level int) int {
	if level < 0 {
		return 0
	}
	if level > 9 {
		return 9
	}
	return level
}

func createLocalZipArchive(ctx context.Context, srcAbs []string, srcNames []string, dstAbs string, level int, password string) error {
	if strings.TrimSpace(password) == "" {
		return createLocalZipArchivePlain(ctx, srcAbs, srcNames, dstAbs, level)
	}
	return createLocalZipArchiveEncrypted(ctx, srcAbs, srcNames, dstAbs, level, password)
}

func createLocalZipArchivePlain(ctx context.Context, srcAbs []string, srcNames []string, dstAbs string, level int) error {
	level = clampZipLevel(level)
	file, err := os.Create(dstAbs)
	if err != nil {
		return err
	}
	defer file.Close()
	zipWriter := stdzip.NewWriter(file)
	store := level == 0
	if !store {
		flateLevel := localBrowserZipFlateLevel(level)
		zipWriter.RegisterCompressor(stdzip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
			return flate.NewWriter(w, flateLevel)
		})
	}
	for i, src := range srcAbs {
		if ctx.Err() != nil {
			_ = zipWriter.Close()
			return ctx.Err()
		}
		if err := addPathToStdZip(ctx, zipWriter, src, srcNames[i], store); err != nil {
			_ = zipWriter.Close()
			return err
		}
	}
	return zipWriter.Close()
}

func createLocalZipArchiveEncrypted(ctx context.Context, srcAbs []string, srcNames []string, dstAbs string, level int, password string) error {
	_ = level // yeka/zip has global compressor registration, no per-writer level override.
	file, err := os.Create(dstAbs)
	if err != nil {
		return err
	}
	defer file.Close()
	zipWriter := zip.NewWriter(file)
	for i, src := range srcAbs {
		if ctx.Err() != nil {
			_ = zipWriter.Close()
			return ctx.Err()
		}
		if err := addPathToZip(ctx, zipWriter, src, srcNames[i], level == 0, password); err != nil {
			_ = zipWriter.Close()
			return err
		}
	}
	return zipWriter.Close()
}

func localBrowserZipFlateLevel(level int) int {
	switch {
	case level <= 1:
		return flate.BestSpeed
	case level >= 9:
		return flate.BestCompression
	default:
		return level
	}
}

func addPathToStdZip(ctx context.Context, zw *stdzip.Writer, srcAbs, entryName string, store bool) error {
	srcAbs = filepath.Clean(srcAbs)
	info, err := os.Stat(srcAbs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return addFileToStdZip(ctx, zw, srcAbs, filepath.ToSlash(entryName), info, store)
	}
	root := srcAbs
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		zipName := filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if zipName != "" && !strings.HasSuffix(zipName, "/") {
				zipName += "/"
			}
			hdr, err := stdzip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = zipName
			_, err = zw.CreateHeader(hdr)
			return err
		}
		return addFileToStdZip(ctx, zw, path, zipName, info, store)
	})
}

func addFileToStdZip(ctx context.Context, zw *stdzip.Writer, absPath, zipName string, info os.FileInfo, store bool) error {
	hdr, err := stdzip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = zipName
	if store {
		hdr.Method = stdzip.Store
	} else {
		hdr.Method = stdzip.Deflate
	}
	writer, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	file, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = localBrowserCopyWithContext(ctx, writer, file)
	return err
}

func addPathToZip(ctx context.Context, zw *zip.Writer, srcAbs, entryName string, store bool, password string) error {
	srcAbs = filepath.Clean(srcAbs)
	info, err := os.Stat(srcAbs)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return addFileToZip(ctx, zw, srcAbs, filepath.ToSlash(entryName), info, store, password)
	}
	root := srcAbs
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		zipName := filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			if zipName != "" && !strings.HasSuffix(zipName, "/") {
				zipName += "/"
			}
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = zipName
			_, err = zw.CreateHeader(hdr)
			return err
		}
		return addFileToZip(ctx, zw, path, zipName, info, store, password)
	})
}

func addFileToZip(ctx context.Context, zw *zip.Writer, absPath, zipName string, info os.FileInfo, store bool, password string) error {
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = zipName
	if store {
		hdr.Method = zip.Store
	} else {
		hdr.Method = zip.Deflate
	}
	var writer io.Writer
	if password != "" {
		writer, err = zw.Encrypt(zipName, password, zip.AES256Encryption)
	} else {
		writer, err = zw.CreateHeader(hdr)
	}
	if err != nil {
		return err
	}
	file, err := os.Open(absPath)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = localBrowserCopyWithContext(ctx, writer, file)
	return err
}

func extractLocalZipArchive(ctx context.Context, archiveAbs, dstAbs, password string) ([]string, error) {
	reader, err := zip.OpenReader(archiveAbs)
	if err != nil {
		if isArchiveTruncatedOrCorrupted(err) {
			return []string{
				"zip archive is incomplete/corrupted, extracted files may be partial",
			}, nil
		}
		return nil, err
	}
	defer reader.Close()
	return extractLocalZipArchiveEntries(ctx, reader.File, dstAbs, password)
}

func extractLocalZipArchiveFromReader(ctx context.Context, reader *zip.Reader, dstAbs, password string) ([]string, error) {
	if reader == nil {
		return nil, errors.New("zip reader is nil")
	}
	return extractLocalZipArchiveEntries(ctx, reader.File, dstAbs, password)
}

func extractLocalZipArchiveEntries(ctx context.Context, files []*zip.File, dstAbs, password string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	warnings := make([]string, 0, 4)
	for _, f := range files {
		if err := extractSingleLocalZipEntry(ctx, f, dstAbs, password); err != nil {
			if isArchiveTruncatedOrCorrupted(err) {
				warnings = append(warnings, fmt.Sprintf("skip %s: archive entry is incomplete/corrupted", f.Name))
				continue
			}
			return warnings, err
		}
	}
	if err := ctx.Err(); err != nil {
		return warnings, err
	}
	return warnings, nil
}

func extractSingleLocalZipEntry(ctx context.Context, f *zip.File, dstAbs, password string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	cleanTarget, err := localBrowserResolveExtractPath(dstAbs, f.Name)
	if err != nil {
		return err
	}
	if f.FileInfo().IsDir() {
		if err := os.MkdirAll(cleanTarget, 0o755); err != nil {
			return err
		}
		return nil
	}
	mode := f.Mode()
	if mode&os.ModeSymlink != 0 {
		return errors.Errorf("unsupported symlink entry: %s", f.Name)
	}
	if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
		return err
	}
	if f.IsEncrypted() {
		f.SetPassword(password)
	}
	src, err := f.Open()
	if err != nil {
		if f.IsEncrypted() {
			return &localBrowserRequestError{message: "wrong password or encrypted zip requires password"}
		}
		return err
	}
	dst, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, localBrowserNormalizeFileMode(mode))
	if err != nil {
		_ = src.Close()
		return err
	}
	_, copyErr := localBrowserCopyWithContext(ctx, dst, src)
	closeSrcErr := src.Close()
	closeDstErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(cleanTarget)
		if f.IsEncrypted() && isArchiveTruncatedOrCorrupted(copyErr) {
			return archivePasswordRequestError(localBrowserArchiveFormatZip)
		}
		return copyErr
	}
	if closeSrcErr != nil {
		_ = os.Remove(cleanTarget)
		if f.IsEncrypted() && isArchiveTruncatedOrCorrupted(closeSrcErr) {
			return archivePasswordRequestError(localBrowserArchiveFormatZip)
		}
		return closeSrcErr
	}
	if closeDstErr != nil {
		_ = os.Remove(cleanTarget)
		return closeDstErr
	}
	return nil
}
