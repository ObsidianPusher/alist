package handles

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
	zip "github.com/yeka/zip"
)

const localBrowserPasswordPrecheckTimeout = 12 * time.Second
const localBrowserPasswordPrecheckMaxReadBytes int64 = 8 * 1024 * 1024

func precheckLocalArchivePassword(ctx context.Context, archiveAbs, password string) error {
	inputPath, meta, err := resolveLocalBrowserArchiveInputWithMode(archiveAbs, false)
	if err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, localBrowserPasswordPrecheckTimeout)
	defer cancel()
	switch meta.Format {
	case localBrowserArchiveFormatZip:
		reader, cleanup, err := openLocalZipReader(inputPath, meta)
		if err != nil {
			if localBrowserIsArchivePasswordErrorMessage(err.Error()) {
				return archivePasswordRequestError(localBrowserArchiveFormatZip)
			}
			return nil
		}
		defer cleanup()
		return precheckLocalZipPassword(checkCtx, reader, password)
	case localBrowserArchiveFormatRar:
		return precheckLocalRarPassword(checkCtx, inputPath, password)
	case localBrowserArchiveFormat7z:
		return precheckLocalSevenZipPassword(checkCtx, inputPath, password)
	default:
		return nil
	}
}

func precheckLocalZipPassword(ctx context.Context, reader *zip.Reader, password string) error {
	if reader == nil {
		return nil
	}
	var target *zip.File
	for _, f := range reader.File {
		if f.FileInfo().IsDir() || !f.IsEncrypted() {
			continue
		}
		if target == nil {
			target = f
			continue
		}
		if f.CompressedSize64 < target.CompressedSize64 ||
			(f.CompressedSize64 == target.CompressedSize64 && f.UncompressedSize64 < target.UncompressedSize64) {
			target = f
		}
	}
	if target == nil {
		return nil
	}
	if password == "" {
		return archivePasswordRequestError(localBrowserArchiveFormatZip)
	}
	if ctx.Err() != nil {
		if stderrors.Is(ctx.Err(), context.DeadlineExceeded) {
			return archivePasswordPrecheckTimeoutError(localBrowserArchiveFormatZip)
		}
		return ctx.Err()
	}
	target.SetPassword(password)
	src, err := target.Open()
	if err != nil {
		return archivePasswordRequestError(localBrowserArchiveFormatZip)
	}
	return precheckEncryptedStream(ctx, src, int64(target.CompressedSize64), localBrowserArchiveFormatZip, true)
}

func precheckLocalRarPassword(ctx context.Context, archiveAbs, password string) error {
	opts := make([]rardecode.Option, 0, 1)
	if password != "" {
		opts = append(opts, rardecode.Password(password))
	}
	reader, err := rardecode.OpenReader(archiveAbs, opts...)
	if err != nil {
		if isRarPasswordError(err) || localBrowserIsArchivePasswordErrorMessage(err.Error()) {
			return archivePasswordRequestError(localBrowserArchiveFormatRar)
		}
		return nil
	}
	defer reader.Close()

	for {
		if ctx.Err() != nil {
			if stderrors.Is(ctx.Err(), context.DeadlineExceeded) {
				return archivePasswordPrecheckTimeoutError(localBrowserArchiveFormatRar)
			}
			return ctx.Err()
		}
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if isRarPasswordError(err) || localBrowserIsArchivePasswordErrorMessage(err.Error()) {
				return archivePasswordRequestError(localBrowserArchiveFormatRar)
			}
			return nil
		}
		if header.IsDir {
			continue
		}
		_, readErr := localBrowserCopyWithContext(ctx, io.Discard, io.LimitReader(reader, localBrowserPasswordPrecheckMaxReadBytes))
		if readErr != nil && readErr != io.EOF {
			if stderrors.Is(readErr, context.DeadlineExceeded) {
				return archivePasswordPrecheckTimeoutError(localBrowserArchiveFormatRar)
			}
			if isRarPasswordError(readErr) || localBrowserIsArchivePasswordErrorMessage(readErr.Error()) || password != "" {
				return archivePasswordRequestError(localBrowserArchiveFormatRar)
			}
		}
		return nil
	}
}

func precheckLocalSevenZipPassword(ctx context.Context, archiveAbs, password string) error {
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
		if isSevenZipPasswordError(err) || localBrowserIsArchivePasswordErrorMessage(err.Error()) {
			return archivePasswordRequestError(localBrowserArchiveFormat7z)
		}
		return nil
	}
	defer reader.Close()

	for _, f := range reader.File {
		if ctx.Err() != nil {
			if stderrors.Is(ctx.Err(), context.DeadlineExceeded) {
				return archivePasswordPrecheckTimeoutError(localBrowserArchiveFormat7z)
			}
			return ctx.Err()
		}
		if f.FileInfo().IsDir() {
			continue
		}
		src, err := f.Open()
		if err != nil {
			if isSevenZipPasswordError(err) || localBrowserIsArchivePasswordErrorMessage(err.Error()) {
				return archivePasswordRequestError(localBrowserArchiveFormat7z)
			}
			return nil
		}
		return precheckEncryptedStream(ctx, src, int64(f.UncompressedSize), localBrowserArchiveFormat7z, false)
	}
	return nil
}

func precheckEncryptedStream(ctx context.Context, src io.ReadCloser, sizeHint int64, format string, fullRead bool) error {
	limit := localBrowserPasswordPrecheckMaxReadBytes
	if fullRead {
		if sizeHint > 0 {
			limit = sizeHint
		}
	} else if sizeHint > 0 && sizeHint < limit {
		limit = sizeHint
	}

	_, readErr := localBrowserCopyWithContext(ctx, io.Discard, io.LimitReader(src, limit))
	closeErr := src.Close()

	if readErr != nil && readErr != io.EOF {
		if stderrors.Is(readErr, context.DeadlineExceeded) {
			return archivePasswordPrecheckTimeoutError(format)
		}
		return archivePasswordRequestError(format)
	}
	if closeErr != nil {
		if stderrors.Is(closeErr, context.DeadlineExceeded) {
			return archivePasswordPrecheckTimeoutError(format)
		}
		if localBrowserIsArchivePasswordErrorMessage(closeErr.Error()) || isArchiveTruncatedOrCorrupted(closeErr) {
			return archivePasswordRequestError(format)
		}
		return archivePasswordRequestError(format)
	}
	if ctx.Err() != nil {
		if stderrors.Is(ctx.Err(), context.DeadlineExceeded) {
			return archivePasswordPrecheckTimeoutError(format)
		}
		return ctx.Err()
	}
	return nil
}

func archivePasswordRequestError(format string) error {
	return &localBrowserRequestError{
		message: fmt.Sprintf("wrong password or encrypted %s requires password", format),
	}
}

func archivePasswordPrecheckTimeoutError(format string) error {
	return &localBrowserRequestError{
		message: fmt.Sprintf("password precheck timed out for %s archive, please retry", format),
	}
}

func localBrowserIsArchivePasswordErrorMessage(msg string) bool {
	m := strings.ToLower(msg)
	if m == "" {
		return false
	}
	if strings.Contains(m, "wrong password") {
		return true
	}
	if strings.Contains(m, "incorrect password") {
		return true
	}
	if strings.Contains(m, "encrypted") && strings.Contains(m, "password") {
		return true
	}
	return false
}
