package s3server

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type handler struct{}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rw := &s3ResponseWriter{ResponseWriter: w, req: r}
	defer func() {
		if rec := recover(); rec != nil {
			log.WithFields(s3RequestLogFields(r)).
				Errorf("[s3] panic recovered: %v\n%s", rec, debug.Stack())
			if !rw.wroteHeader {
				writeS3Error(rw, http.StatusInternalServerError, "InternalError", "internal server error")
			}
		}
	}()

	cfg, err := loadConfig()
	if err != nil {
		writeS3Error(rw, http.StatusServiceUnavailable, "ServiceUnavailable", err.Error())
		return
	}
	if !cfg.enabled() {
		writeS3Error(rw, http.StatusServiceUnavailable, "ServiceUnavailable", "s3 service is not configured")
		return
	}

	body, cleanup, err := prepareBody(r)
	if err != nil {
		writeS3Error(rw, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	if cleanup != nil {
		defer cleanup()
	}

	if err := verifyRequestSignature(r, body, cfg); err != nil {
		writeS3Error(rw, http.StatusForbidden, "AccessDenied", err.Error())
		return
	}
	if body != nil {
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			writeS3Error(rw, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
	}

	bucket, key := resolveRequestTarget(r, cfg)
	if bucket == "" {
		h.serveRootBuckets(rw, r, cfg)
		return
	}

	if key == "" {
		switch {
		case r.Method == http.MethodGet || r.Method == http.MethodHead:
			h.serveBucket(rw, r, cfg, bucket)
		case r.Method == http.MethodPut:
			h.serveCreateBucket(rw, r, cfg, bucket)
		case r.Method == http.MethodDelete:
			h.serveDeleteBucket(rw, r, cfg, bucket)
		default:
			writeS3Error(rw, http.StatusMethodNotAllowed, "MethodNotAllowed", "unsupported method")
		}
		return
	}

	if r.Method == http.MethodPost && strings.Contains(r.URL.RawQuery, "uploads") {
		h.serveInitiateMultipartUpload(rw, r, cfg, bucket, key)
		return
	}
	if r.Method == http.MethodPut && r.URL.Query().Has("partNumber") && r.URL.Query().Has("uploadId") {
		h.serveUploadPart(rw, r, cfg, bucket, key, body)
		return
	}
	if r.Method == http.MethodPost && r.URL.Query().Has("uploadId") {
		h.serveCompleteMultipartUpload(rw, r, cfg, bucket, key, body)
		return
	}
	if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
		h.serveBucketListV2(rw, r, cfg, bucket)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveObjectGet(rw, r, cfg, bucket, key)
	case http.MethodPut:
		h.serveObjectPut(rw, r, cfg, bucket, key, body)
	case http.MethodDelete:
		h.serveObjectDelete(rw, r, cfg, bucket, key)
	default:
		writeS3Error(rw, http.StatusMethodNotAllowed, "MethodNotAllowed", "unsupported method")
	}
}

func prepareBody(r *http.Request) (io.ReadSeeker, func(), error) {
	if r.Body == nil {
		return nil, nil, nil
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost && r.Method != http.MethodPatch {
		return nil, nil, nil
	}
	tmp, err := os.CreateTemp("", "alist-s3-*")
	if err != nil {
		return nil, nil, err
	}
	if _, err := io.Copy(tmp, r.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, nil, err
	}
	_ = r.Body.Close()
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, nil, err
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}
	return tmp, cleanup, nil
}

func splitRequestPath(rawPath string) (bucket, key string) {
	clean := strings.TrimPrefix(rawPath, "/")
	if clean == "" {
		return "", ""
	}
	parts := strings.SplitN(clean, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		key = strings.TrimPrefix(parts[1], "/")
	}
	return bucket, key
}

func resolveRequestTarget(r *http.Request, cfg *Config) (bucket, key string) {
	bucket, key = splitRequestPath(r.URL.Path)
	if bucket != "" {
		if _, ok := cfg.bucketPath(bucket); ok {
			return bucket, key
		}
	}
	return bucket, key
}

func (h *handler) serveRootBuckets(w http.ResponseWriter, r *http.Request, cfg *Config) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "unsupported method")
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	now := time.Now().UTC()
	resp := listBucketsResponse{
		XMLName: xml.Name{Local: "ListAllMyBucketsResult"},
		Xmlns:   "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner: owner{
			ID:          "alist",
			DisplayName: "alist",
		},
		Buckets: make([]bucketInfo, 0, len(cfg.Buckets)),
	}
	for name, mountPath := range cfg.Buckets {
		if _, err := resolveBucketTarget(cfg, name); err != nil {
			continue
		}
		resp.Buckets = append(resp.Buckets, bucketInfo{
			Name:         name,
			CreationDate: now.Format(awsXMLTimeLayout),
		})
		_ = mountPath
	}
	sort.Slice(resp.Buckets, func(i, j int) bool {
		return resp.Buckets[i].Name < resp.Buckets[j].Name
	})
	writeXML(w, http.StatusOK, resp)
}

