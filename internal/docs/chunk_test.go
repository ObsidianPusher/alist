package docs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChunkMarkdownUsesHeadingBreadcrumbAndParagraphBoundaries(t *testing.T) {
	chunks := ChunkMarkdown("# Guide\nintro\n\n## Install\nfirst\n\nsecond", 12)
	require.Len(t, chunks, 3)
	require.Equal(t, "Guide", chunks[0].Heading)
	require.Equal(t, "intro", chunks[0].Content)
	require.Equal(t, "Guide / Install", chunks[1].Heading)
	require.Equal(t, "first", chunks[1].Content)
	require.Equal(t, "second", chunks[2].Content)
	require.Equal(t, "section-2", chunks[1].ID)
}
