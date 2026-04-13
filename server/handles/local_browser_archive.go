package handles

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
	"github.com/pkg/errors"
	zip "github.com/yeka/zip"
)

const (
	localBrowserArchiveFormatZip = "zip"
	localBrowserArchiveFormatRar = "rar"
	localBrowserArchiveFormat7z  = "7z"
)

var (
	localBrowserPartRarRegex = regexp.MustCompile(`(?i)^(.+)\.part(\d+)\.rar$`)
	localBrowserRarVolRegex  = regexp.MustCompile(`(?i)^(.+)\.r(\d{2})$`)
	localBrowserPart7zRegex  = regexp.MustCompile(`(?i)^(.+)\.7z\.(\d{3})$`)
	localBrowserPartZipRegex = regexp.MustCompile(`(?i)^(.+)\.zip\.(\d{3})$`)
	localBrowserZipVolRegex  = regexp.MustCompile(`(?i)^(.+)\.z(\d{2})$`)
)

type localBrowserRequestError struct {
	message string
}

func (e *localBrowserRequestError) Error() string {
	return e.message
}

type localBrowserArchiveMeta struct {
	Extractable    bool
	Format         string
	Group          string
	MultiPart      bool
	PreferredInput string
}

func detectLocalBrowserArchiveMeta(fileName string) localBrowserArchiveMeta {
	name := strings.TrimSpace(fileName)
	if name == "" {
		return localBrowserArchiveMeta{}
	}
	if m := localBrowserPartRarRegex.FindStringSubmatch(name); len(m) == 3 {
		width := len(m[2])
		mainPart := fmt.Sprintf("%s.part%0*d.rar", m[1], width, 1)
		return localBrowserArchiveMeta{
			Extractable:    true,
			Format:         localBrowserArchiveFormatRar,
			Group:          fmt.Sprintf("%s.rar", m[1]),
			MultiPart:      true,
			PreferredInput: mainPart,
		}
	}
	if m := localBrowserRarVolRegex.FindStringSubmatch(name); len(m) == 3 {
		mainPart := fmt.Sprintf("%s.rar", m[1])
		return localBrowserArchiveMeta{
			Extractable:    true,
			Format:         localBrowserArchiveFormatRar,
			Group:          mainPart,
			MultiPart:      true,
			PreferredInput: mainPart,
		}
	}
	if hasSuffixFold(name, ".rar") {
		return localBrowserArchiveMeta{
			Extractable: true,
			Format:      localBrowserArchiveFormatRar,
			Group:       name,
		}
	}

	if m := localBrowserPart7zRegex.FindStringSubmatch(name); len(m) == 3 {
		width := len(m[2])
		mainPart := fmt.Sprintf("%s.7z.%0*d", m[1], width, 1)
		return localBrowserArchiveMeta{
			Extractable:    true,
			Format:         localBrowserArchiveFormat7z,
			Group:          fmt.Sprintf("%s.7z", m[1]),
			MultiPart:      true,
			PreferredInput: mainPart,
		}
	}
	if hasSuffixFold(name, ".7z") {
		return localBrowserArchiveMeta{
			Extractable: true,
			Format:      localBrowserArchiveFormat7z,
			Group:       name,
		}
	}

	if m := localBrowserPartZipRegex.FindStringSubmatch(name); len(m) == 3 {
		width := len(m[2])
		mainPart := fmt.Sprintf("%s.zip.%0*d", m[1], width, 1)
		return localBrowserArchiveMeta{
			Extractable:    true,
			Format:         localBrowserArchiveFormatZip,
			Group:          fmt.Sprintf("%s.zip", m[1]),
			MultiPart:      true,
			PreferredInput: mainPart,
		}
	}
	if m := localBrowserZipVolRegex.FindStringSubmatch(name); len(m) == 3 {
		mainPart := fmt.Sprintf("%s.zip", m[1])
		return localBrowserArchiveMeta{
			Extractable:    true,
			Format:         localBrowserArchiveFormatZip,
			Group:          mainPart,
			MultiPart:      true,
			PreferredInput: mainPart,
		}
	}
	if hasSuffixFold(name, ".zip") {
		return localBrowserArchiveMeta{
			Extractable: true,
			Format:      localBrowserArchiveFormatZip,
			Group:       name,
		}
	}
	return localBrowserArchiveMeta{}
}

