package webdav

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/alist-org/alist/v3/drivers/local"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type webdavFakeStorage struct {
	model.Storage
	renameCalls int
	moveCalls   int
	copyCalls   int
	root        *model.Object
	items       map[string]*model.Object
	delayedMove delayedMutation
}

type delayedMutation struct {
	srcPath        string
	dstPath        string
	pendingReads   int
	remainingReads int
}

func newWebdavFakeStorage() *webdavFakeStorage {
	root := &model.Object{Path: "/", Name: "root", IsFolder: true}
	parent := &model.Object{Path: "/dir", Name: "dir", IsFolder: true}
	src := &model.Object{Path: "/dir/source.txt", Name: "source.txt"}
	return &webdavFakeStorage{
		root: root,
		items: map[string]*model.Object{
			"/":               root,
			"/dir":            parent,
			"/dir/source.txt": src,
		},
	}
}

func (s *webdavFakeStorage) Config() driver.Config            { return driver.Config{Name: "webdav-fake"} }
func (s *webdavFakeStorage) GetAddition() driver.Additional   { return nil }
func (s *webdavFakeStorage) Init(ctx context.Context) error   { return nil }
func (s *webdavFakeStorage) Drop(ctx context.Context) error   { return nil }
func (s *webdavFakeStorage) SetStorage(storage model.Storage) { s.Storage = storage }
func (s *webdavFakeStorage) GetStorage() *model.Storage       { return &s.Storage }
func (s *webdavFakeStorage) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	res := make([]model.Obj, 0)
	dirPath := dir.GetPath()
	for p, obj := range s.items {
		if p == "/" || p == dirPath {
			continue
		}
		if filepath.Dir(p) == dirPath {
			cloned := *obj
			res = append(res, &cloned)
		}
	}
	return res, nil
}
func (s *webdavFakeStorage) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	return &model.Link{URL: "https://example.invalid/file"}, nil
}
func (s *webdavFakeStorage) Get(ctx context.Context, path string) (model.Obj, error) {
	if s.delayedMove.remainingReads > 0 && (path == s.delayedMove.srcPath || path == s.delayedMove.dstPath) {
		s.delayedMove.remainingReads--
		if s.delayedMove.remainingReads == 0 {
			if obj, ok := s.items[s.delayedMove.srcPath]; ok {
				cloned := *obj
				cloned.Path = s.delayedMove.dstPath
				cloned.Name = filepath.Base(s.delayedMove.dstPath)
				delete(s.items, s.delayedMove.srcPath)
				s.items[s.delayedMove.dstPath] = &cloned
			}
		}
	}
	if obj, ok := s.items[path]; ok {
		cloned := *obj
		return &cloned, nil
	}
	return nil, os.ErrNotExist
}
func (s *webdavFakeStorage) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	s.renameCalls++
	srcPath := srcObj.GetPath()
	if s.delayedMove.pendingReads > 0 {
		s.delayedMove.srcPath = srcPath
		s.delayedMove.dstPath = filepath.Join(filepath.Dir(srcPath), newName)
		s.delayedMove.remainingReads = s.delayedMove.pendingReads
		return &model.Object{Path: s.delayedMove.dstPath, Name: newName}, nil
	}
	obj := s.items[srcPath]
	delete(s.items, srcPath)
	cloned := *obj
	cloned.Name = newName
	cloned.Path = filepath.Join(filepath.Dir(srcPath), newName)
	s.items[cloned.Path] = &cloned
	return &cloned, nil
}
func (s *webdavFakeStorage) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	s.copyCalls++
	obj := s.items[srcObj.GetPath()]
	if obj == nil {
		return nil, os.ErrNotExist
	}
	cloned := *obj
	cloned.Path = filepath.Join(dstDir.GetPath(), srcObj.GetName())
	cloned.Name = srcObj.GetName()
	s.items[cloned.Path] = &cloned
	return &cloned, nil
}
func (s *webdavFakeStorage) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	s.moveCalls++
	return nil, fmt.Errorf("unexpected move call")
}

type webdavNoCopyStorage struct {
	*webdavFakeStorage
}

func (s *webdavNoCopyStorage) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotImplement
}

type webdavCacheOnlyStorage struct {
	model.Storage
	renameCalls int
	items       map[string]*model.Object
}

