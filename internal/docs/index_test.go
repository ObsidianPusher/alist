package docs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIndexReplacesDocumentWithHeadingAwareChunks(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Replace("guide.md", "v1", time.Now(), ChunkMarkdown("# Guide\n\nfirst needle\n\n## Install\nsecond needle", 1200)))
	results, err := idx.Search("needle", "", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)
	var install SearchResult
	for _, result := range results {
		if result.SectionID == "section-2" {
			install = result
		}
	}
	require.Equal(t, "Guide / Install", install.Heading)
}
