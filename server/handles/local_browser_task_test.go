package handles

import (
	"context"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalBrowserCompressTaskRun_CreatesArchive(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, os.WriteFile(src, []byte("compress-task-ok"), 0o644))

	dstAbs := filepath.Join(tmp, "archive.zip")
	task := &localBrowserCompressTask{
		DirPath:  "/",
		DstPath:  "/archive.zip",
		DstAbs:   dstAbs,
		SrcAbs:   []string{src},
		SrcNames: []string{"a.txt"},
		Level:    5,
		Status:   "compress: queued",
	}
	task.SetCtx(context.Background())

	require.NoError(t, task.Run())
	require.Equal(t, "compress: done", task.GetStatus())

	content, err := os.ReadFile(dstAbs)
	require.NoError(t, err)
	require.NotEmpty(t, content)
}
