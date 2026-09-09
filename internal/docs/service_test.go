package docs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/alist-org/alist/v3/drivers/local"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupListService(t *testing.T) *Service {
	t.Helper()
	conf.Conf = conf.DefaultConfig()
	conf.Conf.Database.Type = "sqlite3"
	database, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	db.Init(database)

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "guides"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("root"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "guides", "intro.md"), []byte("intro"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "guides", "config.yaml"), []byte("enabled: true"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "guides", "archive.zip"), []byte("ignored"), 0o644))

	mount := "/docs-" + t.Name()
	_, err = op.CreateStorage(context.Background(), model.Storage{
		MountPath: mount,
		Driver:    "Local",
		Addition:  `{"root_folder_path":"` + root + `"}`,
	})
	require.NoError(t, err)

	svc, err := New(conf.MCPConfig{DocsRoot: mount, IndexDir: filepath.Join(t.TempDir(), "index"), MaxReadBytes: 1024, MaxWriteBytes: 1024, HistoryLimit: 1})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, svc.Close()) })
	return svc
}

func TestListRecursivelyReturnsOnlySupportedDocuments(t *testing.T) {
	svc := setupListService(t)

	result, err := svc.List(context.Background(), "", "", 100)
	require.NoError(t, err)
	require.Equal(t, []string{"README.md", "guides/intro.md"}, documentPaths(result.Items))
	require.Equal(t, []int64{4, 5}, documentSizes(result.Items))
	for _, item := range result.Items {
		require.False(t, item.Modified.IsZero())
	}
	require.Empty(t, result.NextCursor)
}

func TestListPaginatesAfterCursor(t *testing.T) {
	svc := setupListService(t)

	first, err := svc.List(context.Background(), "", "", 2)
	require.NoError(t, err)
	require.Equal(t, []string{"README.md", "guides/intro.md"}, documentPaths(first.Items))
	require.Empty(t, first.NextCursor)

	second, err := svc.List(context.Background(), "", "README.md", 2)
	require.NoError(t, err)
	require.Equal(t, []string{"guides/intro.md"}, documentPaths(second.Items))
}

func TestReplaceEditsUniqueTextWithoutSendingWholeDocument(t *testing.T) {
	svc := setupListService(t)
	read, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	next, err := svc.Replace(context.Background(), "README.md", "root", "updated", read.Revision)
	require.NoError(t, err)
	require.NotEqual(t, read.Revision, next)
	changed, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	require.Equal(t, "updated", changed.Content)
}

func TestReadTruncatesUTF8AtCharacterBoundary(t *testing.T) {
	svc := setupListService(t)
	before, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.Update(context.Background(), "README.md", "a益", &before.Revision)
	require.NoError(t, err)

	page, err := svc.Read(context.Background(), "README.md", 0, 2)
	require.NoError(t, err)
	require.Equal(t, "a", page.Content)
	require.True(t, page.Truncated)
	require.EqualValues(t, 1, page.NextOffset)

	next, err := svc.Read(context.Background(), "README.md", page.NextOffset, 3)
	require.NoError(t, err)
	require.Equal(t, "益", next.Content)
}

func TestReadPaginatesCanonicalizedGB18030Document(t *testing.T) {
	svc := setupListService(t)
	raw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("钢渣外卖"))
	require.NoError(t, err)
	err = fs.PutDirectly(context.Background(), svc.config.DocsRoot, &stream.FileStream{
		Obj:      &model.Object{Name: "legacy.md", Size: int64(len(raw))},
		Reader:   bytes.NewReader(raw),
		Mimetype: "text/markdown",
	})
	require.NoError(t, err)

	page, err := svc.Read(context.Background(), "legacy.md", 0, 3)
	require.NoError(t, err)
	require.Equal(t, "钢", page.Content)
	require.True(t, page.Truncated)
	require.EqualValues(t, len([]byte("钢")), page.NextOffset)

	next, err := svc.Read(context.Background(), "legacy.md", page.NextOffset, 6)
	require.NoError(t, err)
	require.Equal(t, "渣外", next.Content)
}

func TestHistoryReturnsCanonicalizedNormalizationSnapshot(t *testing.T) {
	svc := setupListService(t)
	raw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("钢渣外卖"))
	require.NoError(t, err)
	err = fs.PutDirectly(context.Background(), svc.config.DocsRoot, &stream.FileStream{
		Obj:      &model.Object{Name: "legacy-history.md", Size: int64(len(raw))},
		Reader:   bytes.NewReader(raw),
		Mimetype: "text/markdown",
	})
	require.NoError(t, err)

	_, err = svc.Read(context.Background(), "legacy-history.md", 0, 0)
	require.NoError(t, err)
	history, err := svc.History("legacy-history.md", revision(raw), true)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "钢渣外卖", history[0].Content)
}

func TestReplaceRejectsAmbiguousText(t *testing.T) {
	svc := setupListService(t)
	before, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.Update(context.Background(), "README.md", "repeat repeat", &before.Revision)
	require.NoError(t, err)
	read, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.Replace(context.Background(), "README.md", "repeat", "changed", read.Revision)
	require.Error(t, err)
}

func TestOutlineAndAppendAvoidFullDocumentEdits(t *testing.T) {
	svc := setupListService(t)
	before, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.Update(context.Background(), "README.md", "# Guide\nintro", &before.Revision)
	require.NoError(t, err)
	outline, err := svc.Outline(context.Background(), "README.md")
	require.NoError(t, err)
	require.Equal(t, "Guide", outline.Sections[0].Heading)
	require.Equal(t, len("intro"), outline.Sections[0].Chars)
	_, err = svc.Append(context.Background(), "README.md", "section-1", "more", outline.Revision)
	require.NoError(t, err)
}

func TestInsertAfterEditsWithUniqueAnchor(t *testing.T) {
	svc := setupListService(t)
	read, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	_, err = svc.InsertAfter(context.Background(), "README.md", "root", " added", read.Revision)
	require.NoError(t, err)
	changed, err := svc.Read(context.Background(), "README.md", 0, 0)
	require.NoError(t, err)
	require.Equal(t, "root added", changed.Content)
}

func documentPaths(items []DocumentInfo) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.Path)
	}
	return paths
}

func documentSizes(items []DocumentInfo) []int64 {
	sizes := make([]int64, 0, len(items))
	for _, item := range items {
		sizes = append(sizes, item.Size)
	}
	return sizes
}
