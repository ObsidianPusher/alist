package handles

import (
	stdzip "archive/zip"
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectLocalBrowserArchiveMeta(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		fileName       string
		expectFormat   string
		expectGroup    string
		expectParted   bool
		expectInput    string
		expectCanExtra bool
	}{
		{
			name:           "zip",
			fileName:       "pack.zip",
			expectFormat:   localBrowserArchiveFormatZip,
			expectGroup:    "pack.zip",
			expectCanExtra: true,
		},
		{
			name:           "rar part",
			fileName:       "movie.part03.rar",
			expectFormat:   localBrowserArchiveFormatRar,
			expectGroup:    "movie.rar",
			expectParted:   true,
			expectInput:    "movie.part01.rar",
			expectCanExtra: true,
		},
		{
			name:           "7z part",
			fileName:       "logs.7z.004",
			expectFormat:   localBrowserArchiveFormat7z,
			expectGroup:    "logs.7z",
			expectParted:   true,
			expectInput:    "logs.7z.001",
			expectCanExtra: true,
		},
		{
			name:           "zip z01",
			fileName:       "backup.z01",
			expectFormat:   localBrowserArchiveFormatZip,
			expectGroup:    "backup.zip",
			expectParted:   true,
			expectInput:    "backup.zip",
			expectCanExtra: true,
		},
		{
			name:           "unsupported",
			fileName:       "notes.txt",
			expectCanExtra: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := detectLocalBrowserArchiveMeta(tc.fileName)
			require.Equal(t, tc.expectCanExtra, meta.Extractable)
			require.Equal(t, tc.expectFormat, meta.Format)
			require.Equal(t, tc.expectGroup, meta.Group)
			require.Equal(t, tc.expectParted, meta.MultiPart)
			require.Equal(t, tc.expectInput, meta.PreferredInput)
		})
	}
}

func TestResolveLocalBrowserArchiveInput(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	part01 := filepath.Join(tmp, "bundle.part01.rar")
	part02 := filepath.Join(tmp, "bundle.part02.rar")
	part03 := filepath.Join(tmp, "bundle.part03.rar")
	require.NoError(t, osWriteFile(part01, "1"))
	require.NoError(t, osWriteFile(part02, "2"))
	require.NoError(t, osWriteFile(part03, "3"))

	resolved, meta, err := resolveLocalBrowserArchiveInput(part03)
	require.NoError(t, err)
	require.Equal(t, part01, resolved)
	require.Equal(t, localBrowserArchiveFormatRar, meta.Format)
	require.True(t, meta.MultiPart)
}

func TestResolveLocalBrowserArchiveInput_MissingFirstPart(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	part02 := filepath.Join(tmp, "archive.7z.002")
	require.NoError(t, osWriteFile(part02, "2"))

	_, _, err := resolveLocalBrowserArchiveInput(part02)
	require.Error(t, err)
	reqErr := &localBrowserRequestError{}
	require.ErrorAs(t, err, &reqErr)
}

func TestPrepareLocalZipInput_DotParts(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	part1 := filepath.Join(tmp, "demo.zip.001")
	part2 := filepath.Join(tmp, "demo.zip.002")
	require.NoError(t, osWriteFile(part1, "hello "))
	require.NoError(t, osWriteFile(part2, "world"))

	meta := localBrowserArchiveMeta{
		Extractable: true,
		Format:      localBrowserArchiveFormatZip,
		MultiPart:   true,
	}
	assembled, cleanup, err := prepareLocalZipInput(context.Background(), part1, meta)
	require.NoError(t, err)
	defer cleanup()
	require.NotEqual(t, part1, assembled)

	b, err := os.ReadFile(assembled)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(b))
}

func TestLocalBrowserCollectZipDotParts_Hole(t *testing.T) {
	t.Parallel()

	_, err := localBrowserCollectZipDotParts("demo.zip.001", map[string]string{
		"demo.zip.001": "demo.zip.001",
		"demo.zip.003": "demo.zip.003",
	})
	require.Error(t, err)
}

func TestValidateLocalBrowserMultipartInput_RarPartHole(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	require.NoError(t, osWriteFile(filepath.Join(tmp, "movie.part01.rar"), "a"))
	require.NoError(t, osWriteFile(filepath.Join(tmp, "movie.part03.rar"), "c"))

	meta := detectLocalBrowserArchiveMeta("movie.part03.rar")
	err := validateLocalBrowserMultipartInput(tmp, "movie.part01.rar", meta)
	require.Error(t, err)
	reqErr := &localBrowserRequestError{}
	require.ErrorAs(t, err, &reqErr)
	require.Contains(t, reqErr.Error(), "movie.part02.rar")
}