func newWebdavCacheOnlyStorage() *webdavCacheOnlyStorage {
	root := &model.Object{Path: "/", Name: "root", IsFolder: true}
	parent := &model.Object{Path: "/dir", Name: "dir", IsFolder: true}
	src := &model.Object{Path: "/dir/source.txt", Name: "source.txt"}
	return &webdavCacheOnlyStorage{
		items: map[string]*model.Object{
			"/":               root,
			"/dir":            parent,
			"/dir/source.txt": src,
		},
	}
}

func (s *webdavCacheOnlyStorage) Config() driver.Config {
	return driver.Config{Name: "webdav-cache-only"}
}
func (s *webdavCacheOnlyStorage) GetAddition() driver.Additional   { return nil }
func (s *webdavCacheOnlyStorage) Init(ctx context.Context) error   { return nil }
func (s *webdavCacheOnlyStorage) Drop(ctx context.Context) error   { return nil }
func (s *webdavCacheOnlyStorage) SetStorage(storage model.Storage) { s.Storage = storage }
func (s *webdavCacheOnlyStorage) GetStorage() *model.Storage       { return &s.Storage }
func (s *webdavCacheOnlyStorage) GetRoot(ctx context.Context) (model.Obj, error) {
	if obj, ok := s.items["/"]; ok {
		cloned := *obj
		return &cloned, nil
	}
	return nil, os.ErrNotExist
}
func (s *webdavCacheOnlyStorage) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	res := make([]model.Obj, 0)
	dirPath := dir.GetPath()
	for p, obj := range s.items {
		if p == "/" || p == dirPath {
			continue
		}
		if filepath.Dir(p) == dirPath {
			cloned := *obj
			res = append(res, &cloned)
		}
	}
	return res, nil
}
func (s *webdavCacheOnlyStorage) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	return &model.Link{URL: "https://example.invalid/file"}, nil
}
func (s *webdavCacheOnlyStorage) Rename(ctx context.Context, srcObj model.Obj, newName string) (model.Obj, error) {
	s.renameCalls++
	srcPath := srcObj.GetPath()
	obj := s.items[srcPath]
	delete(s.items, srcPath)
	cloned := *obj
	cloned.Name = newName
	cloned.Path = filepath.Join(filepath.Dir(srcPath), newName)
	s.items[cloned.Path] = &cloned
	return &cloned, nil
}
func (s *webdavCacheOnlyStorage) Move(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotImplement
}
func (s *webdavCacheOnlyStorage) Copy(ctx context.Context, srcObj, dstDir model.Obj) (model.Obj, error) {
	return nil, errs.NotImplement
}

var webdavTestOnce sync.Once

func initWebdavTestEnv() {
	webdavTestOnce.Do(func() {
		dB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
		if err != nil {
			panic(fmt.Sprintf("failed to connect database: %v", err))
		}
		conf.Conf = conf.DefaultConfig()
		db.Init(dB)
	})
}

func testMountPath(t *testing.T) string {
	t.Helper()
	base := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	return "/" + base + "-" + fmt.Sprint(time.Now().UnixNano())
}

func createLocalStorage(t *testing.T, root string) (uint, string) {
	t.Helper()
	initWebdavTestEnv()
	mountPath := testMountPath(t)
	storage := model.Storage{
		Driver:    "Local",
		MountPath: mountPath,
		Addition:  fmt.Sprintf(`{"root_folder_path":%q}`, root),
	}
	id, err := op.CreateStorage(context.Background(), storage)
	if err != nil {
		t.Fatalf("create storage: %v", err)
	}
	t.Cleanup(func() {
		_ = op.DeleteStorageById(context.Background(), id)
	})
	return id, mountPath
}

