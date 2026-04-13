package handles

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrecheckLocalArchivePassword_ZipEncrypted(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, osWriteFile(src, "zip-password-check"))

	archive := filepath.Join(tmp, "a.zip")
	require.NoError(t, createLocalZipArchive(context.Background(), []string{src}, []string{"a.txt"}, archive, 5, "123456"))

	require.NoError(t, precheckLocalArchivePassword(context.Background(), archive, "123456"))

	err := precheckLocalArchivePassword(context.Background(), archive, "bad-pass")
	require.Error(t, err)
	reqErr := &localBrowserRequestError{}
	require.ErrorAs(t, err, &reqErr)
	require.Contains(t, reqErr.Error(), "zip")
}

func TestPrecheckLocalArchivePassword_ZipEncrypted_EmptyPassword(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, osWriteFile(src, "zip-password-empty"))

	archive := filepath.Join(tmp, "a.zip")
	require.NoError(t, createLocalZipArchive(context.Background(), []string{src}, []string{"a.txt"}, archive, 5, "123456"))

	err := precheckLocalArchivePassword(context.Background(), archive, "")
	require.Error(t, err)
	reqErr := &localBrowserRequestError{}
	require.ErrorAs(t, err, &reqErr)
	require.Contains(t, reqErr.Error(), "zip")
}
