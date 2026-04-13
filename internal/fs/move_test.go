package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/alist-org/alist/v3/drivers/local"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	conf.Conf = conf.DefaultConfig()
	conf.Conf.Database.Type = "sqlite3"
	dB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	db.Init(dB)
}

func TestMoveLocalBetweenStoragesRemovesSource(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	srcMount := "/src-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	dstMount := "/dst-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	dstDirActual := "target"

	require.NoError(t, os.MkdirAll(filepath.Join(srcRoot, "folder"), 0o777))
	require.NoError(t, os.MkdirAll(filepath.Join(dstRoot, dstDirActual), 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(srcRoot, "folder", "file.txt"), []byte("hello"), 0o666))

	_, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: srcMount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + srcRoot + `"}`,
	})
	require.NoError(t, err)
	_, err = op.CreateStorage(context.Background(), model.Storage{
		MountPath: dstMount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + dstRoot + `"}`,
	})
	require.NoError(t, err)

	require.NoError(t, Move(context.Background(), srcMount+"/folder/file.txt", dstMount+"/"+dstDirActual))

	_, err = os.Stat(filepath.Join(srcRoot, "folder", "file.txt"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))

	data, err := os.ReadFile(filepath.Join(dstRoot, dstDirActual, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(data))
}

func TestMoveAsTaskWithinSameLocalStorageUsesNativeMove(t *testing.T) {
	root := t.TempDir()
	mount := "/single-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	dstDirActual := "target"

	require.NoError(t, os.MkdirAll(filepath.Join(root, "folder"), 0o777))
	require.NoError(t, os.MkdirAll(filepath.Join(root, dstDirActual), 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(root, "folder", "file.txt"), []byte("hello"), 0o666))

	_, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: mount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + root + `"}`,
	})
	require.NoError(t, err)

	taskInfo, err := MoveAsTask(context.Background(), mount+"/folder/file.txt", mount+"/"+dstDirActual)
	require.NoError(t, err)
	require.Nil(t, taskInfo)

	_, err = os.Stat(filepath.Join(root, "folder", "file.txt"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))

	data, err := os.ReadFile(filepath.Join(root, dstDirActual, "file.txt"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(data))
}
