package handles

import (
	"github.com/alist-org/alist/v3/internal/task"
	"mime/multipart"
	"net/url"
	stdpath "path"
	"strconv"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
)

func cancelUploadTasks(tasks []task.TaskInfoWithCreator) {
	for _, added := range tasks {
		if fs.UploadTaskManager != nil {
			fs.UploadTaskManager.Cancel(added.GetID())
		}
		if fs.BaiduUploadTaskManager != nil {
			fs.BaiduUploadTaskManager.Cancel(added.GetID())
		}
	}
}

func getLastModified(c *gin.Context) time.Time {
	now := time.Now()
	lastModifiedStr := c.GetHeader("Last-Modified")
	lastModifiedMillisecond, err := strconv.ParseInt(lastModifiedStr, 10, 64)
	if err != nil {
		return now
	}
	lastModified := time.UnixMilli(lastModifiedMillisecond)
	return lastModified
}

func FsStream(c *gin.Context) {
	path := c.GetHeader("File-Path")
	path, err := url.PathUnescape(path)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	asTask := true
	groupID := c.GetHeader("Task-Group")
	if groupID == "" {
		groupID = c.GetHeader("X-Task-Group")
	}
	user := c.MustGet("user").(*model.User)
	path, err = user.JoinPath(path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	dir, name := stdpath.Split(path)
	sizeStr := c.GetHeader("Content-Length")
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	s := &stream.FileStream{
		Obj: &model.Object{
			Name:     name,
			Size:     size,
			Modified: getLastModified(c),
		},
		Reader:       c.Request.Body,
		Mimetype:     c.GetHeader("Content-Type"),
		WebPutAsTask: asTask,
	}
	var t task.TaskInfoWithCreator
	t, err = fs.PutAsTask(c, dir, s)
	defer c.Request.Body.Close()
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if t == nil {
		common.SuccessResp(c)
		return
	}
	if groupID != "" {
		t.SetGroupID(groupID)
	}
	common.SuccessResp(c, gin.H{
		"task": getTaskInfo(t),
	})
}

func FsForm(c *gin.Context) {
	path := c.GetHeader("File-Path")
	path, err := url.PathUnescape(path)
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	asTask := true
	groupID := c.GetHeader("Task-Group")
	if groupID == "" {
		groupID = c.GetHeader("X-Task-Group")
	}
	user := c.MustGet("user").(*model.User)
	path, err = user.JoinPath(path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	storage, err := fs.GetStorage(path, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	if storage.Config().NoUpload {
		common.ErrorStrResp(c, "Current storage doesn't support upload", 405)
		return
	}
	form, err := c.MultipartForm()
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	files := form.File["file"]
	if len(files) == 0 {
		files = form.File["files"]
	}
	if len(files) == 0 {
		files = form.File["file[]"]
	}
	if len(files) == 0 {
		file, err := c.FormFile("file")
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		files = []*multipart.FileHeader{file}
	}
	if len(files) > 1 && groupID == "" {
		groupID = task.NewGroupID()
	}

	var tasks []task.TaskInfoWithCreator
	lastModified := getLastModified(c)
	for _, file := range files {
		f, err := file.Open()
		if err != nil {
			cancelUploadTasks(tasks)
			common.ErrorResp(c, err, 500)
			return
		}
		name := file.Filename
		dir := path
		if len(files) == 1 {
			dir, name = stdpath.Split(path)
			if name == "" {
				name = file.Filename
				dir = path
			}
		} else if !strings.HasSuffix(path, "/") {
			dir = stdpath.Dir(path)
		}
		s := stream.FileStream{
			Obj: &model.Object{
				Name:     name,
				Size:     file.Size,
				Modified: lastModified,
			},
			Reader:       f,
			Mimetype:     file.Header.Get("Content-Type"),
			WebPutAsTask: asTask,
		}
		if _, err := s.CacheFullInTempFile(); err != nil {
			_ = f.Close()
			cancelUploadTasks(tasks)
			common.ErrorResp(c, err, 500)
			return
		}
		_ = f.Close()
		t, err := fs.PutAsTask(c, dir, &s)
		if err != nil {
			cancelUploadTasks(tasks)
			common.ErrorResp(c, err, 500)
			return
		}
		if t == nil {
			continue
		}
		if groupID != "" {
			t.SetGroupID(groupID)
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		common.SuccessResp(c)
		return
	}
	if groupID != "" && len(tasks) > 1 {
		common.SuccessResp(c, gin.H{
			"task": buildGroupTaskInfo[task.TaskInfoWithCreator](groupID, tasks, "upload batch"),
		})
		return
	}
	common.SuccessResp(c, gin.H{
		"task": getTaskInfo(tasks[0]),
	})
}
