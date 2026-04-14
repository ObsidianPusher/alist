package handles

import (
	"context"
	"fmt"
	"io"
	stdpath "path"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/sign"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/alist-org/alist/v3/pkg/generic"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

type MkdirOrLinkReq struct {
	Path string `json:"path" form:"path"`
}

func FsMkdir(c *gin.Context) {
	var req MkdirOrLinkReq
	if !bindRequest(c, &req) {
		return
	}
	user := currentUser(c)
	reqPath, ok := joinUserPathOrAbort(c, user, req.Path)
	if !ok {
		return
	}
	if !ensureCanWritePath(c, user, reqPath) {
		return
	}
	if err := fs.MakeDir(c, reqPath); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c)
}

type MoveCopyReq struct {
	SrcDir string   `json:"src_dir"`
	DstDir string   `json:"dst_dir"`
	Names  []string `json:"names"`
}

type copySourceEntry struct {
	mountPath string
	storage   driver.Driver
	actual    string
	isDir     bool
	name      string
}

type fileCopyPlan struct {
	srcMountPath string
	dstDirPath   string
}

func collectDirFileCopyPlans(
	ctx context.Context,
	srcStorage driver.Driver,
	srcMountPath string,
	srcDirActualPath string,
	dstMountPath string,
	dstDirActualPath string,
	plans *[]fileCopyPlan,
) error {
	objs, err := op.List(ctx, srcStorage, srcDirActualPath, model.ListArgs{})
	if err != nil {
		return err
	}
	for _, obj := range objs {
		childSrcActualPath := stdpath.Join(srcDirActualPath, obj.GetName())
		if obj.IsDir() {
			childDstDirActualPath := stdpath.Join(dstDirActualPath, obj.GetName())
			childDstDirMountPath := stdpath.Join(dstMountPath, childDstDirActualPath)
			if err := fs.MakeDir(ctx, childDstDirMountPath, true); err != nil {
				return err
			}
			if err := collectDirFileCopyPlans(ctx, srcStorage, srcMountPath, childSrcActualPath, dstMountPath, childDstDirActualPath, plans); err != nil {
				return err
			}
			continue
		}
		*plans = append(*plans, fileCopyPlan{
			srcMountPath: stdpath.Join(srcMountPath, childSrcActualPath),
			dstDirPath:   stdpath.Join(dstMountPath, dstDirActualPath),
		})
	}
	return nil
}

func enqueueDirectoryCopyAsFileTasks(
	ctx context.Context,
	srcStorage driver.Driver,
	srcMountPath string,
	srcDirActualPath string,
	dstMountPath string,
	dstDirActualPath string,
	rootName string,
) ([]task.TaskInfoWithCreator, error) {
	rootDstDirActualPath := stdpath.Join(dstDirActualPath, rootName)
	rootDstDirMountPath := stdpath.Join(dstMountPath, rootDstDirActualPath)
	if err := fs.MakeDir(ctx, rootDstDirMountPath, true); err != nil {
		return nil, err
	}
	plans := make([]fileCopyPlan, 0, 32)
	if err := collectDirFileCopyPlans(
		ctx,
		srcStorage,
		srcMountPath,
		srcDirActualPath,
		dstMountPath,
		rootDstDirActualPath,
		&plans,
	); err != nil {
		return nil, err
	}
	added := make([]task.TaskInfoWithCreator, 0, len(plans))
	for i, plan := range plans {
		t, err := fs.Copy(ctx, plan.srcMountPath, plan.dstDirPath, i < len(plans)-1)
		if t != nil {
			added = append(added, t)
		}
		if err != nil {
			return added, err
		}
	}
	return added, nil
}