func TestValidateLocalBrowserMultipartInput_7zHole(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	require.NoError(t, osWriteFile(filepath.Join(tmp, "pack.7z.001"), "a"))
	require.NoError(t, osWriteFile(filepath.Join(tmp, "pack.7z.003"), "c"))

	meta := detectLocalBrowserArchiveMeta("pack.7z.003")
	err := validateLocalBrowserMultipartInput(tmp, "pack.7z.001", meta)
	require.Error(t, err)
	reqErr := &localBrowserRequestError{}
	require.ErrorAs(t, err, &reqErr)
	require.Contains(t, reqErr.Error(), "pack.7z.002")
}

func TestOpenLocalZipReader_DotParts(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, osWriteFile(src, "zip-part-read-ok"))

	fullZip := filepath.Join(tmp, "demo.zip")
	require.NoError(t, createLocalZipArchive(context.Background(), []string{src}, []string{"a.txt"}, fullZip, 5, ""))
	archiveBytes, err := os.ReadFile(fullZip)
	require.NoError(t, err)
	require.Greater(t, len(archiveBytes), 2)

	splitAt := len(archiveBytes) / 2
	if splitAt <= 0 {
		splitAt = 1
	}
	part1 := filepath.Join(tmp, "demo.zip.001")
	part2 := filepath.Join(tmp, "demo.zip.002")
	require.NoError(t, os.WriteFile(part1, archiveBytes[:splitAt], 0o644))
	require.NoError(t, os.WriteFile(part2, archiveBytes[splitAt:], 0o644))

	meta := detectLocalBrowserArchiveMeta(filepath.Base(part1))
	reader, cleanup, err := openLocalZipReader(part1, meta)
	require.NoError(t, err)
	defer cleanup()

	outDir := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	warnings, err := extractLocalZipArchiveFromReader(context.Background(), reader, outDir, "")
	require.NoError(t, err)
	require.Empty(t, warnings)

	got, err := os.ReadFile(filepath.Join(outDir, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "zip-part-read-ok", string(got))
}

func TestLocalBrowserResolveExtractPath(t *testing.T) {
	t.Parallel()

	dst := filepath.Join(t.TempDir(), "out")
	okPath, err := localBrowserResolveExtractPath(dst, "a/b.txt")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(okPath, filepath.Clean(dst)))

	_, err = localBrowserResolveExtractPath(dst, "../../evil.txt")
	require.Error(t, err)
}

func TestCreateAndExtractLocalZipArchive_WithPassword(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, osWriteFile(src, "zip-password-ok"))
	archive := filepath.Join(tmp, "a.zip")

	err := createLocalZipArchive(context.Background(), []string{src}, []string{"a.txt"}, archive, 5, "123456")
	require.NoError(t, err)

	outDir := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	warnings, err := extractLocalZipArchive(context.Background(), archive, outDir, "123456")
	require.NoError(t, err)
	require.Empty(t, warnings)

	got, err := os.ReadFile(filepath.Join(outDir, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "zip-password-ok", string(got))
}

func TestCreateAndExtractLocalZipArchive_WithPasswordContainingSpaces(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, osWriteFile(src, "zip-password-space-ok"))
	archive := filepath.Join(tmp, "a.zip")

	password := " 123456 "
	err := createLocalZipArchive(context.Background(), []string{src}, []string{"a.txt"}, archive, 5, password)
	require.NoError(t, err)

	outDir := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	warnings, err := extractLocalZipArchive(context.Background(), archive, outDir, password)
	require.NoError(t, err)
	require.Empty(t, warnings)

	got, err := os.ReadFile(filepath.Join(outDir, "a.txt"))
	require.NoError(t, err)
	require.Equal(t, "zip-password-space-ok", string(got))
}

func TestExtractLocalZipArchive_TruncatedArchiveReturnsWarning(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, osWriteFile(src, strings.Repeat("x", 1024)))

	fullZip := filepath.Join(tmp, "a.zip")
	require.NoError(t, createLocalZipArchive(context.Background(), []string{src}, []string{"a.txt"}, fullZip, 5, ""))

	archiveBytes, err := os.ReadFile(fullZip)
	require.NoError(t, err)
	require.Greater(t, len(archiveBytes), 20)

	truncated := filepath.Join(tmp, "a-truncated.zip")
	require.NoError(t, os.WriteFile(truncated, archiveBytes[:len(archiveBytes)-20], 0o644))

	outDir := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	warnings, err := extractLocalZipArchive(context.Background(), truncated, outDir, "")
	require.NoError(t, err)
	require.NotEmpty(t, warnings)
}

