package local

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackendPathResolvesToRootPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "storage-root")
	d := &Local{}
	d.Addition.RootFolderPath = root

	require.Equal(t, root, d.BackendPath("/"))
	require.Equal(t, filepath.Join(root, "folder", "file.txt"), d.BackendPath("/folder/file.txt"))
	require.Equal(t, filepath.Join(root, "folder", "nested"), d.BackendPath("folder/nested"))
}