func (h *handler) serveBucket(w http.ResponseWriter, r *http.Request, cfg *Config, bucket string) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := r.URL.Query()["location"]; ok {
			writeXML(w, http.StatusOK, locationConstraintResponse{LocationConstraint: cfg.Region})
			return
		}
		h.serveBucketListV2(w, r, cfg, bucket)
	case http.MethodHead:
		if _, err := resolveBucketTarget(cfg, bucket); err != nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "unsupported method")
	}
}

func (h *handler) serveCreateBucket(w http.ResponseWriter, r *http.Request, cfg *Config, bucket string) {
	if _, err := resolveBucketTarget(cfg, bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *handler) serveDeleteBucket(w http.ResponseWriter, r *http.Request, cfg *Config, bucket string) {
	if _, err := resolveBucketTarget(cfg, bucket); err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) serveBucketListV2(w http.ResponseWriter, r *http.Request, cfg *Config, bucket string) {
	rootPath, err := resolveBucketTarget(cfg, bucket)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
		return
	}

	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	startAfter := r.URL.Query().Get("start-after")
	continuationToken := r.URL.Query().Get("continuation-token")
	maxKeys := 1000
	if raw := strings.TrimSpace(r.URL.Query().Get("max-keys")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxKeys = n
		}
	}

	objects, err := listBucketObjects(rootPath)
	if err != nil && !errs.IsObjectNotFound(err) {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	sort.Slice(objects, func(i, j int) bool { return objects[i].key < objects[j].key })

	startKey := continuationToken
	if startKey == "" {
		startKey = startAfter
	}

	contents := make([]listObjectInfo, 0, len(objects))
	commonPrefixes := make([]commonPrefix, 0)
	prefixSet := make(map[string]struct{})
	truncated := false
	started := startKey == ""
	for _, obj := range objects {
		keyName := obj.key
		if !started {
			if keyName > startKey {
				started = true
			} else {
				continue
			}
		}
		if prefix != "" && !strings.HasPrefix(keyName, prefix) {
			continue
		}
		if delimiter == "/" {
			remain := strings.TrimPrefix(keyName, prefix)
			remain = strings.TrimPrefix(remain, "/")
			if idx := strings.Index(remain, "/"); idx >= 0 {
				common := prefix + remain[:idx+1]
				if _, ok := prefixSet[common]; !ok {
					prefixSet[common] = struct{}{}
					commonPrefixes = append(commonPrefixes, commonPrefix{Prefix: common})
				}
				continue
			}
		}
		if obj.isDir {
			continue
		}
		meta, err := resolveObjectMetadata(rootPath, obj.fullPath, path.Base(obj.fullPath), "")
		if err != nil {
			if os.IsNotExist(err) || errs.IsObjectNotFound(err) {
				log.WithFields(log.Fields{
					"bucket": bucket,
					"key":    keyName,
				}).Warnf("[s3] skip vanished object during list: %v", err)
				continue
			}
			writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		contents = append(contents, listObjectInfo{
			Key:          keyName,
			LastModified: time.Unix(0, meta.ModTimeUnix).UTC().Format(awsXMLTimeLayout),
			ETag:         meta.ETag,
			Size:         meta.Size,
			StorageClass: "STANDARD",
		})
		if len(contents) >= maxKeys {
			truncated = true
			break
		}
	}
	sort.Slice(contents, func(i, j int) bool { return contents[i].Key < contents[j].Key })
	sort.Slice(commonPrefixes, func(i, j int) bool { return commonPrefixes[i].Prefix < commonPrefixes[j].Prefix })

	resp := listBucketV2Response{
		XMLName:               xml.Name{Local: "ListBucketResult"},
		Xmlns:                 "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:                  bucket,
		Prefix:                prefix,
		Delimiter:             delimiter,
		MaxKeys:               maxKeys,
		KeyCount:              len(contents),
		IsTruncated:           truncated,
		Contents:              contents,
		CommonPrefixes:        commonPrefixes,
		ContinuationToken:     continuationToken,
		NextContinuationToken: "",
		StartAfter:            startAfter,
	}
	if truncated && len(contents) > 0 {
		resp.NextContinuationToken = contents[len(contents)-1].Key
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *handler) serveObjectGet(w http.ResponseWriter, r *http.Request, cfg *Config, bucket, key string) {
	rootPath, objectPath, err := resolveObjectTarget(cfg, bucket, key)
	if err != nil {
		if errors.Is(err, errBucketNotFound) {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "object not found")
		return
	}
	stat, err := os.Stat(objectPath)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "object not found")
		return
	}
	if stat.IsDir() {
		writeS3Error(w, http.StatusNotFound, "NoSuchKey", "object not found")
		return
	}

	meta, err := resolveObjectMetadata(rootPath, objectPath, path.Base(objectPath), "")
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	contentType := meta.ContentType
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Last-Modified", stat.ModTime().UTC().Format(http.TimeFormat))
	w.Header().Set("ETag", meta.ETag)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	file, err := os.Open(objectPath)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	defer func() {
		_ = file.Close()
	}()
	http.ServeContent(w, r, path.Base(objectPath), stat.ModTime().UTC(), file)
}

