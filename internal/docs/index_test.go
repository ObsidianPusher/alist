package docs

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIndexReplacesDocumentWithHeadingAwareChunks(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Replace("guide.md", "v1", time.Now(), ChunkMarkdown("# Guide\n\nfirst needle\n\n## Install\nsecond needle", 1200)))
	results, err := idx.Search("needle", "", "smart", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "guide.md", results[0].Path)
}

func TestIndexDeleteRemovesAllChunks(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	content := strings.Repeat("# Section\nneedle\n\n", 20)
	require.NoError(t, idx.Replace("guide.md", "v1", time.Now(), ChunkMarkdown(content, 20)))
	require.NoError(t, idx.Delete("guide.md"))
	results, err := idx.Search("needle", "", "any", 10)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestIndexExactAndPathQueriesUseLiteralSemantics(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Replace("guides/production-reconciliation.md", "v1", time.Now(), ChunkMarkdown("生产对账规则", 1200)))
	exact, err := idx.Search("生产对账", "", "exact", 10)
	require.NoError(t, err)
	require.Len(t, exact, 1)
	pathResults, err := idx.Search("guides/production-reconciliation.md", "", "path", 10)
	require.NoError(t, err)
	require.Len(t, pathResults, 1)
}
