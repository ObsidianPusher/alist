package thumb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	inet "github.com/alist-org/alist/v3/internal/net"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/sign"
	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/disintegration/imaging"
	ffmpeg "github.com/u2takey/ffmpeg-go"
	"golang.org/x/sync/singleflight"
)

const (
	DefaultThumbSize  = 256
	DefaultCacheTTL   = 7 * 24 * time.Hour
	MaxVideoTempSize  = 128 << 20
	baiduThumbReadCap = 64 << 20
	baiduThumbChunk   = 8 << 20
)

var (
	thumbGroup    singleflight.Group
	thumbOnce     sync.Once
	thumbSem      chan struct{}
	baiduThumbSem chan struct{}
)

func initThumb() {
	thumbOnce.Do(func() {
		thumbSem = make(chan struct{}, 4)
		baiduThumbSem = make(chan struct{}, 1)
	})
}

func IsThumbCandidate(obj model.Obj) bool {
	if obj == nil || obj.IsDir() {
		return false
	}
	ext := strings.ToLower(utils.Ext(obj.GetName()))
	if ext == "svg" {
		return false
	}
	ft := utils.GetFileType(obj.GetName())
	return ft == conf.IMAGE || ft == conf.VIDEO
}

func CacheTTL() time.Duration {
	return DefaultCacheTTL
}

func cacheDir() string {
	return filepath.Join(conf.Conf.TempDir, "thumbs")
}

func cacheKey(storage driver.Driver, obj model.Obj, rawPath string) string {
	storageID := uint(0)
	if storage != nil && storage.GetStorage() != nil {
		storageID = storage.GetStorage().ID
	}
	mod := obj.ModTime().Unix()
	key := fmt.Sprintf("%d:%s:%d:%d", storageID, rawPath, obj.GetSize(), mod)
	return utils.GetMD5EncodeStr(key)
}

func cachePath(key string) string {
	return filepath.Join(cacheDir(), key+".jpg")
}