func (h *handler) serveObjectPut(w http.ResponseWriter, r *http.Request, cfg *Config, bucket, key string, body io.ReadSeeker) {
	if key == "" {
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", "missing object key")
		return
	}

	rootPath, objectPath, err := resolveObjectTarget(cfg, bucket, key)
	if err != nil {
		if errors.Is(err, errBucketNotFound) {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", err.Error())
		return
	}

	if strings.HasSuffix(key, "/") {
		if err := os.MkdirAll(objectPath, 0o755); err != nil {
			writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	tmp, ok := body.(*os.File)
	if !ok {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "missing request body")
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	md5Hex, err := md5HexFromSeeker(tmp)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	size := int64(0)
	if fi, err := tmp.Stat(); err == nil {
		size = fi.Size()
	}
	now := time.Now().UTC()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	if err := os.MkdirAll(filepath.Dir(objectPath), 0o755); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	size, err = writeFileFromSeeker(objectPath, tmp)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	meta := storedObjectMetadata{
		BackendPath:  objectPath,
		ETag:         quoteETag(md5Hex),
		ContentType:  contentTypeFromRequest(r, key),
		Size:         size,
		ModTimeUnix:  now.UnixNano(),
		UpdatedAtUTC: now,
	}
	_ = saveStoredObjectMetadata(rootPath, meta)
	w.Header().Set("ETag", meta.ETag)
	w.WriteHeader(http.StatusOK)
}

func (h *handler) serveObjectDelete(w http.ResponseWriter, r *http.Request, cfg *Config, bucket, key string) {
	rootPath, objectPath, err := resolveObjectTarget(cfg, bucket, key)
	if err != nil {
		if errors.Is(err, errBucketNotFound) {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", err.Error())
		return
	}
	if err := removeLocalObject(objectPath); err != nil && !errs.IsObjectNotFound(err) {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	_ = deleteStoredObjectMetadata(rootPath, objectPath)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) serveInitiateMultipartUpload(w http.ResponseWriter, r *http.Request, cfg *Config, bucket, key string) {
	rootPath, objectPath, err := resolveObjectTarget(cfg, bucket, key)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
		return
	}
	upload, err := createMultipartUpload(bucket, key, rootPath, objectPath, r.Header.Get("Content-Type"))
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	resp := initiateMultipartUploadResult{
		XMLName:  xml.Name{Local: "InitiateMultipartUploadResult"},
		Bucket:   bucket,
		Key:      key,
		UploadID: upload.UploadID,
	}
	writeXML(w, http.StatusOK, resp)
}

func (h *handler) serveUploadPart(w http.ResponseWriter, r *http.Request, cfg *Config, bucket, key string, body io.ReadSeeker) {
	uploadID := r.URL.Query().Get("uploadId")
	partNumber, _ := strconv.Atoi(r.URL.Query().Get("partNumber"))
	upload, ok := getMultipartUpload(uploadID)
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "upload not found")
		return
	}
	part, err := upload.storePart(body, partNumber)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "InvalidPart", err.Error())
		return
	}
	w.Header().Set("ETag", part.ETag)
	w.WriteHeader(http.StatusOK)
	_ = cfg
	_ = bucket
	_ = key
}

