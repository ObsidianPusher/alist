package fs

import (
	"context"
	"io"
	"os"
	stdpath "path"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/alist-org/alist/v3/drivers/local"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
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

type memoryDirEntry struct {
	path  string
	isDir bool
	data  []byte
}

type memoryWriteStorage struct {
	model.Storage
	items map[string]*memoryDirEntry
}

func newMemoryWriteStorage(mount string) *memoryWriteStorage {
	s := &memoryWriteStorage{
		Storage: model.Storage{
			MountPath: mount,
			Driver:    "memory-write-test",
			Status:    op.WORK,
		},
		items: map[string]*memoryDirEntry{
			"/": {path: "/", isDir: true},
		},
	}
	return s
}

func (s *memoryWriteStorage) Config() driver.Config            { return driver.Config{Name: "memory-write-test"} }
func (s *memoryWriteStorage) GetStorage() *model.Storage       { return &s.Storage }
func (s *memoryWriteStorage) SetStorage(storage model.Storage) { s.Storage = storage }
func (s *memoryWriteStorage) GetAddition() driver.Additional   { return nil }
func (s *memoryWriteStorage) Init(context.Context) error       { return nil }
func (s *memoryWriteStorage) Drop(context.Context) error       { return nil }
func (s *memoryWriteStorage) Link(context.Context, model.Obj, model.LinkArgs) (*model.Link, error) {
	return nil, errs.NotImplement
}

func (s *memoryWriteStorage) Get(ctx context.Context, path string) (model.Obj, error) {
	entry, ok := s.items[normalizeTestPath(path)]
	if !ok {
		return nil, errs.ObjectNotFound
	}
	return s.objFromEntry(entry), nil
}

func (s *memoryWriteStorage) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	dirPath := normalizeTestPath(dir.GetPath())
	entry, ok := s.items[dirPath]
	if !ok || !entry.isDir {
		return nil, errs.ObjectNotFound
	}
	objs := make([]model.Obj, 0)
	for path, child := range s.items {
		if path == dirPath || path == "/" {
			continue
		}
		if stdpath.Dir(path) != dirPath {
			continue
		}
		objs = append(objs, s.objFromEntry(child))
	}
	model.SortFiles(objs, "name", "asc")
	return objs, nil
}

func (s *memoryWriteStorage) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	parentPath := normalizeTestPath(parentDir.GetPath())
	fullPath := normalizeTestPath(stdpath.Join(parentPath, dirName))
	if existing, ok := s.items[fullPath]; ok {
		if existing.isDir {
			return nil
		}
		return os.ErrExist
	}
	s.items[fullPath] = &memoryDirEntry{path: fullPath, isDir: true}
	return nil
}

func (s *memoryWriteStorage) Put(ctx context.Context, dstDir model.Obj, stream model.FileStreamer, up driver.UpdateProgress) error {
	dirPath := normalizeTestPath(dstDir.GetPath())
	payload, err := io.ReadAll(stream)
	if err != nil {
		return err
	}
	if up != nil {
		up(100)
	}
	fullPath := normalizeTestPath(stdpath.Join(dirPath, stream.GetName()))
	s.items[fullPath] = &memoryDirEntry{path: fullPath, data: payload}
	return nil
}

func (s *memoryWriteStorage) Remove(ctx context.Context, obj model.Obj) error {
	targetPath := normalizeTestPath(obj.GetPath())
	for path := range s.items {
		if path == targetPath || strings.HasPrefix(path, ensureTestDirPrefix(targetPath)) {
			delete(s.items, path)
		}
	}
	return nil
}

func (s *memoryWriteStorage) objFromEntry(entry *memoryDirEntry) model.Obj {
	name := stdpath.Base(entry.path)
	if entry.path == "/" {
		name = "root"
	}
	return &model.Object{
		Name:     name,
		Path:     entry.path,
		Size:     int64(len(entry.data)),
		IsFolder: entry.isDir,
	}
}

func normalizeTestPath(p string) string {
	if p == "" {
		return "/"
	}
	return stdpath.Clean("/" + strings.TrimPrefix(p, "/"))
}

func ensureTestDirPrefix(p string) string {
	p = normalizeTestPath(p)
	if p == "/" {
		return "/"
	}
	return p + "/"
}

func TestMoveAcrossStoragesRefreshesDirectoryListings(t *testing.T) {
	srcRoot := t.TempDir()
	srcMount := "/src-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))

	require.NoError(t, os.MkdirAll(filepath.Join(srcRoot, "folder"), 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(srcRoot, "folder", "a.txt"), []byte("a"), 0o666))

	srcDriverAny, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: srcMount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + srcRoot + `"}`,
	})
	require.NoError(t, err)
	srcStorage := srcDriverAny

	_, err = op.List(context.Background(), srcStorage, "folder", model.ListArgs{})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(srcRoot, "folder", "b.txt"), []byte("b"), 0o666))

	dstStorage := newMemoryWriteStorage("/dst-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-")))
	task := &CopyTask{
		Operation:    FileOperationMove,
		SrcObjPath:   "/folder",
		DstDirPath:   "/target",
		SrcStorageMp: srcMount,
		DstStorageMp: dstStorage.GetStorage().MountPath,
	}
	task.SetCtx(context.Background())

	require.NoError(t, copyBetween2Storages(task, srcStorage, dstStorage, "/folder", "/target"))

	_, err = os.Stat(filepath.Join(srcRoot, "folder"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))

	first, ok := dstStorage.items["/target/folder/a.txt"]
	require.True(t, ok)
	require.Equal(t, []byte("a"), first.data)

	second, ok := dstStorage.items["/target/folder/b.txt"]
	require.True(t, ok)
	require.Equal(t, []byte("b"), second.data)
}