func TestExtractLocalZipArchive_WrongPasswordFails(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	src := filepath.Join(tmp, "a.txt")
	require.NoError(t, osWriteFile(src, "zip-password-fail"))

	archive := filepath.Join(tmp, "a.zip")
	require.NoError(t, createLocalZipArchive(context.Background(), []string{src}, []string{"a.txt"}, archive, 5, "123456"))

	outDir := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	warnings, err := extractLocalZipArchive(context.Background(), archive, outDir, "bad-pass")
	require.Error(t, err)
	require.Empty(t, warnings)
	reqErr := &localBrowserRequestError{}
	require.ErrorAs(t, err, &reqErr)
	require.Contains(t, reqErr.Error(), "zip")
}

func TestExtractLocalZipArchiveEntries_SkipsUnsupportedEntryTypes(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	archive := filepath.Join(tmp, "unsupported.zip")
	file, err := os.Create(archive)
	require.NoError(t, err)

	writer := stdzip.NewWriter(file)
	hdr := &stdzip.FileHeader{
		Name:   "bad-link",
		Method: stdzip.Store,
	}
	hdr.SetMode(os.ModeSymlink | 0o777)
	entryWriter, err := writer.CreateHeader(hdr)
	require.NoError(t, err)
	_, err = entryWriter.Write([]byte("target"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, file.Close())

	outDir := filepath.Join(tmp, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	warnings, err := extractLocalZipArchive(context.Background(), archive, outDir, "")
	require.NoError(t, err)
	require.NotEmpty(t, warnings)
	require.Contains(t, strings.Join(warnings, "\n"), "symlink entry is not supported")
}

func TestCreateLocalZipArchive_SkipsUnsupportedFiles(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "data")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, osWriteFile(filepath.Join(root, "ok.txt"), "ok"))
	require.NoError(t, os.Symlink(filepath.Join(root, "ok.txt"), filepath.Join(root, "link.txt")))

	archive := filepath.Join(tmp, "data.zip")
	warnings, err := createLocalZipArchiveWithWarnings(context.Background(), []string{root}, []string{"data"}, archive, 5, "")
	require.NoError(t, err)
	require.NotEmpty(t, warnings)
	require.Contains(t, strings.Join(warnings, "\n"), "symlink is not supported")

	reader, err := stdzip.OpenReader(archive)
	require.NoError(t, err)
	defer reader.Close()

	names := make([]string, 0, len(reader.File))
	for _, f := range reader.File {
		names = append(names, f.Name)
	}
	require.Contains(t, names, "data/")
	require.Contains(t, names, "data/ok.txt")
	require.NotContains(t, names, "data/link.txt")
}

func TestCreateLocalZipArchive_OnlyUnsupportedFilesKeepsEmptyDir(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	root := filepath.Join(tmp, "data")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing.txt"), filepath.Join(root, "link.txt")))

	archive := filepath.Join(tmp, "data.zip")
	warnings, err := createLocalZipArchiveWithWarnings(context.Background(), []string{root}, []string{"data"}, archive, 5, "")
	require.NoError(t, err)
	require.NotEmpty(t, warnings)
	require.Contains(t, strings.Join(warnings, "\n"), "symlink is not supported")

	reader, err := stdzip.OpenReader(archive)
	require.NoError(t, err)
	defer reader.Close()
	require.Len(t, reader.File, 1)
	require.Equal(t, "data/", reader.File[0].Name)
}

func TestLocalBrowserExtractUnsupportedTypeWarning(t *testing.T) {
	t.Parallel()

	warning, skip := localBrowserExtractUnsupportedTypeWarning("a.sock", os.ModeSocket)
	require.True(t, skip)
	require.Contains(t, warning, "socket entry is not supported")

	warning, skip = localBrowserExtractUnsupportedTypeWarning("a.pipe", os.ModeNamedPipe)
	require.True(t, skip)
	require.Contains(t, warning, "named pipe entry is not supported")

	warning, skip = localBrowserExtractUnsupportedTypeWarning("a.txt", 0o644)
	require.False(t, skip)
	require.Empty(t, warning)
}

func TestIsArchiveTruncatedOrCorrupted(t *testing.T) {
	t.Parallel()

	require.True(t, isArchiveTruncatedOrCorrupted(stderrors.New("rardecode: decoded file too short")))
	require.True(t, isArchiveTruncatedOrCorrupted(stderrors.New("checksum mismatch")))
	require.False(t, isArchiveTruncatedOrCorrupted(stderrors.New("permission denied")))
}

func osWriteFile(path string, value string) error {
	return os.WriteFile(path, []byte(value), 0o644)
}
