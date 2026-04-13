package crypt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	stdpath "path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/internal/cryptx"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/pkg/cachex"
	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	log "github.com/sirupsen/logrus"
)

type Crypt struct {
	model.Storage
	Addition
	cipher        *cryptx.Cipher
	remoteStorage driver.Driver
	nameEncCache  *cachex.BoundedTTLCache[string]
	nameDecCache  *cachex.BoundedTTLCache[cryptNameDecodeResult]
	pathCache     *cachex.BoundedTTLCache[string]
}

const obfuscatedPrefix = "___Obfuscated___"
const baiduDriverName = "BaiduNetdisk"

const (
	cryptNameCacheMaxEntries = 4096
	cryptPathCacheMaxEntries = 2048
	cryptCacheTTL            = 30 * time.Minute
)

var encryptedSuffixPattern = regexp.MustCompile(`^[.][A-Za-z0-9-_]{2,}$`)

type tempFileWithCleanup struct {
	*os.File
	path string
	once sync.Once
}

type cacheHintFile interface {
	Fd() uintptr
}

func newTempFileWithCleanup(f *os.File) *tempFileWithCleanup {
	if f == nil {
		return nil
	}
	return &tempFileWithCleanup{
		File: f,
		path: f.Name(),
	}
}

func (f *tempFileWithCleanup) Close() error {
	if f == nil {
		return nil
	}
	var closeErr error
	f.once.Do(func() {
		closeErr = f.File.Close()
		if removeErr := os.Remove(f.path); removeErr != nil && !os.IsNotExist(removeErr) && closeErr == nil {
			closeErr = removeErr
		}
	})
	return closeErr
}

func isBaiduBackend(storage driver.Driver) bool {
	if storage == nil {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(storage.Config().Name))
	if name == strings.ToLower(baiduDriverName) || strings.Contains(name, "baidu") {
		return true
	}
	typeName := strings.ToLower(fmt.Sprintf("%T", storage))
	if strings.Contains(typeName, "baidu") {
		return true
	}
	return false
}

func encryptedSizeHint(c *cryptx.Cipher, plainSize int64) int64 {
	if plainSize < 0 || c == nil {
		return -1
	}
	return c.EncryptedSize(plainSize)
}

func closeSilently(closer io.Closer, logMsg string) {
	if closer == nil {
		return
	}
	if err := closer.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		log.Debugf(logMsg, err)
	}
}

func displayNameFromPath(path string) string {
	clean := utils.FixAndCleanPath(path)
	if utils.PathEqual(clean, "/") {
		return "Root"
	}
	return stdpath.Base(clean)
}

func (d *Crypt) Config() driver.Config {
	return config
}

func (d *Crypt) GetAddition() driver.Additional {
	return &d.Addition
}

func (d *Crypt) Init(ctx context.Context) error {
	//obfuscate credentials if it's updated or just created
	err := d.updateObfusParm(&d.Password)
	if err != nil {
		return fmt.Errorf("failed to obfuscate password: %w", err)
	}
	err = d.updateObfusParm(&d.Salt)
	if err != nil {
		return fmt.Errorf("failed to obfuscate salt: %w", err)
	}

	if !encryptedSuffixPattern.MatchString(d.EncryptedSuffix) {
		return fmt.Errorf("EncryptedSuffix is Illegal")
	}
	d.FileNameEncoding = utils.GetNoneEmpty(d.FileNameEncoding, "base64")
	d.EncryptedSuffix = utils.GetNoneEmpty(d.EncryptedSuffix, ".bin")

	op.MustSaveDriverStorage(d)

	//need remote storage exist
	storage, err := fs.GetStorage(d.RemotePath, &fs.GetStoragesArgs{})
	if err != nil {
		return fmt.Errorf("can't find remote storage: %w", err)
	}
	if !isBaiduBackend(storage) {
		return fmt.Errorf("crypt remote_path must point to %s storage in this build", baiduDriverName)
	}
	d.remoteStorage = storage

	p, err := resolveSecret(d.Password)
	if err != nil {
		return fmt.Errorf("failed to resolve password: %w", err)
	}
	p2, err := resolveSecret(d.Salt)
	if err != nil {
		return fmt.Errorf("failed to resolve salt: %w", err)
	}
	c, err := cryptx.NewCipher(cryptx.Config{
		Password:                p,
		Salt:                    p2,
		FilenameEncryption:      d.FileNameEnc,
		DirectoryNameEncryption: d.DirNameEnc == "true",
		FilenameEncoding:        d.FileNameEncoding,
		EncryptedSuffix:         d.EncryptedSuffix,
		PassBadBlocks:           false,
	})
	if err != nil {
		return fmt.Errorf("failed to create Cipher: %w", err)
	}
	d.cipher = c
	d.initCaches()

	return nil
}

