package fs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/stretchr/testify/require"
)

func TestListAcceptsNilArgs(t *testing.T) {
	root := t.TempDir()
	mount := "/list-nil-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "folder"), 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(root, "folder", "file.txt"), []byte("hello"), 0o666))

	_, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: mount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + root + `"}`,
	})
	require.NoError(t, err)

	objs, err := List(context.Background(), mount+"/folder", nil)
	require.NoError(t, err)
	require.Len(t, objs, 1)
	require.Equal(t, "file.txt", objs[0].GetName())
}

func TestGetAcceptsNilArgs(t *testing.T) {
	root := t.TempDir()
	mount := "/get-nil-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))

	require.NoError(t, os.MkdirAll(filepath.Join(root, "folder"), 0o777))
	require.NoError(t, os.WriteFile(filepath.Join(root, "folder", "file.txt"), []byte("hello"), 0o666))

	_, err := op.CreateStorage(context.Background(), model.Storage{
		MountPath: mount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + root + `"}`,
	})
	require.NoError(t, err)

	obj, err := Get(context.Background(), mount+"/folder/file.txt", nil)
	require.NoError(t, err)
	require.Equal(t, "file.txt", obj.GetName())
}