func (h *handler) serveCompleteMultipartUpload(w http.ResponseWriter, r *http.Request, cfg *Config, bucket, key string, body io.ReadSeeker) {
	uploadID := r.URL.Query().Get("uploadId")
	upload, ok := getMultipartUpload(uploadID)
	if !ok {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "upload not found")
		return
	}
	var completeReq completeMultipartUploadRequest
	if body != nil {
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			writeS3Error(w, http.StatusBadRequest, "InvalidRequest", err.Error())
			return
		}
		if err := xml.NewDecoder(body).Decode(&completeReq); err != nil && err != io.EOF {
			writeS3Error(w, http.StatusBadRequest, "MalformedXML", err.Error())
			return
		}
	}
	parts, err := upload.completedParts(completeReq.Parts)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "InvalidPart", err.Error())
		return
	}
	rootPath, objectPath, err := resolveObjectTarget(cfg, bucket, key)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
		return
	}
	meta, err := upload.buildCompletedObject(rootPath, objectPath, path.Base(objectPath), upload.ContentType, parts)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	removeMultipartUpload(uploadID)
	resp := completeMultipartUploadResult{
		XMLName:  xml.Name{Local: "CompleteMultipartUploadResult"},
		Location: buildObjectLocation(cfg, bucket, key),
		Bucket:   bucket,
		Key:      key,
		ETag:     meta.ETag,
	}
	writeXML(w, http.StatusOK, resp)
}

func buildObjectLocation(cfg *Config, bucket, key string) string {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return path.Join("/", bucket, key)
	}
	return strings.TrimRight(endpoint, "/") + "/" + bucket + "/" + key
}

func contentTypeFromRequest(r *http.Request, key string) string {
	if ct := strings.TrimSpace(r.Header.Get("Content-Type")); ct != "" {
		return ct
	}
	if ct := mime.TypeByExtension(path.Ext(key)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func writeFileFromSeeker(dstPath string, src io.ReadSeeker) (int64, error) {
	if src == nil {
		return 0, fmt.Errorf("missing source")
	}
	tmpPath := dstPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = f.Close()
	}()
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		_ = os.Remove(tmpPath)
		return 0, err
	}
	written, err := io.Copy(f, src)
	if err != nil {
		_ = os.Remove(tmpPath)
		return 0, err
	}
	if err := f.Sync(); err != nil {
		_ = os.Remove(tmpPath)
		return 0, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return 0, err
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		_ = os.Remove(tmpPath)
		return 0, err
	}
	return written, nil
}