func TestCopyFilesUsesDestinationNameAndOverwrite(t *testing.T) {
	root := t.TempDir()
	_, mountPath := createLocalStorage(t, root)

	srcPath := filepath.Join(root, "source.txt")
	dstPath := filepath.Join(root, "target.txt")
	if err := os.WriteFile(srcPath, []byte("source-content"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(dstPath, []byte("old-content"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	ctx := context.Background()
	status, err := copyFiles(ctx, filepath.Join(mountPath, "source.txt"), filepath.Join(mountPath, "target.txt"), false)
	if status != http.StatusPreconditionFailed || err == nil {
		t.Fatalf("copy overwrite=false: status=%d err=%v", status, err)
	}

	status, err = copyFiles(ctx, filepath.Join(mountPath, "source.txt"), filepath.Join(mountPath, "target.txt"), true)
	if status != http.StatusCreated || err != nil {
		t.Fatalf("copy overwrite=true: status=%d err=%v", status, err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "source-content" {
		t.Fatalf("unexpected target content: %q", got)
	}
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("source should remain after copy: %v", err)
	}
}

func TestMoveFilesUsesDestinationNameAndOverwrite(t *testing.T) {
	root := t.TempDir()
	_, mountPath := createLocalStorage(t, root)

	srcPath := filepath.Join(root, "source.txt")
	dstPath := filepath.Join(root, "target.txt")
	if err := os.WriteFile(srcPath, []byte("source-content"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(dstPath, []byte("old-content"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	ctx := context.Background()
	status, err := moveFiles(ctx, filepath.Join(mountPath, "source.txt"), filepath.Join(mountPath, "target.txt"), false)
	if status != http.StatusPreconditionFailed || err == nil {
		t.Fatalf("move overwrite=false: status=%d err=%v", status, err)
	}

	status, err = moveFiles(ctx, filepath.Join(mountPath, "source.txt"), filepath.Join(mountPath, "target.txt"), true)
	if status != http.StatusCreated || err != nil {
		t.Fatalf("move overwrite=true: status=%d err=%v", status, err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "source-content" {
		t.Fatalf("unexpected target content: %q", got)
	}
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatalf("source should be removed after move: %v", err)
	}
}

func TestCopyOrMoveWithinStoragePrefersRenameForSameDirMove(t *testing.T) {
	storage := newWebdavFakeStorage()

	status, err := copyOrMoveWithinStorage(context.Background(), storage, "/dir/source.txt", "/dir/target.txt", true, true)
	if status != http.StatusCreated || err != nil {
		t.Fatalf("move within storage: status=%d err=%v", status, err)
	}
	if storage.renameCalls != 1 {
		t.Fatalf("expected rename to be called once, got %d", storage.renameCalls)
	}
	if storage.moveCalls != 0 {
		t.Fatalf("expected move to not be called, got %d", storage.moveCalls)
	}
	if _, ok := storage.items["/dir/target.txt"]; !ok {
		t.Fatalf("target file not found after rename")
	}
	if _, ok := storage.items["/dir/source.txt"]; ok {
		t.Fatalf("source file should not exist after rename")
	}
}

func TestCopyOrMoveWithinStorageWaitsForMoveEffect(t *testing.T) {
	storage := newWebdavFakeStorage()
	storage.delayedMove.pendingReads = 2

	status, err := copyOrMoveWithinStorage(context.Background(), storage, "/dir/source.txt", "/dir/target.txt", true, true)
	if status != http.StatusCreated || err != nil {
		t.Fatalf("move within storage with delayed visibility: status=%d err=%v", status, err)
	}
	if _, ok := storage.items["/dir/target.txt"]; !ok {
		t.Fatalf("target file not found after delayed rename")
	}
	if _, ok := storage.items["/dir/source.txt"]; ok {
		t.Fatalf("source file should not exist after delayed rename")
	}
}

func TestCopyOrMoveWithinStorageReturnsNotImplementedForUnsupportedCopy(t *testing.T) {
	storage := &webdavNoCopyStorage{webdavFakeStorage: newWebdavFakeStorage()}

	status, err := copyOrMoveWithinStorage(context.Background(), storage, "/dir/source.txt", "/dir/copied.txt", false, true)
	if status != http.StatusNotImplemented {
		t.Fatalf("expected 501 for unsupported copy, got status=%d err=%v", status, err)
	}
	if err == nil {
		t.Fatalf("expected error for unsupported copy")
	}
}

func TestCopyOrMoveWithinStorageClearsCacheBeforeVerification(t *testing.T) {
	storage := newWebdavCacheOnlyStorage()

	status, err := copyOrMoveWithinStorage(context.Background(), storage, "/dir/source.txt", "/dir/target.txt", true, true)
	if status != http.StatusCreated || err != nil {
		t.Fatalf("move within storage with cache-only backend: status=%d err=%v", status, err)
	}
	if storage.renameCalls != 1 {
		t.Fatalf("expected rename to be called once, got %d", storage.renameCalls)
	}
	if _, ok := storage.items["/dir/target.txt"]; !ok {
		t.Fatalf("target file not found after rename")
	}
	if _, ok := storage.items["/dir/source.txt"]; ok {
		t.Fatalf("source file should not exist after rename")
	}
}
