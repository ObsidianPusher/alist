package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTempTestFile(t *testing.T, dir, name string, modTime time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes file %s: %v", path, err)
	}
	return path
}

func TestCleanupAListTempFilesForce(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTempTestFile(t, dir, "file-1", now)
	writeTempTestFile(t, dir, "thumb-video-1", now)
	writeTempTestFile(t, dir, "thumb-video-seg-1", now)
	writeTempTestFile(t, dir, "keep.txt", now)
	if err := os.Mkdir(filepath.Join(dir, "thumbs"), 0o755); err != nil {
		t.Fatalf("mkdir thumbs: %v", err)
	}
	writeTempTestFile(t, filepath.Join(dir, "thumbs"), "cache.jpg", now)

	cleanupAListTempFiles(dir, 24*time.Hour, true)

	for _, name := range []string{"file-1", "thumb-video-1", "thumb-video-seg-1"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("expected keep.txt kept, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "thumbs", "cache.jpg")); err != nil {
		t.Fatalf("expected thumbs/cache.jpg kept, err=%v", err)
	}
}

func TestCleanupAListTempFilesByRetention(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	oldTime := now.Add(-25 * time.Hour)
	writeTempTestFile(t, dir, "file-old", oldTime)
	writeTempTestFile(t, dir, "file-new", now)
	writeTempTestFile(t, dir, "keep.txt", oldTime)
	if err := os.Mkdir(filepath.Join(dir, "thumbs"), 0o755); err != nil {
		t.Fatalf("mkdir thumbs: %v", err)
	}
	writeTempTestFile(t, filepath.Join(dir, "thumbs"), "cache.jpg", oldTime)

	cleanupAListTempFiles(dir, 24*time.Hour, false)

	if _, err := os.Stat(filepath.Join(dir, "file-old")); !os.IsNotExist(err) {
		t.Fatalf("expected file-old removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "file-new")); err != nil {
		t.Fatalf("expected file-new kept, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("expected keep.txt kept, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "thumbs", "cache.jpg")); err != nil {
		t.Fatalf("expected thumbs/cache.jpg kept, err=%v", err)
	}
}
