package s3server

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/pkg/utils/random"
)

const s3MetaDirName = ".alist-s3-meta"

type storedObjectMetadata struct {
	BackendPath  string    `json:"backend_path"`
	ETag         string    `json:"etag"`
	ContentType  string    `json:"content_type,omitempty"`
	Size         int64     `json:"size"`
	ModTimeUnix  int64     `json:"mod_time_unix"`
	Multipart    bool      `json:"multipart,omitempty"`
	PartCount    int       `json:"part_count,omitempty"`
	UpdatedAtUTC time.Time `json:"updated_at_utc"`
}

type objectMetaCacheEntry struct {
	Size        int64
	ModTimeUnix int64
	Meta        storedObjectMetadata
}

var objectMetaCache sync.Map

type multipartUploadPart struct {
	Number int
	ETag   string
	Size   int64
	Path   string
}

type multipartUpload struct {
	UploadID       string
	Bucket         string
	Key            string
	BucketRootPath string
	ObjectPath     string
	ContentType    string
	TempDir        string
	CreatedAt      time.Time
	mu             sync.Mutex
	parts          map[int]multipartUploadPart
}

var multipartUploadStore sync.Map

func objectMetaFilePath(bucketRootFullPath, objectPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(objectPath)))
	return filepath.Join(bucketRootFullPath, s3MetaDirName, hex.EncodeToString(sum[:])+".json")
}