func FsMove(c *gin.Context) {
	var req MoveCopyReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if len(req.Names) == 0 {
		common.ErrorStrResp(c, "Empty file names", 400)
		return
	}
	user := currentUser(c)
	if !requireUserCapability(c, user.CanMove()) {
		return
	}
	srcDir, _, ok := prepareScopedPath(c, user, req.SrcDir)
	if !ok {
		return
	}
	dstDir, _, ok := prepareScopedPath(c, user, req.DstDir)
	if !ok {
		return
	}
	groupID := ""
	if len(req.Names) > 1 {
		groupID = task.NewGroupID()
	}
	var ctx context.Context = c
	if groupID != "" {
		ctx = context.WithValue(ctx, conf.CopyGroupKey, groupID)
	}
	var addedTasks []task.TaskInfoWithCreator
	for i, name := range req.Names {
		t, err := fs.MoveAsTask(ctx, stdpath.Join(srcDir, name), dstDir, len(req.Names) > i+1)
		if t != nil {
			if groupID != "" {
				t.SetGroupID(groupID)
			}
			addedTasks = append(addedTasks, t)
		}
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
	}
	successTaskResp(c, groupID, addedTasks, "file operation batch")
}

func FsCopy(c *gin.Context) {
	var req MoveCopyReq
	if !bindRequest(c, &req) {
		return
	}
	if len(req.Names) == 0 {
		common.ErrorStrResp(c, "Empty file names", 400)
		return
	}
	user := currentUser(c)
	if !requireUserCapability(c, user.CanCopy()) {
		return
	}
	srcDir, _, ok := prepareScopedPath(c, user, req.SrcDir)
	if !ok {
		return
	}
	dstDir, _, ok := prepareScopedPath(c, user, req.DstDir)
	if !ok {
		return
	}
	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDir)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	sourceEntries := make([]copySourceEntry, 0, len(req.Names))
	hasDir := false
	for _, name := range req.Names {
		srcMountPath := stdpath.Join(srcDir, name)
		srcStorage, srcActualPath, err := op.GetStorageAndActualPath(srcMountPath)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		obj, err := op.Get(c, srcStorage, srcActualPath)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		entry := copySourceEntry{
			mountPath: srcMountPath,
			storage:   srcStorage,
			actual:    srcActualPath,
			isDir:     obj.IsDir(),
			name:      obj.GetName(),
		}
		if entry.isDir {
			hasDir = true
		}
		sourceEntries = append(sourceEntries, entry)
	}
	groupID := ""
	if len(req.Names) > 1 || hasDir {
		groupID = task.NewGroupID()
	}
	var ctx context.Context = c
	if groupID != "" {
		ctx = context.WithValue(ctx, conf.CopyGroupKey, groupID)
	}
	var addedTasks []task.TaskInfoWithCreator
	for i, entry := range sourceEntries {
		isCrossStorage := entry.storage.GetStorage() != dstStorage.GetStorage()
		if entry.isDir && isCrossStorage {
			tasks, copyErr := enqueueDirectoryCopyAsFileTasks(
				ctx,
				entry.storage,
				entry.storage.GetStorage().MountPath,
				entry.actual,
				dstStorage.GetStorage().MountPath,
				dstDirActualPath,
				entry.name,
			)
			if groupID != "" {
				for _, t := range tasks {
					t.SetGroupID(groupID)
				}
			}
			addedTasks = append(addedTasks, tasks...)
			if copyErr != nil {
				common.ErrorResp(c, copyErr, 500)
				return
			}
			continue
		}
		t, copyErr := fs.Copy(ctx, entry.mountPath, dstDir, i < len(sourceEntries)-1)
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
	successTaskResp(c, groupID, addedTasks, "file operation batch")
}

type RenameReq struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func FsRename(c *gin.Context) {
	var req RenameReq
	if !bindRequest(c, &req) {
		return
	}
	user := currentUser(c)
	if !requireUserCapability(c, user.CanRename()) {
		return
	}
	reqPath, _, ok := prepareScopedPath(c, user, req.Path)
	if !ok {
		return
	}
	if err := fs.Rename(c, reqPath, req.Name); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c)
}

type RemoveReq struct {
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
}

