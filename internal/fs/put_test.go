package fs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/stretchr/testify/require"
)

func TestPutAsTaskReturnsErrorWhenManagerMissing(t *testing.T) {
	root := t.TempDir()
	conf.Conf.TempDir = t.TempDir()
	mount := "/put-nil-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))

	_, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: mount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + root + `"}`,
	})
	require.NoError(t, err)

	originalUploadManager := UploadTaskManager
	originalBaiduUploadManager := BaiduUploadTaskManager
	UploadTaskManager = nil
	BaiduUploadTaskManager = nil
	defer func() {
		UploadTaskManager = originalUploadManager
		BaiduUploadTaskManager = originalBaiduUploadManager
	}()

	file := &stream.FileStream{
		Obj: &model.Object{
			Name: "file.txt",
			Size: int64(len("hello")),
		},
		Reader: bytes.NewReader([]byte("hello")),
	}
	tempFile, err := file.CacheFullInTempFile()
	require.NoError(t, err)
	tempFilePath := tempFile.(*os.File).Name()

	taskInfo, err := PutAsTask(context.Background(), filepath.ToSlash(mount), file)
	require.Nil(t, taskInfo)
	require.Error(t, err)
	require.Contains(t, err.Error(), "upload task manager is not initialized")
	_, statErr := os.Stat(tempFilePath)
	require.True(t, os.IsNotExist(statErr))
}
