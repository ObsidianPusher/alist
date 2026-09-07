package docs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