func FsRemove(c *gin.Context) {
	var req RemoveReq
	if !bindRequest(c, &req) {
		return
	}
	if len(req.Names) == 0 {
		common.ErrorStrResp(c, "Empty file names", 400)
		return
	}
	user := currentUser(c)
	if !requireUserCapability(c, user.CanRemove()) {
		return
	}
	reqDir, _, ok := prepareScopedPath(c, user, req.Dir)
	if !ok {
		return
	}
	for _, name := range req.Names {
		fullPath := stdpath.Join(reqDir, name)
		err := fs.Remove(c, fullPath)
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		CleanupVideoFavoriteByPath(c, user, fullPath)
	}
	//fs.ClearCache(req.Dir)
	common.SuccessResp(c)
}

type RemoveEmptyDirectoryReq struct {
	SrcDir string `json:"src_dir"`
}

func FsRemoveEmptyDirectory(c *gin.Context) {
	var req RemoveEmptyDirectoryReq
	if !bindRequest(c, &req) {
		return
	}

	user := currentUser(c)
	if !requireUserCapability(c, user.CanRemove()) {
		return
	}
	srcDir, _, ok := prepareScopedPath(c, user, req.SrcDir)
	if !ok {
		return
	}
	rootFiles, err := fs.List(c, srcDir, &fs.ListArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}

	// record the file path
	filePathMap := make(map[model.Obj]string)
	// record the parent file
	fileParentMap := make(map[model.Obj]model.Obj)
	// removing files
	removingFiles := generic.NewQueue[model.Obj]()
	// removed files
	removedFiles := make(map[string]bool)
	for _, file := range rootFiles {
		if !file.IsDir() {
			continue
		}
		removingFiles.Push(file)
		filePathMap[file] = srcDir
	}

	for !removingFiles.IsEmpty() {

		removingFile := removingFiles.Pop()
		removingFilePath := fmt.Sprintf("%s/%s", filePathMap[removingFile], removingFile.GetName())

		if removedFiles[removingFilePath] {
			continue
		}

		subFiles, err := fs.List(c, removingFilePath, &fs.ListArgs{Refresh: true})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}

		if len(subFiles) == 0 {
			// remove empty directory
			err = fs.Remove(c, removingFilePath)
			removedFiles[removingFilePath] = true
			if err != nil {
				common.ErrorResp(c, err, 500)
				return
			}
			// recheck parent folder
			parentFile, exist := fileParentMap[removingFile]
			if exist {
				removingFiles.Push(parentFile)
			}

		} else {
			// recursive remove
			for _, subFile := range subFiles {
				if !subFile.IsDir() {
					continue
				}
				removingFiles.Push(subFile)
				filePathMap[subFile] = removingFilePath
				fileParentMap[subFile] = removingFile
			}
		}

	}

	common.SuccessResp(c)
}

// Link return real link, just for proxy program, it may contain cookie, so just allowed for admin
func Link(c *gin.Context) {
	var req MkdirOrLinkReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	//user := c.MustGet("user").(*model.User)
	//rawPath := stdpath.Join(user.BasePath, req.Path)
	// why need not join base_path? because it's always the full path
	rawPath := req.Path
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if storage.Config().OnlyLocal {
		common.SuccessResp(c, model.Link{
			URL: fmt.Sprintf("%s/p%s?d&sign=%s",
				common.GetApiUrl(c.Request),
				utils.EncodePath(rawPath, true),
				sign.Sign(rawPath)),
		})
		return
	}
	link, _, err := fs.Link(c, rawPath, model.LinkArgs{IP: c.ClientIP(), Header: c.Request.Header, HttpReq: c.Request})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if link.MFile != nil {
		defer func(ReadSeekCloser io.ReadCloser) {
			err := ReadSeekCloser.Close()
			if err != nil {
				log.Errorf("close link data error: %v", err)
			}
		}(link.MFile)
	}
	common.SuccessResp(c, link)
	return
}
