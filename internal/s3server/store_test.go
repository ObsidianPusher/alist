package s3server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alist-org/alist/v3/cmd/flags"
)

func TestCleanupStoredObjectMetadata(t *testing.T) {
	oldDataDir := flags.DataDir
	t.Cleanup(func() {
		flags.DataDir = oldDataDir
	})

	dataDir := t.TempDir()
	flags.DataDir = dataDir

	validObject := filepath.Join(dataDir, "bucket", "keep.txt")
	if err := os.MkdirAll(filepath.Dir(validObject), 0o755); err != nil {
		t.Fatalf("mkdir valid object parent: %v", err)
	}
	if err := os.WriteFile(validObject, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write valid object: %v", err)
	}
	validMeta := storedObjectMetadata{
		BackendPath: validObject,
		ETag:        `"abcd"`,
		Size:        4,
		ModTimeUnix: 1,
	}
	if err := saveStoredObjectMetadata("", validMeta); err != nil {
		t.Fatalf("save valid meta: %v", err)
	}
	validMetaFile := objectMetaStoreFilePath(validObject)

	orphanMetaFile := objectMetaStoreFilePath(filepath.Join(dataDir, "bucket", "missing.txt"))
	if err := os.MkdirAll(filepath.Dir(orphanMetaFile), 0o755); err != nil {
		t.Fatalf("mkdir orphan meta parent: %v", err)
	}
	orphanMeta := storedObjectMetadata{
		BackendPath: filepath.Join(dataDir, "bucket", "missing.txt"),
		ETag:        `"orphan"`,
	}
	orphanBytes, err := json.Marshal(orphanMeta)
	if err != nil {
		t.Fatalf("marshal orphan meta: %v", err)
	}
	if err := os.WriteFile(orphanMetaFile, orphanBytes, 0o644); err != nil {
		t.Fatalf("write orphan meta: %v", err)
	}

	legacyRoot := filepath.Join(dataDir, "legacy-root")
	legacyDir := filepath.Join(legacyRoot, s3MetaDirName)
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "old.json"), []byte(`{"backend_path":"`+validObject+`"}`), 0o644); err != nil {
		t.Fatalf("write legacy meta: %v", err)
	}

	cfg := &Config{
		Buckets: map[string]bucketMappingTarget{
			"b": {localRootPath: legacyRoot},
		},
	}
	if err := cleanupStoredObjectMetadata(cfg); err != nil {
		t.Fatalf("cleanupStoredObjectMetadata error: %v", err)
	}

	if _, err := os.Stat(validMetaFile); err != nil {
		t.Fatalf("valid meta should remain: %v", err)
	}
	if _, err := os.Stat(orphanMetaFile); !os.IsNotExist(err) {
		t.Fatalf("orphan meta should be removed, got err=%v", err)
	}
	if _, err := os.Stat(legacyDir); !os.IsNotExist(err) {
		t.Fatalf("legacy meta dir should be removed, got err=%v", err)
	}
}