func (d *Crypt) initCaches() {
	if d.nameEncCache == nil {
		d.nameEncCache = cachex.NewBoundedTTLCache[string](cryptNameCacheMaxEntries, 16)
	}
	if d.nameDecCache == nil {
		d.nameDecCache = cachex.NewBoundedTTLCache[cryptNameDecodeResult](cryptNameCacheMaxEntries, 16)
	}
	if d.pathCache == nil {
		d.pathCache = cachex.NewBoundedTTLCache[string](cryptPathCacheMaxEntries, 16)
	}
}

func (d *Crypt) updateObfusParm(str *string) error {
	temp := *str
	if temp == "" {
		return nil
	}
	if !strings.HasPrefix(temp, obfuscatedPrefix) {
		temp, err := cryptx.Obscure(temp)
		if err != nil {
			return err
		}
		temp = obfuscatedPrefix + temp
		*str = temp
	}
	return nil
}

func resolveSecret(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	if !strings.HasPrefix(secret, obfuscatedPrefix) {
		return secret, nil
	}
	raw, _ := strings.CutPrefix(secret, obfuscatedPrefix)
	return cryptx.Reveal(raw)
}

func (d *Crypt) Drop(ctx context.Context) error {
	if d.nameEncCache != nil {
		d.nameEncCache.Clear()
	}
	if d.nameDecCache != nil {
		d.nameDecCache.Clear()
	}
	if d.pathCache != nil {
		d.pathCache.Clear()
	}
	return nil
}

func (d *Crypt) List(ctx context.Context, dir model.Obj, args model.ListArgs) ([]model.Obj, error) {
	path := dir.GetPath()
	//return d.list(ctx, d.RemotePath, path)
	//remoteFull

	objs, err := fs.List(ctx, d.getPathForRemote(path, true), &fs.ListArgs{NoLog: true})
	// the obj must implement the model.SetPath interface
	// return objs, err
	if err != nil {
		return nil, err
	}

	result := make([]model.Obj, 0, len(objs))
	for _, obj := range objs {
		if obj.IsDir() {
			name, err := d.decryptDirName(obj.GetName())
			if err != nil {
				// filter illegal files
				continue
			}
			if !d.ShowHidden && strings.HasPrefix(name, ".") {
				continue
			}
			objRes := model.Object{
				Name:     name,
				Size:     0,
				Modified: obj.ModTime(),
				IsFolder: obj.IsDir(),
				Ctime:    obj.CreateTime(),
				// discarding hash as it's encrypted
			}
			result = append(result, &objRes)
		} else {
			thumb, ok := model.GetThumb(obj)
			size, err := d.cipher.DecryptedSize(obj.GetSize())
			if err != nil {
				// filter illegal files
				continue
			}
			name, err := d.decryptFileName(obj.GetName())
			if err != nil {
				// filter illegal files
				continue
			}
			if !d.ShowHidden && strings.HasPrefix(name, ".") {
				continue
			}
			objRes := model.Object{
				Name:     name,
				Size:     size,
				Modified: obj.ModTime(),
				IsFolder: obj.IsDir(),
				Ctime:    obj.CreateTime(),
				// discarding hash as it's encrypted
			}
			if d.Thumbnail && thumb == "" {
				thumb = utils.EncodePath(common.GetApiUrl(nil)+stdpath.Join("/d", args.ReqPath, ".thumbnails", name+".webp"), true)
			}
			if !ok && !d.Thumbnail {
				result = append(result, &objRes)
			} else {
				objWithThumb := model.ObjThumb{
					Object: objRes,
					Thumbnail: model.Thumbnail{
						Thumbnail: thumb,
					},
				}
				result = append(result, &objWithThumb)
			}
		}
	}

	return result, nil
}

