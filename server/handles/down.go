package handles

import (
	"fmt"
	"io"
	"net/http"
	stdpath "path"
	"strings"

	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	inet "github.com/alist-org/alist/v3/internal/net"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/internal/sign"
	"github.com/alist-org/alist/v3/internal/thumb"
	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

func Down(c *gin.Context) {
	rawPath := c.MustGet("path").(string)
	filename := stdpath.Base(rawPath)
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if c.Query("type") == "thumb" {
		if served := serveThumb(c, rawPath, storage); served {
			return
		}
		thumbError(c, rawPath, "thumbnail not available", 404, nil)
		return
	}
	markVideoWatchFallback(c, rawPath, filename, storage)
	if isBaiduStorage(storage) {
		Proxy(c)
		return
	}
	if common.ShouldProxy(storage, filename) {
		Proxy(c)
		return
	} else {
		link, file, err := fs.Link(c, rawPath, model.LinkArgs{
			IP:      c.ClientIP(),
			Header:  c.Request.Header,
			Type:    c.Query("type"),
			HttpReq: c.Request,
		})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		if link.MFile != nil || link.URL == "" {
			if file == nil {
				file, _ = fs.Get(c, rawPath, &fs.GetArgs{NoLog: true})
			}
			if file == nil {
				common.ErrorStrResp(c, "file not found", 404)
				return
			}
			if err := common.Proxy(c.Writer, c.Request, link, file); err != nil {
				common.ErrorResp(c, err, 500, true)
			}
			return
		}
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Cache-Control", "max-age=0, no-cache, no-store, must-revalidate")
		if setting.GetBool(conf.ForwardDirectLinkParams) {
			query := c.Request.URL.Query()
			for _, v := range conf.SlicesMap[conf.IgnoreDirectLinkParams] {
				query.Del(v)
			}
			link.URL, err = utils.InjectQuery(link.URL, query)
			if err != nil {
				common.ErrorResp(c, err, 500)
				return
			}
		}
		c.Redirect(302, link.URL)
	}
}

func serveThumb(c *gin.Context, rawPath string, storage driver.Driver) bool {
	traceID := uuid.NewString()
	c.Header("X-Alist-Trace-Id", traceID)
	driverName := "unknown"
	if storage != nil && storage.GetStorage() != nil {
		driverName = storage.GetStorage().Driver
	}
	log.WithFields(log.Fields{
		"trace_id": traceID,
		"path":     rawPath,
		"driver":   driverName,
	}).Info("thumb request start")
	obj, err := fs.Get(c, rawPath, &fs.GetArgs{})
	if err != nil {
		thumbErrorWithTrace(c, rawPath, "failed to get object for thumbnail", 404, err, traceID, driverName)
		return true
	}
	if !thumb.IsThumbCandidate(obj) {
		log.WithFields(log.Fields{
			"trace_id": traceID,
			"path":     rawPath,
			"driver":   driverName,
			"name":     obj.GetName(),
		}).Warn("thumb request skipped: not thumbnail candidate")
		return false
	}
	if isBaiduStorage(storage) && utils.GetFileType(obj.GetName()) == conf.VIDEO {
		log.WithFields(log.Fields{
			"trace_id": traceID,
			"path":     rawPath,
			"driver":   driverName,
			"name":     obj.GetName(),
		}).Info("thumb request skipped: video thumbnail disabled for baidu/crypt-baidu")
		return false
	}
	args := model.LinkArgs{
		IP:      c.ClientIP(),
		Header:  c.Request.Header,
		HttpReq: c.Request,
	}
	if storage != nil && storage.GetStorage() != nil && storage.GetStorage().Driver == "Local" {
		if serveLocalThumb(c, rawPath) {
			log.WithFields(log.Fields{
				"trace_id": traceID,
				"path":     rawPath,
				"driver":   driverName,
			}).Info("thumb served by local provider")
			return true
		}
	}
	if !isBaiduStorage(storage) {
		urls, err := collectThumbURLs(c, rawPath, storage, obj, args)
		if err != nil {
			thumbErrorWithTrace(c, rawPath, "failed to collect thumbnail urls", 500, err, traceID, driverName)
			return true
		}
		log.WithFields(log.Fields{
			"trace_id": traceID,
			"path":     rawPath,
			"driver":   driverName,
			"url_cnt":  len(urls),
		}).Info("thumb collected remote urls")
		for _, url := range urls {
			if serveThumbFromURL(c, url, traceID) {
				log.WithFields(log.Fields{
					"trace_id": traceID,
					"path":     rawPath,
					"driver":   driverName,
				}).Info("thumb served by remote url")
				return true
			}
		}
	}
	log.WithFields(log.Fields{
		"trace_id": traceID,
		"path":     rawPath,
		"driver":   driverName,
		"name":     obj.GetName(),
		"size":     obj.GetSize(),
		"type":     utils.GetFileType(obj.GetName()),
	}).Info("thumb generate start")
	path, err := thumb.Generate(c, storage, obj, rawPath, args)
	if err != nil {
		thumbErrorWithTrace(c, rawPath, "failed to generate thumbnail", 500, err, traceID, driverName)
		return true
	}
	log.WithFields(log.Fields{
		"trace_id":   traceID,
		"path":       rawPath,
		"driver":     driverName,
		"cache_path": path,
	}).Info("thumb generate success")
	c.Header("Content-Type", "image/jpeg")
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", int(thumb.CacheTTL().Seconds())))
	c.File(path)
	return true
}

func collectThumbURLs(c *gin.Context, rawPath string, storage driver.Driver, obj model.Obj, args model.LinkArgs) ([]string, error) {
	seen := map[string]struct{}{}
	urls := make([]string, 0, 4)
	add := func(url string) {
		if url == "" || isSelfThumbURL(url) {
			return
		}
		if _, ok := seen[url]; ok {
			return
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	if direct, ok := model.GetThumb(obj); ok {
		add(direct)
	}
	if storage != nil {
		if tp, ok := storage.(driver.ThumbProvider); ok {
			url, err := tp.Thumb(c, obj, args)
			if err != nil {
				return nil, err
			}
			add(url)
		}
		if isBaiduStorage(storage) {
			add(findThumbFromList(c, rawPath, obj))
		}
	}
	return urls, nil
}

func isSelfThumbURL(url string) bool {
	return strings.Contains(url, "type=thumb")
}

func serveLocalThumb(c *gin.Context, rawPath string) bool {
	link, _, err := fs.Link(c, rawPath, model.LinkArgs{
		IP:      c.ClientIP(),
		Header:  c.Request.Header,
		Type:    "thumb",
		HttpReq: c.Request,
	})
	if err != nil || link == nil {
		return false
	}
	if link.URL != "" {
		return serveThumbFromURL(c, link.URL, "")
	}
	contentType := link.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/png"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", int(thumb.CacheTTL().Seconds())))
	if link.MFile != nil {
		defer link.MFile.Close()
		_, err = io.Copy(c.Writer, link.MFile)
		return err == nil
	}
	if link.RangeReadCloser != nil {
		rc, err := link.RangeReadCloser.RangeRead(c, http_range.Range{Start: 0, Length: -1})
		if err != nil {
			return false
		}
		defer rc.Close()
		defer link.RangeReadCloser.Close()
		_, err = io.Copy(c.Writer, rc)
		return err == nil
	}
	return false
}

func findThumbFromList(c *gin.Context, rawPath string, obj model.Obj) string {
	parent := stdpath.Dir(rawPath)
	objs, err := fs.List(c, parent, &fs.ListArgs{})
	if err != nil {
		return ""
	}
	for _, o := range objs {
		if o.GetName() != obj.GetName() {
			continue
		}
		if t, ok := model.GetThumb(o); ok && t != "" {
			return t
		}
	}
	return ""
}

func serveThumbFromURL(c *gin.Context, url string, traceID string) bool {
	ua := base.UserAgent
	if strings.Contains(url, "pcs.baidu.com") || strings.Contains(url, "pan.baidu.com") {
		ua = "pan.baidu.com"
	}
	header := http.Header{
		"User-Agent": []string{ua},
	}
	if ua == "pan.baidu.com" {
		header.Set("Referer", "https://pan.baidu.com/")
	}
	resp, err := inet.RequestHttp(c, "GET", header, url)
	if err != nil {
		log.WithFields(log.Fields{
			"trace_id": traceID,
			"url":      url,
		}).Warnf("thumb url request failed: %v", err)
		return false
	}
	if resp.StatusCode >= 400 && strings.Contains(url, "baidu") && ua != "pan.baidu.com" {
		_ = resp.Body.Close()
		header.Set("User-Agent", "pan.baidu.com")
		resp, err = inet.RequestHttp(c, "GET", header, url)
		if err != nil {
			log.WithFields(log.Fields{
				"trace_id": traceID,
				"url":      url,
			}).Warnf("thumb url retry failed: %v", err)
			return false
		}
	}
	if resp.StatusCode >= 400 {
		log.WithFields(log.Fields{
			"trace_id": traceID,
			"url":      url,
			"status":   resp.StatusCode,
		}).Warn("thumb url response status not ok")
		_ = resp.Body.Close()
		return false
	}
	defer resp.Body.Close()
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", int(thumb.CacheTTL().Seconds())))
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.WithFields(log.Fields{
			"trace_id": traceID,
			"url":      url,
		}).Warnf("thumb stream to client failed: %v", err)
	}
	return err == nil
}

