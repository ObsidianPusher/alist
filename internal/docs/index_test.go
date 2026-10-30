package docs

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIndexSearchFindsHeadingOnlyTerms(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	require.NoError(t, err)
	p := "smart-integrated-logistics-platform/docs/配置.md"
	require.NoError(t, idx.Upsert(p, "# 启动检查\n\nNACOS_ADDR 配置说明", "v1"))
	require.NoError(t, idx.Upsert("other/unrelated.md", "# 启动检查\n\n其他说明", "v1"))
	// Existing indexes already store headings; querying them must not need a rebuild.
	require.NoError(t, idx.Close())
	idx, err = OpenIndex(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	for _, mode := range []string{"all", "exact", "any"} {
		t.Run(mode, func(t *testing.T) {
			results, err := idx.Search("启动检查", "smart-integrated-logistics-platform", mode, 10)
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, p, results[0].Path)
			require.Contains(t, results[0].Heading, "启动检查")
		})
	}
}

func TestIndexAllCombinesHeadingAndBodyTerms(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Upsert("config.md", "# 启动检查\n\nNACOS_ADDR 配置说明", "v1"))
	results, err := idx.Search("启动检查 NACOS_ADDR", "", "all", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "config.md", results[0].Path)
}

func TestIndexHeadingChinesePhrasesRequireContinuity(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	for n, content := range []string{"# 启动，检查\n\n配置说明", "# 启 动 检 查\n\n配置说明", "# 启动\n\n检查", "# 启动检查\n\n配置说明"} {
		require.NoError(t, idx.Upsert(fmt.Sprintf("%d.md", n), content, "v1"))
	}
	for _, mode := range []string{"all", "exact"} {
		results, err := idx.Search("启动检查", "", mode, 1)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "3.md", results[0].Path)
	}
}

func TestIndexAllChineseRequiresOriginalTextContinuity(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	for n, content := range []string{"启 动 检 查", "启动，检查", "启\n动\n检\n查", strings.Repeat("配置说明。", 30) + "启动检查"} {
		require.NoError(t, idx.Upsert(fmt.Sprintf("%d.md", n), content, "v1"))
	}
	results, err := idx.Search("启动检查", "", "all", 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "3.md", results[0].Path)
}

func TestIndexRemovesMoreThanTenThousandLegacyChunks(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	type legacyDoc indexedDoc
	p := "smart-integrated-logistics-platform/config.md"
	batch := idx.index.NewBatch()
	for n := 0; n < 10001; n++ {
		require.NoError(t, batch.Index(fmt.Sprintf("%s#section-%d", p, n), legacyDoc{Path: p, Content: "oldneedle"}))
	}
	require.NoError(t, idx.index.Batch(batch))
	require.NoError(t, idx.Upsert(p, "currentneedle", "v2"))
	results, err := idx.Search("oldneedle", "", "all", 10)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestIndexSnippetSkipsIgnoredStopwords(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Upsert("config.md", "The introduction "+strings.Repeat("irrelevant ", 30)+"configure Nacos", "v1"))
	results, err := idx.Search("the configure", "", "all", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Contains(t, results[0].Snippet, "configure")
}

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

func TestIndexContentSearchPreservesBusinessPathPrefix(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	p := "smart-integrated-logistics-platform/docs/kb/10-结算settle/维修对账单与维修应收生成链路.md"
	require.NoError(t, idx.Upsert(p, "NACOS_ADDR NACOS_NS bootstrap.yml", "v1"))
	for _, mode := range []string{"all", "any", "exact", "path"} {
		for _, prefix := range []string{"", "smart-integrated-logistics-platform", "smart-integrated-logistics-platform/docs/kb/10-结算settle/"} {
			q := "bootstrap.yml"
			if mode == "all" {
				q = "NACOS_ADDR NACOS_NS"
			} else if mode == "path" {
				q = p
			}
			results, err := idx.Search(q, prefix, mode, 10)
			require.NoError(t, err)
			require.Len(t, results, 1, "mode=%s prefix=%s", mode, prefix)
			require.Equal(t, p, results[0].Path)
		}
	}
	require.NoError(t, idx.Delete(p))
	results, err := idx.Search("NACOS_ADDR", "", "all", 10)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestIndexRebuildRepairsExistingPathMapping(t *testing.T) {
	dir := t.TempDir()
	idx, err := OpenIndex(dir)
	require.NoError(t, err)
	p := "smart-integrated-logistics-platform/docs/config.md"
	// The legacy writer did not select the doc mapping.
	type legacyDoc indexedDoc
	require.NoError(t, idx.index.Index(p+"#section-1", legacyDoc{Path: p, Content: "NACOS_ADDR NACOS_NS"}))
	require.NoError(t, idx.Close())
	idx, err = OpenIndex(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Upsert(p, "NACOS_ADDR NACOS_NS", "v2"))
	results, err := idx.Search("NACOS_ADDR NACOS_NS", "smart-integrated-logistics-platform", "all", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "v2", results[0].Revision)
}

func TestIndexRemovesLegacyChunksWithoutDeletingSibling(t *testing.T) {
	for _, action := range []string{"replace", "delete"} {
		t.Run(action, func(t *testing.T) {
			idx, err := OpenIndex(t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, idx.Close()) })
			type legacyDoc indexedDoc
			p := "smart-integrated-logistics-platform/docs/config.md"
			sibling := "smart-integrated-logistics-platform/docs/config-md"
			for _, path := range []string{p, sibling} {
				for _, id := range []string{"section-1", "section-2"} {
					require.NoError(t, idx.index.Index(path+"#"+id, legacyDoc{Path: path, Content: "legacyneedle", Revision: "v1"}))
				}
			}
			if action == "replace" {
				require.NoError(t, idx.Upsert(p, "currentneedle", "v2"))
			} else {
				require.NoError(t, idx.Delete(p))
			}
			results, err := idx.Search("legacyneedle", "", "all", 10)
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.Equal(t, sibling, results[0].Path)
		})
	}
}

func TestIndexAllRequiresEachChinesePhrase(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Upsert("config.md", strings.Repeat("配置说明。", 30)+"启动检查 NACOS_ADDR", "v1"))
	require.NoError(t, idx.Upsert("unrelated.md", "启动服务，然后检查状态 NACOS_ADDR", "v1"))
	results, err := idx.Search("启动检查 NACOS_ADDR", "", "all", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "config.md", results[0].Path)
	require.Contains(t, results[0].Snippet, "启动检查")
}

func TestIndexAllEmptyQueryReturnsNoResults(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Upsert("config.md", "configuration", "v1"))
	results, err := idx.Search(" \t ", "", "all", 10)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestIndexAllIgnoresEnglishStopwords(t *testing.T) {
	idx, err := OpenIndex(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, idx.Close()) })
	require.NoError(t, idx.Upsert("config.md", "how to configure Nacos", "v1"))
	results, err := idx.Search("how to configure", "", "all", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "config.md", results[0].Path)
	results, err = idx.Search("how to the", "", "all", 10)
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