func (d *Crypt) Get(ctx context.Context, path string) (model.Obj, error) {
	if utils.PathEqual(path, "/") {
		return &model.Object{
			Name:     "Root",
			IsFolder: true,
			Path:     "/",
		}, nil
	}
	remoteFullPath := ""
	var remoteObj model.Obj
	var err, err2 error
	firstTryIsFolder, secondTry := guessPath(path)
	remoteFullPath = d.getPathForRemote(path, firstTryIsFolder)
	remoteObj, err = fs.Get(ctx, remoteFullPath, &fs.GetArgs{NoLog: true})
	if err != nil {
		if errs.IsObjectNotFound(err) && secondTry {
			//try the opposite
			remoteFullPath = d.getPathForRemote(path, !firstTryIsFolder)
			remoteObj, err2 = fs.Get(ctx, remoteFullPath, &fs.GetArgs{NoLog: true})
			if err2 != nil {
				return nil, err2
			}
		} else {
			return nil, err
		}
	}
	var size int64
	name := displayNameFromPath(path)
	if !remoteObj.IsDir() {
		size, err = d.cipher.DecryptedSize(remoteObj.GetSize())
		if err != nil {
			log.Warnf("DecryptedSize failed for %s ,will use original size, err:%s", path, err)
			size = remoteObj.GetSize()
		}
	} else {
		size = 0
	}
	if !d.ShowHidden && strings.HasPrefix(name, ".") {
		return nil, errs.ObjectNotFound
	}
	obj := &model.Object{
		Path:     path,
		Name:     name,
		Size:     size,
		Modified: remoteObj.ModTime(),
		IsFolder: remoteObj.IsDir(),
	}
	return obj, nil
	//return nil, errs.ObjectNotFound
}

func (d *Crypt) Link(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	dstDirActualPath, err := d.getActualPathForRemote(file.GetPath(), false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	remoteLink, remoteFile, err := op.Link(ctx, d.remoteStorage, dstDirActualPath, args)
	if err != nil {
		return nil, err
	}

	if remoteLink.RangeReadCloser == nil && remoteLink.MFile == nil && len(remoteLink.URL) == 0 {
		return nil, fmt.Errorf("the remote storage driver needs to support encrypted link reading")
	}
	remoteFileSize := remoteFile.GetSize()
	remoteClosers := utils.EmptyClosers()
	remoteRangeReader := remoteLink.RangeReadCloser
	if remoteRangeReader == nil && len(remoteLink.URL) > 0 {
		converted, err := newCryptHTTPRangeReadCloser(remoteFileSize, remoteLink)
		if err != nil {
			return nil, err
		}
		remoteRangeReader = converted
	}
	if remoteRangeReader != nil {
		remoteClosers.Add(remoteRangeReader)
	}
	if remoteLink.MFile != nil {
		remoteClosers.Add(remoteLink.MFile)
	}
	rangeReaderFunc := func(ctx context.Context, underlyingOffset, underlyingLength int64) (io.ReadCloser, error) {
		length := underlyingLength
		if underlyingLength >= 0 && underlyingOffset+underlyingLength >= remoteFileSize {
			length = -1
		}
		if remoteRangeReader != nil {
			remoteReader, err := remoteRangeReader.RangeRead(ctx, http_range.Range{Start: underlyingOffset, Length: length})
			if err != nil {
				return nil, err
			}
			return remoteReader, nil
		}
		if remoteLink.MFile != nil {
			if remoteFileSize > 0 && underlyingOffset >= remoteFileSize {
				return io.NopCloser(strings.NewReader("")), nil
			}
			if length < 0 {
				if remoteFileSize > 0 {
					length = remoteFileSize - underlyingOffset
				} else {
					length = 0
				}
			}
			if length < 0 {
				length = 0
			}
			return io.NopCloser(io.NewSectionReader(remoteLink.MFile, underlyingOffset, length)), nil
		}

		return nil, errs.NotSupport

	}
	resultRangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
		readSeeker, err := d.cipher.DecryptDataSeek(ctx, rangeReaderFunc, httpRange.Start, httpRange.Length)
		if err != nil {
			return nil, err
		}
		return readSeeker, nil
	}

	resultLink := &model.Link{
		Header:          remoteLink.Header,
		RangeReadCloser: newCryptRangeReadCloser(resultRangeReader, remoteClosers),
		Expiration:      remoteLink.Expiration,
	}

	return resultLink, nil

}

func (d *Crypt) MakeDir(ctx context.Context, parentDir model.Obj, dirName string) error {
	dstDirActualPath, err := d.getActualPathForRemote(parentDir.GetPath(), true)
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	dir := d.encryptDirName(dirName)
	return op.MakeDir(ctx, d.remoteStorage, stdpath.Join(dstDirActualPath, dir))
}