func thumbError(c *gin.Context, rawPath, reason string, code int, err error) {
	thumbErrorWithTrace(c, rawPath, reason, code, err, "", "")
}

func thumbErrorWithTrace(c *gin.Context, rawPath, reason string, code int, err error, traceID, driverName string) {
	if traceID == "" {
		traceID = uuid.NewString()
	}
	c.Header("X-Alist-Trace-Id", traceID)
	msg := reason
	if err != nil {
		msg = fmt.Sprintf("%s: %v", reason, err)
	}
	if driverName == "" {
		driverName = "unknown"
	}
	log.WithFields(log.Fields{
		"trace_id": traceID,
		"path":     rawPath,
		"reason":   reason,
		"driver":   driverName,
	}).Error(msg)
	common.ErrorWithDataResp(c, fmt.Errorf("%s [trace_id=%s]", msg, traceID), code, gin.H{
		"trace_id": traceID,
		"path":     rawPath,
		"reason":   reason,
		"driver":   driverName,
	})
}

func Proxy(c *gin.Context) {
	rawPath := c.MustGet("path").(string)
	filename := stdpath.Base(rawPath)
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	markVideoWatchFallback(c, rawPath, filename, storage)
	if canProxy(storage, filename) {
		downProxyUrl := storage.GetStorage().DownProxyUrl
		if downProxyUrl != "" {
			_, ok := c.GetQuery("d")
			if !ok {
				URL := fmt.Sprintf("%s%s?sign=%s",
					strings.Split(downProxyUrl, "\n")[0],
					utils.EncodePath(rawPath, true),
					sign.Sign(rawPath))
				c.Redirect(302, URL)
				return
			}
		}
		link, file, err := fs.Link(c, rawPath, model.LinkArgs{
			Header:  c.Request.Header,
			Type:    c.Query("type"),
			HttpReq: c.Request,
		})
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		if link.URL != "" && setting.GetBool(conf.ForwardDirectLinkParams) {
			query := c.Request.URL.Query()
			for _, v := range conf.SlicesMap[conf.IgnoreDirectLinkParams] {
				query.Del(v)
			}
			link.URL, err = utils.InjectQuery(link.URL, query)
			if err != nil {
				common.ErrorResp(c, err, 500)
				return
			}
		}
		if storage.GetStorage().ProxyRange {
			common.ProxyRange(link, file.GetSize())
		}
		err = common.Proxy(c.Writer, c.Request, link, file)
		if err != nil {
			common.ErrorResp(c, err, 500, true)
			return
		}
	} else {
		common.ErrorStrResp(c, "proxy not allowed", 403)
		return
	}
}