func extractLocalArchive(ctx context.Context, archiveAbs, dstAbs, password string) (localBrowserArchiveMeta, []string, error) {
	inputPath, meta, err := resolveLocalBrowserArchiveInputWithMode(archiveAbs, false)
	if err != nil {
		return localBrowserArchiveMeta{}, nil, err
	}
	switch meta.Format {
	case localBrowserArchiveFormatZip:
		reader, cleanup, err := openLocalZipReader(inputPath, meta)
		if err != nil {
			if isArchiveTruncatedOrCorrupted(err) {
				return meta, []string{"zip archive is incomplete/corrupted, extracted files may be partial"}, nil
			}
			return localBrowserArchiveMeta{}, nil, err
		}
		defer cleanup()
		warnings, err := extractLocalZipArchiveFromReader(ctx, reader, dstAbs, password)
		if err != nil {
			return localBrowserArchiveMeta{}, nil, err
		}
		return meta, warnings, nil
	case localBrowserArchiveFormatRar:
		warnings, err := extractLocalRarArchive(ctx, inputPath, dstAbs, password)
		if err != nil {
			return localBrowserArchiveMeta{}, nil, err
		}
		return meta, warnings, nil
	case localBrowserArchiveFormat7z:
		warnings, err := extractLocalSevenZipArchive(ctx, inputPath, dstAbs, password)
		if err != nil {
			return localBrowserArchiveMeta{}, nil, err
		}
		return meta, warnings, nil
	default:
		return localBrowserArchiveMeta{}, nil, &localBrowserRequestError{
			message: "unsupported archive format",
		}
	}
}

func extractLocalRarArchive(ctx context.Context, archiveAbs, dstAbs, password string) ([]string, error) {
	warnings := make([]string, 0, 4)
	opts := make([]rardecode.Option, 0, 1)
	if password != "" {
		opts = append(opts, rardecode.Password(password))
	}
	reader, err := rardecode.OpenReader(archiveAbs, opts...)
	if err != nil {
		if isRarPasswordError(err) {
			return nil, &localBrowserRequestError{message: "wrong password or encrypted rar requires password"}
		}
		if isArchiveTruncatedOrCorrupted(err) {
			if password != "" {
				return nil, &localBrowserRequestError{message: "wrong password or encrypted rar requires password"}
			}
			return []string{
				"rar archive is incomplete/corrupted, extracted files may be partial",
			}, nil
		}
		return nil, err
	}
	defer reader.Close()

	for {
		if ctx.Err() != nil {
			return warnings, ctx.Err()
		}
		header, err := reader.Next()
		if err == io.EOF {
			return warnings, nil
		}
		if err != nil {
			if isRarPasswordError(err) {
				return warnings, &localBrowserRequestError{message: "wrong password or encrypted rar requires password"}
			}
			if isArchiveTruncatedOrCorrupted(err) {
				if password != "" {
					return warnings, &localBrowserRequestError{message: "wrong password or encrypted rar requires password"}
				}
				warnings = append(warnings, "rar archive stream ended early (incomplete/corrupted)")
				return warnings, nil
			}
			return warnings, err
		}
		targetPath, err := localBrowserResolveExtractPath(dstAbs, header.Name)
		if err != nil {
			return warnings, err
		}
		mode := header.Mode()
		if header.IsDir {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return warnings, err
			}
			continue
		}
		if mode&os.ModeSymlink != 0 {
			return warnings, errors.Errorf("unsupported symlink entry: %s", header.Name)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return warnings, err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, localBrowserNormalizeFileMode(mode))
		if err != nil {
			return warnings, err
		}
		_, copyErr := localBrowserCopyWithContext(ctx, dst, reader)
		closeErr := dst.Close()
		if copyErr != nil {
			_ = os.Remove(targetPath)
			if isArchiveTruncatedOrCorrupted(copyErr) {
				if password != "" {
					return warnings, &localBrowserRequestError{message: "wrong password or encrypted rar requires password"}
				}
				warnings = append(warnings, fmt.Sprintf("failed to fully extract %s: archive stream ended early", header.Name))
				return warnings, nil
			}
			return warnings, copyErr
		}
		if closeErr != nil {
			return warnings, closeErr
		}
	}
}