func (d *Crypt) Move(ctx context.Context, srcObj, dstDir model.Obj) error {
	srcRemoteActualPath, err := d.getActualPathForRemote(srcObj.GetPath(), srcObj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	dstRemoteActualPath, err := d.getActualPathForRemote(dstDir.GetPath(), dstDir.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	return op.Move(ctx, d.remoteStorage, srcRemoteActualPath, dstRemoteActualPath)
}

func (d *Crypt) Rename(ctx context.Context, srcObj model.Obj, newName string) error {
	remoteActualPath, err := d.getActualPathForRemote(srcObj.GetPath(), srcObj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	var newEncryptedName string
	if srcObj.IsDir() {
		newEncryptedName = d.encryptDirName(newName)
	} else {
		newEncryptedName = d.encryptFileName(newName)
	}
	return op.Rename(ctx, d.remoteStorage, remoteActualPath, newEncryptedName)
}

func (d *Crypt) Copy(ctx context.Context, srcObj, dstDir model.Obj) error {
	srcRemoteActualPath, err := d.getActualPathForRemote(srcObj.GetPath(), srcObj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	dstRemoteActualPath, err := d.getActualPathForRemote(dstDir.GetPath(), dstDir.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	return op.Copy(ctx, d.remoteStorage, srcRemoteActualPath, dstRemoteActualPath)

}

func (d *Crypt) Remove(ctx context.Context, obj model.Obj) error {
	remoteActualPath, err := d.getActualPathForRemote(obj.GetPath(), obj.IsDir())
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	return op.Remove(ctx, d.remoteStorage, remoteActualPath)
}

func (d *Crypt) Put(ctx context.Context, dstDir model.Obj, streamer model.FileStreamer, up driver.UpdateProgress) error {
	dstDirActualPath, err := d.getActualPathForRemote(dstDir.GetPath(), true)
	if err != nil {
		return fmt.Errorf("failed to convert path to remote path: %w", err)
	}
	return d.putBaiduOptimized(ctx, dstDirActualPath, streamer, up)
}

func (d *Crypt) putBaiduOptimized(ctx context.Context, dstDirActualPath string, streamer model.FileStreamer, up driver.UpdateProgress) error {
	// Baidu upload path eventually requires a seekable encrypted stream.
	// Materialize encrypted data once here so Baidu driver can reuse the same file
	// and we can release plaintext resources early.
	wrappedIn, err := d.cipher.EncryptData(streamer)
	if err != nil {
		return fmt.Errorf("failed to EncryptData for baidu optimized put: %w", err)
	}
	var wrappedCloser io.Closer
	if c, ok := wrappedIn.(io.Closer); ok {
		wrappedCloser = c
	}
	sizeHint := encryptedSizeHint(d.cipher, streamer.GetSize())
	tmp, err := utils.CreateTempFile(wrappedIn, sizeHint)
	if err != nil {
		closeSilently(wrappedCloser, "crypt: close encrypted wrapper failed: %v")
		return fmt.Errorf("failed to build encrypted temp file for baidu optimized put: %w", err)
	}
	closeSilently(wrappedCloser, "crypt: close encrypted wrapper failed: %v")
	wrappedCloser = nil
	encFile := newTempFileWithCleanup(tmp)
	encryptedSize := sizeHint
	if stat, statErr := tmp.Stat(); statErr == nil {
		encryptedSize = stat.Size()
	}
	if f, ok := interface{}(tmp).(cacheHintFile); ok {
		_ = utils.DropFileCacheByFD(f.Fd(), 0, 0)
	}
	defer func() {
		if encFile != nil {
			_ = encFile.Close()
		}
	}()

	// Release upstream plaintext data as soon as encrypted temp has been generated.
	closeSilently(streamer, "crypt: early close source stream in baidu optimized put failed: %v")

	streamOut := &stream.FileStream{
		Obj: &model.Object{
			ID:       streamer.GetID(),
			Path:     streamer.GetPath(),
			Name:     d.encryptFileName(streamer.GetName()),
			Size:     encryptedSize,
			Modified: streamer.ModTime(),
			IsFolder: streamer.IsDir(),
		},
		Reader:            encFile,
		Mimetype:          "application/octet-stream",
		WebPutAsTask:      false,
		ForceStreamUpload: true,
		Exist:             streamer.GetExist(),
	}
	streamOut.Add(encFile)
	if err := op.Put(ctx, d.remoteStorage, dstDirActualPath, streamOut, up, false); err != nil {
		return err
	}
	encFile = nil
	return nil
}

//func (d *Safe) Other(ctx context.Context, args model.OtherArgs) (interface{}, error) {
//	return nil, errs.NotSupport
//}

var _ driver.Driver = (*Crypt)(nil)