func markVideoWatchFallback(c *gin.Context, rawPath string, filename string, storage driver.Driver) {
	if storage == nil || storage.GetStorage() == nil {
		return
	}
	if utils.GetFileType(filename) != conf.VIDEO {
		return
	}
	if !shouldMarkPlaybackByRequest(c) {
		return
	}
	obj, err := fs.Get(c, rawPath, &fs.GetArgs{NoLog: true})
	if err != nil || obj == nil || obj.IsDir() {
		return
	}
	if _, err = op.MarkVideoWatched(
		storage.GetStorage().ID,
		rawPath,
		obj.GetID(),
		obj.GetSize(),
		obj.ModTime().UnixMilli(),
	); err != nil {
		log.WithFields(log.Fields{
			"path": rawPath,
		}).Warnf("failed to mark video watched fallback: %v", err)
	}
}

func shouldMarkPlaybackByRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	if c.Request.Method != http.MethodGet {
		return false
	}
	if c.Query("type") == "thumb" {
		return false
	}
	rangeHeader := strings.TrimSpace(c.GetHeader("Range"))
	if rangeHeader != "" {
		return strings.HasPrefix(strings.ToLower(rangeHeader), "bytes=0-")
	}
	dest := strings.ToLower(strings.TrimSpace(c.GetHeader("Sec-Fetch-Dest")))
	if dest == "video" {
		return true
	}
	accept := strings.ToLower(c.GetHeader("Accept"))
	return strings.Contains(accept, "video/")
}

// TODO need optimize
// when can be proxy?
// 1. text file
// 2. config.MustProxy()
// 3. storage.WebProxy
// 4. proxy_types
// solution: text_file + shouldProxy()
func canProxy(storage driver.Driver, filename string) bool {
	if isBaiduStorage(storage) {
		return true
	}
	if storage.Config().MustProxy() || storage.GetStorage().WebProxy || storage.GetStorage().WebdavProxy() {
		return true
	}
	// Allow proxy for Baidu/Crypt media to satisfy UA/Referer requirements.
	if storage.GetStorage().Driver == "BaiduNetdisk" || storage.GetStorage().Driver == "Crypt" {
		switch utils.GetFileType(filename) {
		case conf.VIDEO, conf.AUDIO, conf.IMAGE:
			return true
		}
	}
	if utils.SliceContains(conf.SlicesMap[conf.ProxyTypes], utils.Ext(filename)) {
		return true
	}
	if utils.SliceContains(conf.SlicesMap[conf.TextTypes], utils.Ext(filename)) {
		return true
	}
	return false
}
