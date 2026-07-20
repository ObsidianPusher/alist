package op

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/model"
	_ "github.com/alist-org/alist/v3/drivers/local"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	dB, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	conf.Conf = conf.DefaultConfig()
	db.Init(dB)
}

func TestResolveBucketLocalPathUsesLongestMatchingMount(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootA, "data", "deep"), 0o755); err != nil {
		t.Fatalf("mkdir root a path: %v", err)
	}

	storageA := model.Storage{
		Driver:    "Local",
		MountPath: "/b",
		Addition:  `{"root_folder_path":"` + rootA + `","thumbnail":false,"show_hidden":true,"mkdir_perm":"777","thumb_concurrency":"1","recycle_bin_path":"delete permanently"}`,
	}
	storageB := model.Storage{
		Driver:    "Local",
		MountPath: "/b/data",
		Addition:  `{"root_folder_path":"` + rootB + `","thumbnail":false,"show_hidden":true,"mkdir_perm":"777","thumb_concurrency":"1","recycle_bin_path":"delete permanently"}`,
	}
	if _, err := CreateStorage(context.Background(), storageA); err != nil {
		t.Fatalf("create storage a: %v", err)
	}
	if _, err := CreateStorage(context.Background(), storageB); err != nil {
		t.Fatalf("create storage b: %v", err)
	}

	got, err := ResolveBucketLocalPath("/b/data/deep/note.txt")
	if err != nil {
		t.Fatalf("resolve path: %v", err)
	}
	want := filepath.Join(rootB, "deep", "note.txt")
	if got != want {
		t.Fatalf("unexpected resolved path: got %q want %q", got, want)
	}
}

func TestResolveBucketLocalPathKeepsAbsolutePathUnchanged(t *testing.T) {
	root := t.TempDir()
	storage := model.Storage{
		Driver:    "Local",
		MountPath: "/mnt",
		Addition:  `{"root_folder_path":"` + root + `","thumbnail":false,"show_hidden":true,"mkdir_perm":"777","thumb_concurrency":"1","recycle_bin_path":"delete permanently"}`,
	}
	if _, err := CreateStorage(context.Background(), storage); err != nil {
		t.Fatalf("create storage: %v", err)
	}

	absPath := filepath.Join(root, "dm-1", ".ugreen_nas", "830402", "data", "goclaw", "runtime", "workspace", "mini", "ws", "system", "obsidian")
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		t.Fatalf("mkdir abs path: %v", err)
	}
	got, err := ResolveBucketLocalPath(absPath)
	if err != nil {
		t.Fatalf("resolve absolute path: %v", err)
	}
	want, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("unexpected absolute path: got %q want %q", got, filepath.Clean(want))
	}
}
