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
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