func extractLocalSevenZipArchive(ctx context.Context, archiveAbs, dstAbs, password string) ([]string, error) {
	warnings := make([]string, 0, 4)
	var (
		reader *sevenzip.ReadCloser
		err    error
	)
	if password != "" {
		reader, err = sevenzip.OpenReaderWithPassword(archiveAbs, password)
	} else {
		reader, err = sevenzip.OpenReader(archiveAbs)
	}
	if err != nil {
		if isSevenZipPasswordError(err) {
			return nil, &localBrowserRequestError{message: "wrong password or encrypted 7z requires password"}
		}
		if isArchiveTruncatedOrCorrupted(err) {
			if password != "" {
				return nil, &localBrowserRequestError{message: "wrong password or encrypted 7z requires password"}
			}
			return []string{
				"7z archive is incomplete/corrupted, extracted files may be partial",
			}, nil
		}
		return nil, err
	}
	defer reader.Close()

	for _, f := range reader.File {
		if ctx.Err() != nil {
			return warnings, ctx.Err()
		}
		targetPath, err := localBrowserResolveExtractPath(dstAbs, f.Name)
		if err != nil {
			return warnings, err
		}
		info := f.FileInfo()
		mode := info.Mode()
		if info.IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return warnings, err
			}
			continue
		}
		if mode&os.ModeSymlink != 0 {
			return warnings, errors.Errorf("unsupported symlink entry: %s", f.Name)
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return warnings, err
		}
		src, err := f.Open()
		if err != nil {
			if isSevenZipPasswordError(err) {
				return warnings, &localBrowserRequestError{message: "wrong password or encrypted 7z requires password"}
			}
			if isArchiveTruncatedOrCorrupted(err) {
				if password != "" {
					return warnings, &localBrowserRequestError{message: "wrong password or encrypted 7z requires password"}
				}
				warnings = append(warnings, fmt.Sprintf("skip %s: archive entry is incomplete/corrupted", f.Name))
				continue
			}
			return warnings, err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, localBrowserNormalizeFileMode(mode))
		if err != nil {
			src.Close()
			return warnings, err
		}
		_, copyErr := localBrowserCopyWithContext(ctx, dst, src)
		closeDstErr := dst.Close()
		closeSrcErr := src.Close()
		if copyErr != nil {
			_ = os.Remove(targetPath)
			if isArchiveTruncatedOrCorrupted(copyErr) {
				if password != "" {
					return warnings, &localBrowserRequestError{message: "wrong password or encrypted 7z requires password"}
				}
				warnings = append(warnings, fmt.Sprintf("failed to fully extract %s: archive entry ended early", f.Name))
				continue
			}
			return warnings, copyErr
		}
		if closeDstErr != nil {
			return warnings, closeDstErr
		}
		if closeSrcErr != nil {
			if isArchiveTruncatedOrCorrupted(closeSrcErr) {
				if password != "" {
					return warnings, &localBrowserRequestError{message: "wrong password or encrypted 7z requires password"}
				}
				_ = os.Remove(targetPath)
				warnings = append(warnings, fmt.Sprintf("failed to fully extract %s: archive entry ended early", f.Name))
				continue
			}
			return warnings, closeSrcErr
		}
	}
	return warnings, nil
}

func resolveLocalBrowserArchiveInput(archiveAbs string) (string, localBrowserArchiveMeta, error) {
	return resolveLocalBrowserArchiveInputWithMode(archiveAbs, true)
}

func resolveLocalBrowserArchiveInputWithMode(archiveAbs string, requireComplete bool) (string, localBrowserArchiveMeta, error) {
	clean := filepath.Clean(archiveAbs)
	meta := detectLocalBrowserArchiveMeta(filepath.Base(clean))
	if !meta.Extractable {
		return "", localBrowserArchiveMeta{}, &localBrowserRequestError{
			message: "unsupported archive format",
		}
	}
	if !meta.MultiPart || meta.PreferredInput == "" {
		return clean, meta, nil
	}
	curName := filepath.Base(clean)
	if strings.EqualFold(curName, meta.PreferredInput) {
		if requireComplete {
			if err := validateLocalBrowserMultipartInput(filepath.Dir(clean), curName, meta); err != nil {
				return "", localBrowserArchiveMeta{}, err
			}
		}
		return clean, meta, nil
	}
	matchPath, err := findArchiveVolume(filepath.Dir(clean), meta.PreferredInput)
	if err != nil {
		return "", localBrowserArchiveMeta{}, err
	}
	if matchPath == "" {
		if !requireComplete {
			return clean, meta, nil
		}
		return "", localBrowserArchiveMeta{}, &localBrowserRequestError{
			message: fmt.Sprintf("multipart archive incomplete, first volume %s not found", meta.PreferredInput),
		}
	}
	if requireComplete {
		if err := validateLocalBrowserMultipartInput(filepath.Dir(matchPath), filepath.Base(matchPath), meta); err != nil {
			return "", localBrowserArchiveMeta{}, err
		}
	}
	return matchPath, meta, nil
}