func removeLocalObject(objectPath string) error {
	if objectPath == "" {
		return fmt.Errorf("missing object path")
	}
	if err := os.Remove(objectPath); err != nil {
		if os.IsNotExist(err) {
			return errs.ObjectNotFound
		}
		return err
	}
	return nil
}

func writeXML(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(v)
}

func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	req := requestFromWriter(w)
	if req != nil {
		log.WithFields(s3RequestLogFields(req)).
			WithField("http_status", status).
			WithField("s3_code", code).
			Error(message)
	} else {
		log.WithFields(log.Fields{
			"http_status": status,
			"s3_code":     code,
		}).Error(message)
	}
	writeXML(w, status, s3ErrorResponse{
		XMLName: xml.Name{Local: "Error"},
		Code:    code,
		Message: message,
	})
}

type s3ResponseWriter struct {
	http.ResponseWriter
	req         *http.Request
	wroteHeader bool
}

func (w *s3ResponseWriter) WriteHeader(status int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *s3ResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func requestFromWriter(w http.ResponseWriter) *http.Request {
	type requestCarrier interface {
		Request() *http.Request
	}
	if c, ok := w.(requestCarrier); ok {
		return c.Request()
	}
	if sw, ok := w.(*s3ResponseWriter); ok {
		return sw.req
	}
	return nil
}

func s3RequestLogFields(r *http.Request) log.Fields {
	fields := log.Fields{
		"method":               "",
		"host":                 "",
		"path":                 "",
		"query":                "",
		"remote_addr":          "",
		"user_agent":           "",
		"request_id":           "",
		"range":                "",
		"x_amz_date":           "",
		"x_amz_content_sha256": "",
		"if_modified_since":    "",
		"if_none_match":        "",
		"auth_type":            "",
	}
	if r == nil {
		return fields
	}
	fields["method"] = r.Method
	fields["host"] = r.Host
	fields["path"] = r.URL.Path
	fields["query"] = sanitizeQueryString(r.URL.RawQuery)
	fields["remote_addr"] = r.RemoteAddr
	fields["user_agent"] = r.UserAgent()
	fields["request_id"] = strings.TrimSpace(r.Header.Get("X-Request-ID"))
	fields["range"] = strings.TrimSpace(r.Header.Get("Range"))
	fields["x_amz_date"] = strings.TrimSpace(r.Header.Get("X-Amz-Date"))
	fields["x_amz_content_sha256"] = strings.TrimSpace(r.Header.Get("X-Amz-Content-Sha256"))
	fields["if_modified_since"] = strings.TrimSpace(r.Header.Get("If-Modified-Since"))
	fields["if_none_match"] = strings.TrimSpace(r.Header.Get("If-None-Match"))
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		if idx := strings.IndexByte(auth, ' '); idx > 0 {
			fields["auth_type"] = auth[:idx]
		} else {
			fields["auth_type"] = "present"
		}
	} else if r.URL != nil && strings.TrimSpace(r.URL.Query().Get("X-Amz-Signature")) != "" {
		fields["auth_type"] = "presign"
	}
	return fields
}

func sanitizeQueryString(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	for i, pair := range parts {
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		key := strings.ToLower(strings.TrimSpace(kv[0]))
		switch key {
		case "x-amz-signature", "x-amz-security-token", "x-amz-credential":
			if len(kv) == 1 {
				parts[i] = kv[0] + "=***"
			} else {
				parts[i] = kv[0] + "=***"
			}
		}
	}
	return strings.Join(parts, "&")
}