func cacheFresh(path string, ttl time.Duration) bool {
	if ttl <= 0 {
		return utils.Exists(path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) <= ttl
}

type readerCloser struct {
	r     io.Reader
	close func() error
}

func (r readerCloser) Read(p []byte) (int, error) {
	return r.r.Read(p)
}

func (r readerCloser) Close() error {
	if r.close == nil {
		return nil
	}
	return r.close()
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	select {
	case <-c.ctx.Done():
		return 0, c.ctx.Err()
	default:
		return c.r.Read(p)
	}
}

func openLinkReader(ctx context.Context, link *model.Link) (io.ReadCloser, error) {
	if link == nil {
		return nil, errors.New("empty link")
	}
	if link.MFile != nil {
		return link.MFile, nil
	}
	if link.RangeReadCloser != nil {
		rc, err := link.RangeReadCloser.RangeRead(ctx, http_range.Range{Start: 0, Length: -1})
		if err != nil {
			return nil, err
		}
		return readerCloser{
			r: rc,
			close: func() error {
				return link.RangeReadCloser.Close()
			},
		}, nil
	}
	if link.URL != "" {
		header := inet.ProcessHeader(http.Header{}, link.Header)
		resp, err := inet.RequestHttp(ctx, "GET", header, link.URL)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
	return nil, errors.New("empty link content")
}

func decodeImage(r io.Reader) (image.Image, error) {
	return imaging.Decode(r, imaging.AutoOrientation(true))
}

func resizeImage(img image.Image) image.Image {
	return imaging.Fit(img, DefaultThumbSize, DefaultThumbSize, imaging.Lanczos)
}

func encodeJPEG(w io.Writer, img image.Image) error {
	return imaging.Encode(w, img, imaging.JPEG, imaging.JPEGQuality(85))
}

var videoSeekTimestamps = []string{"00:00:01", "00:00:00.1", "00:00:00"}

func buildSignedRawURL(rawPath string, args model.LinkArgs) string {
	if args.HttpReq == nil {
		return ""
	}
	baseURL := common.GetApiUrl(args.HttpReq)
	if baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/d%s?sign=%s", baseURL, utils.EncodePath(rawPath, true), sign.Sign(rawPath))
}

func cloneHeader(h http.Header) http.Header {
	if len(h) == 0 {
		return nil
	}
	out := make(http.Header, len(h))
	for k, v := range h {
		out[k] = append([]string{}, v...)
	}
	return out
}

func mergeHeader(primary, fallback http.Header) http.Header {
	if len(primary) == 0 {
		return cloneHeader(fallback)
	}
	out := cloneHeader(primary)
	for k, v := range fallback {
		if _, ok := out[k]; ok {
			continue
		}
		out[k] = append([]string{}, v...)
	}
	return out
}

func headerToFFmpeg(header http.Header) (string, string) {
	if len(header) == 0 {
		return "", ""
	}
	var b strings.Builder
	var ua string
	for k, vals := range header {
		if strings.EqualFold(k, "User-Agent") {
			if len(vals) > 0 {
				ua = vals[0]
			}
			continue
		}
		if strings.EqualFold(k, "Range") || strings.EqualFold(k, "Accept-Encoding") {
			continue
		}
		for _, v := range vals {
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\r\n")
		}
	}
	return b.String(), ua
}

func snapshotVideoFromURL(ctx context.Context, url string, header http.Header) (*bytes.Buffer, error) {
	if url == "" {
		return nil, errors.New("empty video url")
	}
	var lastErr error
	for _, ts := range videoSeekTimestamps {
		srcBuf := bytes.NewBuffer(nil)
		inputArgs := ffmpeg.KwArgs{
			"ss":         ts,
			"rw_timeout": "10000000",
		}
		if headerStr, ua := headerToFFmpeg(header); headerStr != "" || ua != "" {
			if headerStr != "" {
				inputArgs["headers"] = headerStr
			}
			if ua != "" {
				inputArgs["user_agent"] = ua
			}
		}
		if _, ok := inputArgs["user_agent"]; !ok {
			inputArgs["user_agent"] = base.UserAgent
		}
		stream := ffmpeg.Input(url, inputArgs).
			Output("pipe:", ffmpeg.KwArgs{"vframes": 1, "format": "image2", "vcodec": "mjpeg"}).
			GlobalArgs("-loglevel", "error", "-nostdin").Silent(true).
			WithOutput(srcBuf, os.Stdout)
		if ctx != nil {
			stream.Context = ctx
		}
		if err := stream.Run(); err != nil {
			lastErr = err
			continue
		}
		return srcBuf, nil
	}
	if lastErr == nil {
		lastErr = errors.New("ffmpeg failed to snapshot video url")
	}
	return nil, lastErr
}

func snapshotVideoFromPath(ctx context.Context, path string) (*bytes.Buffer, error) {
	if path == "" {
		return nil, errors.New("empty video path")
	}
	var lastErr error
	for _, ts := range videoSeekTimestamps {
		srcBuf := bytes.NewBuffer(nil)
		stream := ffmpeg.Input(path, ffmpeg.KwArgs{"ss": ts}).
			Output("pipe:", ffmpeg.KwArgs{"vframes": 1, "format": "image2", "vcodec": "mjpeg"}).
			GlobalArgs("-loglevel", "error", "-nostdin").Silent(true).
			WithOutput(srcBuf, os.Stdout)
		if ctx != nil {
			stream.Context = ctx
		}
		if err := stream.Run(); err != nil {
			lastErr = err
			continue
		}
		return srcBuf, nil
	}
	if lastErr == nil {
		lastErr = errors.New("ffmpeg failed to snapshot video path")
	}
	return nil, lastErr
}

type videoSourceKind int

const (
	videoSourceFile videoSourceKind = iota
	videoSourceURL
	videoSourceReader
)

type videoSource struct {
	kind          videoSourceKind
	path          string
	url           string
	header        http.Header
	readerFactory func(ctx context.Context) (io.ReadCloser, error)
	cleanup       func()
}

type cryptAddition struct {
	RemotePath string `json:"remote_path"`
}

func snapshotVideoFromReader(ctx context.Context, r io.Reader) (*bytes.Buffer, error) {
	srcBuf := bytes.NewBuffer(nil)
	stream := ffmpeg.Input("pipe:").
		Filter("select", ffmpeg.Args{fmt.Sprintf("gte(n,%d)", 10)}).
		Output("pipe:", ffmpeg.KwArgs{"vframes": 1, "format": "image2", "vcodec": "mjpeg"}).
		GlobalArgs("-loglevel", "error", "-nostdin").Silent(true).
		WithInput(r).
		WithOutput(srcBuf, os.Stdout)
	if ctx != nil {
		stream.Context = ctx
	}
	if err := stream.Run(); err != nil {
		return nil, err
	}
	return srcBuf, nil
}

func buildVideoSources(link *model.Link, rawPath string, args model.LinkArgs) []videoSource {
	var sources []videoSource
	if link != nil && link.MFile != nil {
		if f, ok := link.MFile.(*os.File); ok && f.Name() != "" {
			sources = append(sources, videoSource{
				kind: videoSourceFile,
				path: f.Name(),
			})
		}
	}
	if link != nil && link.URL != "" {
		header := mergeHeader(link.Header, args.Header)
		sources = append(sources, videoSource{
			kind:   videoSourceURL,
			url:    link.URL,
			header: header,
		})
	}
	if rawURL := buildSignedRawURL(rawPath, args); rawURL != "" {
		sources = append(sources, videoSource{
			kind: videoSourceURL,
			url:  rawURL,
		})
	}
	if link != nil {
		sources = append(sources, videoSource{
			kind: videoSourceReader,
			readerFactory: func(ctx context.Context) (io.ReadCloser, error) {
				return openLinkReader(ctx, link)
			},
		})
	}
	return sources
}

func snapshotVideoViaTempFile(ctx context.Context, link *model.Link, size int64) (*bytes.Buffer, error) {
	if link == nil || link.RangeReadCloser == nil {
		return nil, errors.New("no range reader")
	}
	defer link.RangeReadCloser.Close()
	if size <= 0 || size > MaxVideoTempSize {
		return nil, errors.New("video too large for temp snapshot")
	}
	tmp, err := os.CreateTemp(conf.Conf.TempDir, "thumb-video-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	rc, err := link.RangeReadCloser.RangeRead(ctx, http_range.Range{Start: 0, Length: -1})
	if err != nil {
		return nil, err
	}
	_, copyErr := io.Copy(tmp, rc)
	_ = rc.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	_ = tmp.Sync()
	return snapshotVideoFromPath(ctx, tmpName)
}

func snapshotVideoViaSegmentedTempFile(ctx context.Context, link *model.Link, size int64, limit int64) (*bytes.Buffer, error) {
	if link == nil || link.RangeReadCloser == nil {
		return nil, errors.New("no range reader")
	}
	defer link.RangeReadCloser.Close()
	if limit <= 0 {
		return nil, errors.New("invalid segment limit")
	}
	if size > 0 && size < limit {
		limit = size
	}
	tmp, err := os.CreateTemp(conf.Conf.TempDir, "thumb-video-seg-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	var (
		offset int64
		wrote  int64
	)
	for wrote < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		length := int64(baiduThumbChunk)
		remain := limit - wrote
		if remain < length {
			length = remain
		}
		rc, err := link.RangeReadCloser.RangeRead(ctx, http_range.Range{Start: offset, Length: length})
		if err != nil {
			return nil, err
		}
		n, copyErr := io.CopyN(tmp, rc, length)
		_ = rc.Close()
		wrote += n
		offset += n
		if copyErr == nil {
			continue
		}
		if errors.Is(copyErr, io.EOF) || errors.Is(copyErr, io.ErrUnexpectedEOF) {
			break
		}
		return nil, copyErr
	}
	if wrote == 0 {
		return nil, errors.New("empty video data for segmented snapshot")
	}
	_ = tmp.Sync()
	return snapshotVideoFromPath(ctx, tmpName)
}

func snapshotVideo(ctx context.Context, link *model.Link, rawPath string, args model.LinkArgs, size int64, isBaidu bool) (*bytes.Buffer, error) {
	if link != nil && link.MFile != nil {
		defer func() {
			_ = link.MFile.Close()
		}()
	}
	if isBaidu {
		if buf, err := snapshotVideoViaSegmentedTempFile(ctx, link, size, baiduThumbReadCap); err == nil {
			return buf, nil
		}
	}
	sources := buildVideoSources(link, rawPath, args)
	if len(sources) == 0 {
		return nil, errors.New("no video source available")
	}
	var lastErr error
	for _, src := range sources {
		var buf *bytes.Buffer
		var err error
		switch src.kind {
		case videoSourceFile:
			buf, err = snapshotVideoFromPath(ctx, src.path)
		case videoSourceURL:
			buf, err = snapshotVideoFromURL(ctx, src.url, src.header)
		case videoSourceReader:
			var rc io.ReadCloser
			rc, err = src.readerFactory(ctx)
			if err == nil {
				buf, err = snapshotVideoFromReader(ctx, rc)
				_ = rc.Close()
			}
		default:
			err = errors.New("unknown video source")
		}
		if src.cleanup != nil {
			src.cleanup()
		}
		if err == nil {
			return buf, nil
		}
		lastErr = err
	}
	if buf, err := snapshotVideoViaTempFile(ctx, link, size); err == nil {
		return buf, nil
	} else if lastErr == nil {
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("video thumbnail generation failed")
	}
	return nil, lastErr
}

func isBaiduStorage(storage driver.Driver) bool {
	return isBaiduStorageWithSeen(storage, map[uint]bool{})
}

func isBaiduStorageWithSeen(storage driver.Driver, seen map[uint]bool) bool {
	if storage == nil || storage.GetStorage() == nil {
		return false
	}
	if ider, ok := storage.(driver.BackendIdentifier); ok && strings.HasPrefix(ider.BackendID(), "baidu:") {
		return true
	}
	st := storage.GetStorage()
	if st.Driver == "BaiduNetdisk" {
		return true
	}
	if st.Driver != "Crypt" {
		return false
	}
	if seen[st.ID] || st.Addition == "" {
		return false
	}
	seen[st.ID] = true
	var add cryptAddition
	if err := json.Unmarshal([]byte(st.Addition), &add); err != nil || add.RemotePath == "" {
		return false
	}
	remoteStorage, _, err := op.GetStorageAndActualPath(add.RemotePath)
	if err != nil {
		return false
	}
	return isBaiduStorageWithSeen(remoteStorage, seen)
}

func buildThumbFile(ctx context.Context, storage driver.Driver, obj model.Obj, rawPath string, args model.LinkArgs, dst string) error {
	link, _, err := fs.Link(ctx, rawPath, args)
	if err != nil {
		return err
	}

	fileType := utils.GetFileType(obj.GetName())
	var img image.Image
	switch fileType {
	case conf.IMAGE:
		rc, err := openLinkReader(ctx, link)
		if err != nil {
			return err
		}
		defer rc.Close()
		img, err = decodeImage(rc)
		if err != nil {
			return err
		}
	case conf.VIDEO:
		buf, err := snapshotVideo(ctx, link, rawPath, args, obj.GetSize(), isBaiduStorage(storage))
		if err != nil {
			return err
		}
		img, err = decodeImage(buf)
		if err != nil {
			return err
		}
	default:
		return errors.New("unsupported type")
	}
	img = resizeImage(img)

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := encodeJPEG(out, img); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func Generate(ctx context.Context, storage driver.Driver, obj model.Obj, rawPath string, args model.LinkArgs) (string, error) {
	if !IsThumbCandidate(obj) {
		return "", errors.New("not thumbnail candidate")
	}
	initThumb()
	key := cacheKey(storage, obj, rawPath)
	path := cachePath(key)
	if cacheFresh(path, DefaultCacheTTL) {
		return path, nil
	}
	v, err, _ := thumbGroup.Do(key, func() (interface{}, error) {
		if cacheFresh(path, DefaultCacheTTL) {
			return path, nil
		}
		sem := thumbSem
		if isBaiduStorage(storage) {
			sem = baiduThumbSem
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		defer func() { <-sem }()
		if err := buildThumbFile(ctx, storage, obj, rawPath, args, path); err != nil {
			return "", err
		}
		return path, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}