func contentTypeForName(name, fallback string) string {
	if ct := strings.TrimSpace(fallback); ct != "" {
		return ct
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func loadStoredObjectMetadata(bucketRootFullPath, objectPath, objectName, contentTypeHint string) (storedObjectMetadata, bool) {
	b, err := os.ReadFile(objectMetaFilePath(bucketRootFullPath, objectPath))
	if err != nil {
		return storedObjectMetadata{}, false
	}
	var meta storedObjectMetadata
	if err := json.Unmarshal(b, &meta); err != nil {
		return storedObjectMetadata{}, false
	}
	if meta.BackendPath == "" {
		meta.BackendPath = objectPath
	}
	if meta.ContentType == "" {
		meta.ContentType = contentTypeForName(objectName, contentTypeHint)
	}
	return meta, true
}

func saveStoredObjectMetadata(bucketRootFullPath string, meta storedObjectMetadata) error {
	if meta.BackendPath == "" {
		return fmt.Errorf("missing backend path")
	}
	metaFile := objectMetaFilePath(bucketRootFullPath, meta.BackendPath)
	if err := os.MkdirAll(filepath.Dir(metaFile), 0o755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(filepath.Dir(metaFile), "meta-*.json")
	if err != nil {
		return err
	}
	enc, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return err
	}
	if _, err := tmpFile.Write(enc); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		return err
	}
	if err := os.Rename(tmpFile.Name(), metaFile); err != nil {
		_ = os.Remove(tmpFile.Name())
		return err
	}
	objectMetaCache.Store(meta.BackendPath, objectMetaCacheEntry{
		Size:        meta.Size,
		ModTimeUnix: meta.ModTimeUnix,
		Meta:        meta,
	})
	return nil
}

func deleteStoredObjectMetadata(bucketRootFullPath, objectPath string) error {
	objectMetaCache.Delete(objectPath)
	if err := os.Remove(objectMetaFilePath(bucketRootFullPath, objectPath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func resolveObjectMetadata(bucketRootFullPath, objectPath, objectName, contentTypeHint string) (storedObjectMetadata, error) {
	stat, err := os.Stat(objectPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storedObjectMetadata{}, errs.ObjectNotFound
		}
		return storedObjectMetadata{}, err
	}
	if cached, ok := objectMetaCache.Load(objectPath); ok {
		entry := cached.(objectMetaCacheEntry)
		if entry.Size == stat.Size() && entry.ModTimeUnix == stat.ModTime().UTC().UnixNano() {
			return entry.Meta, nil
		}
	}
	if meta, ok := loadStoredObjectMetadata(bucketRootFullPath, objectPath, objectName, contentTypeHint); ok {
		if meta.Size == stat.Size() && meta.ModTimeUnix == stat.ModTime().UTC().UnixNano() {
			objectMetaCache.Store(objectPath, objectMetaCacheEntry{
				Size:        meta.Size,
				ModTimeUnix: meta.ModTimeUnix,
				Meta:        meta,
			})
			return meta, nil
		}
	}
	md5Hex, err := md5HexOfFile(objectPath)
	if err != nil {
		return storedObjectMetadata{}, err
	}
	meta := storedObjectMetadata{
		BackendPath:  objectPath,
		ETag:         quoteETag(md5Hex),
		ContentType:  contentTypeForName(objectName, contentTypeHint),
		Size:         stat.Size(),
		ModTimeUnix:  stat.ModTime().UTC().UnixNano(),
		UpdatedAtUTC: time.Now().UTC(),
	}
	_ = saveStoredObjectMetadata(bucketRootFullPath, meta)
	return meta, nil
}

func md5HexOfFile(fullPath string) (string, error) {
	f, err := os.Open(fullPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func md5HexFromSeeker(rs io.ReadSeeker) (string, error) {
	if rs == nil {
		sum := md5.Sum(nil)
		return hex.EncodeToString(sum[:]), nil
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := md5.New()
	if _, err := io.Copy(h, rs); err != nil {
		return "", err
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func quoteETag(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, `"`))
	return `"` + raw + `"`
}

func multipartETagFromParts(parts []multipartUploadPart) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("no multipart parts")
	}
	h := md5.New()
	for _, part := range parts {
		partETag := strings.Trim(part.ETag, `"`)
		raw, err := hex.DecodeString(partETag)
		if err != nil {
			return "", err
		}
		if _, err := h.Write(raw); err != nil {
			return "", err
		}
	}
	return quoteETag(fmt.Sprintf("%x-%d", h.Sum(nil), len(parts))), nil
}

func createMultipartUpload(bucket, key, bucketRootPath, objectPath, contentType string) (*multipartUpload, error) {
	id := random.String(32)
	tempDir, err := os.MkdirTemp("", "alist-s3-mpu-*")
	if err != nil {
		return nil, err
	}
	upload := &multipartUpload{
		UploadID:       id,
		Bucket:         bucket,
		Key:            key,
		BucketRootPath: bucketRootPath,
		ObjectPath:     objectPath,
		ContentType:    contentTypeForName(path.Base(key), contentType),
		TempDir:        tempDir,
		CreatedAt:      time.Now().UTC(),
		parts:          make(map[int]multipartUploadPart),
	}
	multipartUploadStore.Store(id, upload)
	return upload, nil
}

func getMultipartUpload(uploadID string) (*multipartUpload, bool) {
	if uploadID == "" {
		return nil, false
	}
	v, ok := multipartUploadStore.Load(uploadID)
	if !ok {
		return nil, false
	}
	upload, ok := v.(*multipartUpload)
	return upload, ok
}

func removeMultipartUpload(uploadID string) {
	v, ok := multipartUploadStore.LoadAndDelete(uploadID)
	if !ok {
		return
	}
	upload, ok := v.(*multipartUpload)
	if !ok || upload == nil {
		return
	}
	_ = os.RemoveAll(upload.TempDir)
}

func (u *multipartUpload) storePart(body io.ReadSeeker, partNumber int) (multipartUploadPart, error) {
	if u == nil {
		return multipartUploadPart{}, fmt.Errorf("missing multipart upload")
	}
	if partNumber <= 0 {
		return multipartUploadPart{}, fmt.Errorf("invalid part number")
	}
	if body == nil {
		return multipartUploadPart{}, fmt.Errorf("missing body")
	}
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return multipartUploadPart{}, err
	}
	partPath := filepath.Join(u.TempDir, fmt.Sprintf("part-%06d", partNumber))
	partFile, err := os.Create(partPath)
	if err != nil {
		return multipartUploadPart{}, err
	}
	defer func() { _ = partFile.Close() }()
	h := md5.New()
	written, err := io.Copy(io.MultiWriter(partFile, h), body)
	if err != nil {
		_ = os.Remove(partPath)
		return multipartUploadPart{}, err
	}
	if err := partFile.Sync(); err != nil {
		_ = os.Remove(partPath)
		return multipartUploadPart{}, err
	}
	part := multipartUploadPart{
		Number: partNumber,
		ETag:   quoteETag(hex.EncodeToString(h.Sum(nil))),
		Size:   written,
		Path:   partPath,
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.parts[partNumber] = part
	return part, nil
}

func (u *multipartUpload) completedParts(requested []completeMultipartUploadPart) ([]multipartUploadPart, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	ordered := make([]multipartUploadPart, 0, len(requested))
	var last int
	for i, requestedPart := range requested {
		if requestedPart.PartNumber <= 0 {
			return nil, fmt.Errorf("invalid part number")
		}
		if i > 0 && requestedPart.PartNumber <= last {
			return nil, fmt.Errorf("invalid part order")
		}
		last = requestedPart.PartNumber
		stored, ok := u.parts[requestedPart.PartNumber]
		if !ok {
			return nil, fmt.Errorf("missing part %d", requestedPart.PartNumber)
		}
		if requestedPart.ETag != "" && !strings.EqualFold(quoteETag(strings.Trim(requestedPart.ETag, `"`)), stored.ETag) {
			return nil, fmt.Errorf("etag mismatch for part %d", requestedPart.PartNumber)
		}
		ordered = append(ordered, stored)
	}
	return ordered, nil
}

func (u *multipartUpload) buildCompletedObject(bucketRootPath, dstPath, fileName, contentType string, parts []multipartUploadPart) (storedObjectMetadata, error) {
	finalFile, err := os.CreateTemp(u.TempDir, "complete-*")
	if err != nil {
		return storedObjectMetadata{}, err
	}
	finalPath := finalFile.Name()
	defer func() { _ = finalFile.Close() }()
	for _, part := range parts {
		partFile, err := os.Open(part.Path)
		if err != nil {
			_ = os.Remove(finalPath)
			return storedObjectMetadata{}, err
		}
		if _, err := io.Copy(finalFile, partFile); err != nil {
			_ = partFile.Close()
			_ = os.Remove(finalPath)
			return storedObjectMetadata{}, err
		}
		_ = partFile.Close()
	}
	if err := finalFile.Sync(); err != nil {
		_ = os.Remove(finalPath)
		return storedObjectMetadata{}, err
	}
	if _, err := finalFile.Seek(0, io.SeekStart); err != nil {
		_ = os.Remove(finalPath)
		return storedObjectMetadata{}, err
	}
	stat, err := finalFile.Stat()
	if err != nil {
		_ = os.Remove(finalPath)
		return storedObjectMetadata{}, err
	}
	etag, err := multipartETagFromParts(parts)
	if err != nil {
		_ = os.Remove(finalPath)
		return storedObjectMetadata{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		_ = os.Remove(finalPath)
		return storedObjectMetadata{}, err
	}
	if err := os.Rename(finalPath, dstPath); err != nil {
		_ = os.Remove(finalPath)
		return storedObjectMetadata{}, err
	}
	now := time.Now().UTC()
	meta := storedObjectMetadata{
		BackendPath:  dstPath,
		ETag:         etag,
		ContentType:  contentTypeForName(fileName, contentType),
		Size:         stat.Size(),
		ModTimeUnix:  now.UnixNano(),
		Multipart:    true,
		PartCount:    len(parts),
		UpdatedAtUTC: now,
	}
	_ = saveStoredObjectMetadata(bucketRootPath, meta)
	_ = os.Chtimes(dstPath, now, now)
	_ = os.Remove(finalPath)
	return meta, nil
}
