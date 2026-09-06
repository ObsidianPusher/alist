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