type s3ErrorResponse struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

type owner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type bucketInfo struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

type listBucketsResponse struct {
	XMLName xml.Name     `xml:"ListAllMyBucketsResult"`
	Xmlns   string       `xml:"xmlns,attr"`
	Owner   owner        `xml:"Owner"`
	Buckets []bucketInfo `xml:"Buckets>Bucket"`
}

type locationConstraintResponse struct {
	XMLName            xml.Name `xml:"LocationConstraint"`
	LocationConstraint string   `xml:",chardata"`
}

type commonPrefix struct {
	Prefix string `xml:"Prefix"`
}

type listObjectInfo struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

type listBucketV2Response struct {
	XMLName               xml.Name         `xml:"ListBucketResult"`
	Xmlns                 string           `xml:"xmlns,attr"`
	Name                  string           `xml:"Name"`
	Prefix                string           `xml:"Prefix"`
	Delimiter             string           `xml:"Delimiter,omitempty"`
	MaxKeys               int              `xml:"MaxKeys"`
	KeyCount              int              `xml:"KeyCount"`
	IsTruncated           bool             `xml:"IsTruncated"`
	Contents              []listObjectInfo `xml:"Contents"`
	CommonPrefixes        []commonPrefix   `xml:"CommonPrefixes>CommonPrefix,omitempty"`
	ContinuationToken     string           `xml:"ContinuationToken,omitempty"`
	NextContinuationToken string           `xml:"NextContinuationToken,omitempty"`
	StartAfter            string           `xml:"StartAfter,omitempty"`
}

type initiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadID string   `xml:"UploadId"`
}

type completeMultipartUploadRequest struct {
	Parts []completeMultipartUploadPart `xml:"Part"`
}

type completeMultipartUploadPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type completeMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

type resolvedObject struct {
	key      string
	fullPath string
	isDir    bool
}

var errBucketNotFound = errors.New("bucket not found")

func listBucketObjects(rootPath string) ([]resolvedObject, error) {
	rootPath = filepath.Clean(rootPath)
	objects := make([]resolvedObject, 0)
	err := filepath.WalkDir(rootPath, func(currentPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) || errs.IsObjectNotFound(walkErr) {
				return nil
			}
			return walkErr
		}
		if currentPath == rootPath {
			return nil
		}
		rel, err := filepath.Rel(rootPath, currentPath)
		if err != nil {
			return err
		}
		if rel == ".alist-s3-meta" || strings.HasPrefix(rel, ".alist-s3-meta"+string(filepath.Separator)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		objects = append(objects, resolvedObject{
			key:      filepath.ToSlash(rel),
			fullPath: currentPath,
			isDir:    d.IsDir(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return objects, nil
}

func resolveBucketTarget(cfg *Config, bucket string) (string, error) {
	bucketPath, ok := cfg.bucketPath(bucket)
	if !ok {
		return "", errBucketNotFound
	}
	rootPath := strings.TrimSpace(bucketPath.localRootPath)
	if rootPath == "" {
		return "", errBucketNotFound
	}
	rootPath = filepath.Clean(rootPath)
	if _, err := os.Stat(rootPath); err != nil {
		if os.IsNotExist(err) {
			return "", errBucketNotFound
		}
		return "", err
	}
	return rootPath, nil
}

func resolveObjectTarget(cfg *Config, bucket, key string) (string, string, error) {
	rootPath, err := resolveBucketTarget(cfg, bucket)
	if err != nil {
		return "", "", err
	}
	objectPath := rootPath
	if key != "" {
		objectPath = filepath.Clean(filepath.Join(rootPath, filepath.FromSlash(strings.TrimPrefix(key, "/"))))
		if !utils.IsSubPath(rootPath, objectPath) {
			return "", "", fmt.Errorf("invalid object key")
		}
	}
	return rootPath, objectPath, nil
}