func validateLocalBrowserMultipartInput(dir, inputName string, meta localBrowserArchiveMeta) error {
	files, err := localBrowserListDirLowerMap(dir)
	if err != nil {
		return err
	}
	inputLower := strings.ToLower(inputName)
	switch meta.Format {
	case localBrowserArchiveFormatZip:
		switch {
		case strings.HasSuffix(inputLower, ".zip.001"):
			_, err = localBrowserCollectZipDotParts(inputName, files)
			return err
		case strings.HasSuffix(inputLower, ".zip"):
			parts, err := localBrowserCollectZipZParts(inputName, files)
			if err != nil {
				return err
			}
			if meta.MultiPart && len(parts) <= 1 {
				return &localBrowserRequestError{
					message: fmt.Sprintf("multipart archive incomplete, volume %s not found", strings.TrimSuffix(inputName, ".zip")+".z01"),
				}
			}
			return nil
		default:
			return nil
		}
	case localBrowserArchiveFormatRar:
		if m := localBrowserPartRarRegex.FindStringSubmatch(inputName); len(m) == 3 {
			_, err = localBrowserCollectRarPartParts(inputName, files)
			return err
		}
		if hasSuffixFold(inputName, ".rar") {
			_, err = localBrowserCollectRarOldStyleParts(inputName, files, true)
			return err
		}
		return nil
	case localBrowserArchiveFormat7z:
		if strings.HasSuffix(inputLower, ".7z.001") {
			_, err = localBrowserCollectSevenZipParts(inputName, files)
			return err
		}
		return nil
	default:
		return nil
	}
}

func findArchiveVolume(dir, preferredName string) (string, error) {
	candidate := filepath.Join(dir, preferredName)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(entry.Name(), preferredName) {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", nil
}

type localBrowserMultiPartReaderAt struct {
	parts []localBrowserPartReader
	size  int64
}

type localBrowserPartReader struct {
	start int64
	end   int64
	file  *os.File
}

func (r *localBrowserMultiPartReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if off >= r.size {
		return 0, io.EOF
	}
	read := 0
	for len(p) > 0 && off < r.size {
		idx := sort.Search(len(r.parts), func(i int) bool {
			return r.parts[i].end > off
		})
		if idx >= len(r.parts) {
			break
		}
		part := r.parts[idx]
		inner := off - part.start
		want := int64(len(p))
		if max := part.end - off; want > max {
			want = max
		}
		n, err := part.file.ReadAt(p[:int(want)], inner)
		if n > 0 {
			read += n
			off += int64(n)
			p = p[n:]
		}
		if err != nil && err != io.EOF {
			return read, err
		}
		if int64(n) < want {
			return read, io.ErrUnexpectedEOF
		}
	}
	if len(p) > 0 {
		return read, io.EOF
	}
	return read, nil
}

func openLocalZipReader(inputPath string, meta localBrowserArchiveMeta) (*zip.Reader, func(), error) {
	openSingle := func(path string) (*zip.Reader, func(), error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return nil, nil, err
		}
		reader, err := zip.NewReader(file, info.Size())
		if err != nil {
			_ = file.Close()
			return nil, nil, err
		}
		return reader, func() { _ = file.Close() }, nil
	}

	if !meta.MultiPart {
		return openSingle(inputPath)
	}

	inputName := filepath.Base(inputPath)
	inputLower := strings.ToLower(inputName)
	dir := filepath.Dir(inputPath)
	files, err := localBrowserListDirLowerMap(dir)
	if err != nil {
		return nil, nil, err
	}

	var parts []string
	switch {
	case strings.HasSuffix(inputLower, ".zip.001"):
		parts, err = localBrowserCollectZipDotParts(inputName, files)
	case strings.HasSuffix(inputLower, ".zip"):
		parts, err = localBrowserCollectZipZParts(inputName, files)
	default:
		return openSingle(inputPath)
	}
	if err != nil {
		return nil, nil, err
	}
	if len(parts) <= 1 {
		return openSingle(inputPath)
	}

	openFiles := make([]*os.File, 0, len(parts))
	readerParts := make([]localBrowserPartReader, 0, len(parts))
	var total int64
	closeAll := func() {
		for _, file := range openFiles {
			_ = file.Close()
		}
	}
	for _, part := range parts {
		file, err := os.Open(filepath.Join(dir, part))
		if err != nil {
			closeAll()
			return nil, nil, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			closeAll()
			return nil, nil, err
		}
		size := info.Size()
		openFiles = append(openFiles, file)
		readerParts = append(readerParts, localBrowserPartReader{
			start: total,
			end:   total + size,
			file:  file,
		})
		total += size
	}
	readerAt := &localBrowserMultiPartReaderAt{
		parts: readerParts,
		size:  total,
	}
	reader, err := zip.NewReader(readerAt, total)
	if err != nil {
		closeAll()
		return nil, nil, err
	}
	return reader, closeAll, nil
}

