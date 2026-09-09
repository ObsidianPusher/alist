package docs

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestReadSectionReturnsRequestedChunkAndNeighbor(t *testing.T) {
	svc := setupListService(t)
	result, err := svc.ReadSection(context.Background(), "guides/intro.md", "section-1", 1)
	require.NoError(t, err)
	require.Equal(t, "guides/intro.md", result.Path)
	require.Len(t, result.Chunks, 1)
	require.Equal(t, "intro", result.Chunks[0].Content)
}

func TestRebuildAsyncPublishesCompletedStatus(t *testing.T) {
	svc := setupListService(t)
	generation, err := svc.RebuildAsync(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, generation)
	require.Eventually(t, func() bool { return !svc.Status().Running }, time.Second, 10*time.Millisecond)
	require.Equal(t, generation, svc.Status().Generation)
	require.GreaterOrEqual(t, svc.Status().Documents, 2)
}

func TestRebuildAsyncPublishesEncodingMaintenanceCounts(t *testing.T) {
	svc := setupListService(t)
	raw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("钢渣外卖"))
	require.NoError(t, err)
	err = fs.PutDirectly(context.Background(), svc.config.DocsRoot, &stream.FileStream{
		Obj: &model.Object{Name: "legacy.md", Size: int64(len(raw))}, Reader: bytes.NewReader(raw), Mimetype: "text/markdown",
	})
	require.NoError(t, err)

	_, err = svc.RebuildAsync(context.Background())
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !svc.Status().Running }, time.Second, 10*time.Millisecond)
	status := svc.Status()
	require.GreaterOrEqual(t, status.Scanned, 3)
	require.Equal(t, 1, status.Normalized)
	require.Zero(t, status.Failed)
}

func TestUpdateSectionReplacesOnlySelectedMarkdownChunk(t *testing.T) {
	svc := setupListService(t)
	before, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.Update(context.Background(), "README.md", "# One\nfirst\n\n# Two\nsecond", &before.Revision)
	require.NoError(t, err)
	read, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.UpdateSection(context.Background(), "README.md", "section-2", "changed", read.Revision)
	require.NoError(t, err)
	updated, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	require.Contains(t, updated.Content, "first")
	require.Contains(t, updated.Content, "changed")
}

func TestContextCapsReturnedCharacters(t *testing.T) {
	svc := setupListService(t)
	read, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.Update(context.Background(), "README.md", "needle one\n\nneedle two", &read.Revision)
	require.NoError(t, err)
	context, err := svc.Context("needle", "", 10)
	require.NoError(t, err)
	require.LessOrEqual(t, len([]rune(context.Content)), 10)
}