func prepareLocalZipInput(ctx context.Context, inputPath string, meta localBrowserArchiveMeta) (string, func(), error) {
	noop := func() {}
	if !meta.MultiPart {
		return inputPath, noop, nil
	}
	inputName := filepath.Base(inputPath)
	inputLower := strings.ToLower(inputName)
	dir := filepath.Dir(inputPath)

	files, err := localBrowserListDirLowerMap(dir)
	if err != nil {
		return "", noop, err
	}
	var parts []string
	switch {
	case strings.HasSuffix(inputLower, ".zip.001"):
		parts, err = localBrowserCollectZipDotParts(inputName, files)
	case strings.HasSuffix(inputLower, ".zip"):
		parts, err = localBrowserCollectZipZParts(inputName, files)
	default:
		return inputPath, noop, nil
	}
	if err != nil {
		return "", noop, err
	}
	if len(parts) <= 1 {
		return inputPath, noop, nil
	}
	tmpFile, cleanup, err := localBrowserCombineArchiveFiles(ctx, dir, parts)
	if err != nil {
		return "", noop, err
	}
	return tmpFile, cleanup, nil
}

func localBrowserListDirLowerMap(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	res := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		res[strings.ToLower(entry.Name())] = entry.Name()
	}
	return res, nil
}

func localBrowserCollectZipDotParts(inputName string, files map[string]string) ([]string, error) {
	base := inputName[:len(inputName)-len(".001")]
	baseLower := strings.ToLower(base)
	maxPart := 0
	for lowerName := range files {
		if !strings.HasPrefix(lowerName, baseLower+".") {
			continue
		}
		partSuffix := lowerName[len(baseLower)+1:]
		if len(partSuffix) != 3 || !isNumericString(partSuffix) {
			continue
		}
		partNum := parseFixedInt(partSuffix)
		if partNum > maxPart {
			maxPart = partNum
		}
	}
	if maxPart < 1 {
		expect := fmt.Sprintf("%s.%03d", base, 1)
		return nil, &localBrowserRequestError{
			message: fmt.Sprintf("multipart archive incomplete, first volume %s not found", expect),
		}
	}
	parts := make([]string, 0, maxPart)
	for i := 1; i <= maxPart; i++ {
		expect := fmt.Sprintf("%s.%03d", base, i)
		actual, ok := files[strings.ToLower(expect)]
		if !ok {
			return nil, &localBrowserRequestError{
				message: fmt.Sprintf("multipart archive incomplete, volume %s not found", expect),
			}
		}
		parts = append(parts, actual)
	}
	return parts, nil
}

func localBrowserCollectZipZParts(inputName string, files map[string]string) ([]string, error) {
	zipName, ok := files[strings.ToLower(inputName)]
	if !ok {
		zipName = inputName
	}
	base := zipName[:len(zipName)-len(filepath.Ext(zipName))]
	baseLower := strings.ToLower(base)
	maxPart := 0
	for lowerName := range files {
		if !strings.HasPrefix(lowerName, baseLower+".z") {
			continue
		}
		partSuffix := lowerName[len(baseLower)+2:]
		if len(partSuffix) != 2 || !isNumericString(partSuffix) {
			continue
		}
		partNum := parseFixedInt(partSuffix)
		if partNum > maxPart {
			maxPart = partNum
		}
	}
	if maxPart == 0 {
		return []string{zipName}, nil
	}
	parts := make([]string, 0, maxPart+1)
	for i := 1; i <= maxPart; i++ {
		expect := fmt.Sprintf("%s.z%02d", base, i)
		actual, ok := files[strings.ToLower(expect)]
		if !ok {
			return nil, &localBrowserRequestError{
				message: fmt.Sprintf("multipart archive incomplete, volume %s not found", expect),
			}
		}
		parts = append(parts, actual)
	}
	if _, ok := files[strings.ToLower(zipName)]; !ok {
		return nil, &localBrowserRequestError{
			message: fmt.Sprintf("multipart archive incomplete, final volume %s not found", zipName),
		}
	}
	parts = append(parts, zipName)
	return parts, nil
}

func localBrowserCollectRarPartParts(inputName string, files map[string]string) ([]string, error) {
	m := localBrowserPartRarRegex.FindStringSubmatch(inputName)
	if len(m) != 3 {
		return []string{inputName}, nil
	}
	base := m[1]
	width := len(m[2])
	prefix := strings.ToLower(base + ".part")
	suffix := ".rar"
	maxPart := 0
	for lowerName := range files {
		if !strings.HasPrefix(lowerName, prefix) || !strings.HasSuffix(lowerName, suffix) {
			continue
		}
		partSuffix := lowerName[len(prefix) : len(lowerName)-len(suffix)]
		if !isNumericString(partSuffix) {
			continue
		}
		partNum := parseFixedInt(partSuffix)
		if partNum > maxPart {
			maxPart = partNum
		}
	}
	if maxPart < 1 {
		expect := fmt.Sprintf("%s.part%0*d.rar", base, width, 1)
		return nil, &localBrowserRequestError{
			message: fmt.Sprintf("multipart archive incomplete, first volume %s not found", expect),
		}
	}
	parts := make([]string, 0, maxPart)
	for i := 1; i <= maxPart; i++ {
		expect := fmt.Sprintf("%s.part%0*d.rar", base, width, i)
		actual, ok := files[strings.ToLower(expect)]
		if !ok {
			return nil, &localBrowserRequestError{
				message: fmt.Sprintf("multipart archive incomplete, volume %s not found", expect),
			}
		}
		parts = append(parts, actual)
	}
	return parts, nil
}

func localBrowserCollectRarOldStyleParts(inputName string, files map[string]string, requireR00 bool) ([]string, error) {
	rarName, ok := files[strings.ToLower(inputName)]
	if !ok {
		rarName = inputName
	}
	base := strings.TrimSuffix(rarName, filepath.Ext(rarName))
	baseLower := strings.ToLower(base)
	maxPart := -1
	for lowerName := range files {
		if !strings.HasPrefix(lowerName, baseLower+".r") {
			continue
		}
		partSuffix := lowerName[len(baseLower)+2:]
		if len(partSuffix) != 2 || !isNumericString(partSuffix) {
			continue
		}
		partNum := parseFixedInt(partSuffix)
		if partNum > maxPart {
			maxPart = partNum
		}
	}
	if maxPart < 0 {
		if requireR00 {
			expect := fmt.Sprintf("%s.r00", base)
			return nil, &localBrowserRequestError{
				message: fmt.Sprintf("multipart archive incomplete, volume %s not found", expect),
			}
		}
		return []string{rarName}, nil
	}
	parts := make([]string, 0, maxPart+2)
	parts = append(parts, rarName)
	for i := 0; i <= maxPart; i++ {
		expect := fmt.Sprintf("%s.r%02d", base, i)
		actual, ok := files[strings.ToLower(expect)]
		if !ok {
			return nil, &localBrowserRequestError{
				message: fmt.Sprintf("multipart archive incomplete, volume %s not found", expect),
			}
		}
		parts = append(parts, actual)
	}
	return parts, nil
}

func localBrowserCollectSevenZipParts(inputName string, files map[string]string) ([]string, error) {
	m := localBrowserPart7zRegex.FindStringSubmatch(inputName)
	if len(m) != 3 {
		return []string{inputName}, nil
	}
	base := m[1] + ".7z"
	width := len(m[2])
	baseLower := strings.ToLower(base)
	maxPart := 0
	for lowerName := range files {
		if !strings.HasPrefix(lowerName, baseLower+".") {
			continue
		}
		partSuffix := lowerName[len(baseLower)+1:]
		if !isNumericString(partSuffix) {
			continue
		}
		partNum := parseFixedInt(partSuffix)
		if partNum > maxPart {
			maxPart = partNum
		}
	}
	if maxPart < 1 {
		expect := fmt.Sprintf("%s.%0*d", base, width, 1)
		return nil, &localBrowserRequestError{
			message: fmt.Sprintf("multipart archive incomplete, first volume %s not found", expect),
		}
	}
	parts := make([]string, 0, maxPart)
	for i := 1; i <= maxPart; i++ {
		expect := fmt.Sprintf("%s.%0*d", base, width, i)
		actual, ok := files[strings.ToLower(expect)]
		if !ok {
			return nil, &localBrowserRequestError{
				message: fmt.Sprintf("multipart archive incomplete, volume %s not found", expect),
			}
		}
		parts = append(parts, actual)
	}
	return parts, nil
}

func isNumericString(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func parseFixedInt(s string) int {
	n := 0
	for _, ch := range s {
		n = n*10 + int(ch-'0')
	}
	return n
}

func localBrowserCombineArchiveFiles(ctx context.Context, dir string, parts []string) (string, func(), error) {
	tmp, err := os.CreateTemp("", "alist-local-browser-multipart-*.zip")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(tmp.Name())
	}
	for _, part := range parts {
		if ctx.Err() != nil {
			_ = tmp.Close()
			cleanup()
			return "", nil, ctx.Err()
		}
		srcPath := filepath.Join(dir, part)
		src, err := os.Open(srcPath)
		if err != nil {
			_ = tmp.Close()
			cleanup()
			return "", nil, err
		}
		_, copyErr := localBrowserCopyWithContext(ctx, tmp, src)
		closeErr := src.Close()
		if copyErr != nil {
			_ = tmp.Close()
			cleanup()
			return "", nil, copyErr
		}
		if closeErr != nil {
			_ = tmp.Close()
			cleanup()
			return "", nil, closeErr
		}
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmp.Name(), cleanup, nil
}

func localBrowserResolveExtractPath(dstAbs, archivePath string) (string, error) {
	cleanName := strings.ReplaceAll(archivePath, "\\", "/")
	cleanName = strings.TrimPrefix(cleanName, "/")
	cleanName = filepath.Clean(cleanName)
	if cleanName == "." || cleanName == "" {
		return filepath.Clean(dstAbs), nil
	}
	target := filepath.Join(dstAbs, cleanName)
	cleanTarget := filepath.Clean(target)
	cleanDst := filepath.Clean(dstAbs)
	if !strings.HasPrefix(cleanTarget, cleanDst+string(os.PathSeparator)) && cleanTarget != cleanDst {
		return "", errors.Errorf("illegal archive path: %s", archivePath)
	}
	return cleanTarget, nil
}

func localBrowserNormalizeFileMode(mode os.FileMode) os.FileMode {
	perm := mode.Perm()
	if perm == 0 {
		return 0o644
	}
	return perm
}

func isRarPasswordError(err error) bool {
	if stderrors.Is(err, rardecode.ErrBadPassword) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "password")
}

func isSevenZipPasswordError(err error) bool {
	var readErr *sevenzip.ReadError
	if stderrors.As(err, &readErr) && readErr.Encrypted {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "password")
}

func isArchiveTruncatedOrCorrupted(err error) bool {
	if err == nil {
		return false
	}
	if stderrors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if stderrors.Is(err, io.ErrNoProgress) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "decoded file too short") {
		return true
	}
	if strings.Contains(msg, "file too short") {
		return true
	}
	if strings.Contains(msg, "unexpected eof") {
		return true
	}
	if strings.Contains(msg, "short read") {
		return true
	}
	if strings.Contains(msg, "no data or error") {
		return true
	}
	if strings.Contains(msg, "unexpected end of file") {
		return true
	}
	if strings.Contains(msg, "not a valid zip file") {
		return true
	}
	if strings.Contains(msg, "invalid zip") {
		return true
	}
	if strings.Contains(msg, "checksum") {
		return true
	}
	if strings.Contains(msg, "crc") {
		return true
	}
	if strings.Contains(msg, "corrupt") {
		return true
	}
	return false
}

func hasSuffixFold(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return strings.EqualFold(s[len(s)-len(suffix):], suffix)
}
