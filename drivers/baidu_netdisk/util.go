package baidu_netdisk

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Xhofe/go-cache"
	"github.com/alist-org/alist/v3/drivers/base"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/avast/retry-go"
	"github.com/go-resty/resty/v2"
	log "github.com/sirupsen/logrus"
)

// do others that not defined in Driver interface

func (d *BaiduNetdisk) refreshToken(ctx context.Context) error {
	err := d._refreshToken(ctx)
	if err != nil && errors.Is(err, errs.EmptyToken) {
		err = d._refreshToken(ctx)
	}
	return err
}

func (d *BaiduNetdisk) _refreshToken(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	u := "https://openapi.baidu.com/oauth/2.0/token"
	var resp base.TokenResp
	var e TokenErrResp
	_, err := base.RestyClient.R().SetContext(ctx).SetResult(&resp).SetError(&e).SetQueryParams(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": d.RefreshToken,
		"client_id":     d.ClientID,
		"client_secret": d.ClientSecret,
	}).Get(u)
	if err != nil {
		return err
	}
	if e.Error != "" {
		return fmt.Errorf("%s : %s", e.Error, e.ErrorDescription)
	}
	if resp.RefreshToken == "" {
		return errs.EmptyToken
	}
	d.AccessToken, d.RefreshToken = resp.AccessToken, resp.RefreshToken
	op.MustSaveDriverStorage(d)
	return nil
}

func (d *BaiduNetdisk) exchangeAuthCode(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	code := strings.TrimSpace(d.AuthCode)
	if code == "" {
		return errs.EmptyToken
	}
	redirectURI := strings.TrimSpace(d.RedirectURI)
	if redirectURI == "" {
		redirectURI = "oob"
	}
	u := "https://openapi.baidu.com/oauth/2.0/token"
	var resp base.TokenResp
	var e TokenErrResp
	_, err := base.RestyClient.R().SetContext(ctx).SetResult(&resp).SetError(&e).SetQueryParams(map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     d.ClientID,
		"client_secret": d.ClientSecret,
		"redirect_uri":  redirectURI,
	}).Get(u)
	if err != nil {
		return err
	}
	if e.Error != "" {
		return fmt.Errorf("%s : %s", e.Error, e.ErrorDescription)
	}
	if resp.RefreshToken == "" || resp.AccessToken == "" {
		return errs.EmptyToken
	}
	d.AccessToken = resp.AccessToken
	d.RefreshToken = resp.RefreshToken
	d.AuthCode = ""
	op.MustSaveDriverStorage(d)
	return nil
}

func (d *BaiduNetdisk) request(ctx context.Context, furl string, method string, callback base.ReqCallback, resp interface{}) ([]byte, error) {
	var result []byte
	if ctx == nil {
		ctx = context.Background()
	}
	err := retry.Do(func() error {
		if err := ctx.Err(); err != nil {
			return retry.Unrecoverable(err)
		}
		req := base.RestyClient.R().SetContext(ctx)
		req.SetQueryParam("access_token", d.AccessToken)
		if callback != nil {
			callback(req)
		}
		if resp != nil {
			req.SetResult(resp)
		}
		res, err := req.Execute(method, furl)
		if err != nil {
			return err
		}
		log.Debugf("[baidu_netdisk] req: %s, resp: %s", furl, res.String())
		errno := utils.Json.Get(res.Body(), "errno").ToInt()
		if errno != 0 {
			if utils.SliceContains([]int{111, -6}, errno) {
				log.Info("refreshing baidu_netdisk token.")
				if err := ctx.Err(); err != nil {
					return retry.Unrecoverable(err)
				}
				err2 := d.refreshToken(ctx)
				if err2 != nil {
					return retry.Unrecoverable(err2)
				}
			}
			return fmt.Errorf("req: [%s] ,errno: %d, refer to https://pan.baidu.com/union/doc/", furl, errno)
		}
		result = res.Body()
		return nil
	},
		retry.LastErrorOnly(true),
		retry.Attempts(3),
		retry.Delay(time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.Context(ctx),
	)
	return result, err
}

func (d *BaiduNetdisk) get(ctx context.Context, pathname string, params map[string]string, resp interface{}) ([]byte, error) {
	return d.request(ctx, "https://pan.baidu.com/rest/2.0"+pathname, http.MethodGet, func(req *resty.Request) {
		req.SetQueryParams(params)
	}, resp)
}

func (d *BaiduNetdisk) postForm(ctx context.Context, pathname string, params map[string]string, form map[string]string, resp interface{}) ([]byte, error) {
	return d.request(ctx, "https://pan.baidu.com/rest/2.0"+pathname, http.MethodPost, func(req *resty.Request) {
		req.SetQueryParams(params)
		req.SetFormData(form)
	}, resp)
}

func (d *BaiduNetdisk) getFiles(ctx context.Context, dir string) ([]File, error) {
	start := 0
	limit := 200
	params := map[string]string{
		"method": "list",
		"dir":    dir,
		"web":    "web",
	}
	if d.OrderBy != "" {
		params["order"] = d.OrderBy
		if d.OrderDirection == "desc" {
			params["desc"] = "1"
		}
	}
	res := make([]File, 0)
	for {
		if utils.IsCanceled(ctx) {
			return nil, ctx.Err()
		}
		params["start"] = strconv.Itoa(start)
		params["limit"] = strconv.Itoa(limit)
		start += limit
		var resp ListResp
		_, err := d.get(ctx, "/xpan/file", params, &resp)
		if err != nil {
			return nil, err
		}
		if len(resp.List) == 0 {
			break
		}
		res = append(res, resp.List...)
	}
	return res, nil
}

func (d *BaiduNetdisk) linkOfficial(ctx context.Context, file model.Obj, args model.LinkArgs) (*model.Link, error) {
	cacheKey := d.linkURLCacheKey(file)
	if url, ok := d.getCachedLinkURL(cacheKey); ok {
		return &model.Link{
			URL: url,
			Header: http.Header{
				"User-Agent": []string{"pan.baidu.com"},
				"Referer":    []string{"https://pan.baidu.com/"},
			},
		}, nil
	}
	u, err := d.getOfficialDlink(ctx, file)
	if err != nil {
		return nil, err
	}

	return &model.Link{
		URL: u,
		Header: http.Header{
			"User-Agent": []string{"pan.baidu.com"},
			"Referer":    []string{"https://pan.baidu.com/"},
		},
	}, nil
}

func (d *BaiduNetdisk) getOfficialDlink(ctx context.Context, file model.Obj) (string, error) {
	params := map[string]string{
		"method": "filemetas",
		"fsids":  fmt.Sprintf("[%s]", file.GetID()),
		"dlink":  "1",
	}
	var resp DownloadResp
	_, err := d.get(ctx, "/xpan/multimedia", params, &resp)
	if err != nil {
		return "", err
	}
	if len(resp.List) == 0 || resp.List[0].Dlink == "" {
		return "", errors.New("empty baidu dlink")
	}
	url := fmt.Sprintf("%s&access_token=%s", resp.List[0].Dlink, d.AccessToken)
	cacheKey := d.linkURLCacheKey(file)
	d.setCachedLinkURL(cacheKey, url)
	return url, nil
}

func (d *BaiduNetdisk) getCachedLinkURL(key string) (string, bool) {
	if key == "" || d.dlinkURLCache == nil {
		return "", false
	}
	url, ok := d.dlinkURLCache.Get(key)
	if !ok || url == "" {
		return "", false
	}
	d.dlinkURLCache.Set(key, url, cache.WithEx[string](defaultBaiduDlinkCacheTTL))
	return url, true
}

func (d *BaiduNetdisk) setCachedLinkURL(key, url string) {
	if key == "" || url == "" || d.dlinkURLCache == nil {
		return
	}
	d.dlinkURLCache.Set(key, url, cache.WithEx[string](defaultBaiduDlinkCacheTTL))
}

func (d *BaiduNetdisk) linkURLCacheKey(file model.Obj) string {
	if file == nil {
		return ""
	}
	backendID := d.BackendID()
	if backendID == "" {
		backendID = "baidu"
	}
	return fmt.Sprintf(
		"%s:%s:%s:%d:%d",
		backendID,
		d.AccessToken,
		file.GetID(),
		file.GetSize(),
		file.ModTime().Unix(),
	)
}

func (d *BaiduNetdisk) applyDownloadPolicy(link *model.Link, file model.Obj) {
	if link == nil || link.MFile != nil || link.URL == "" {
		return
	}
	runtimeCfg := d.refreshDownloadRuntimePolicy()
	cache := d.ensureProxyCache(runtimeCfg.proxyCacheBytes)
	cacheFileKey := buildBaiduProxyFileCacheKey(d.BackendID(), file)
	link.Concurrency = runtimeCfg.partConcurrency
	link.PartSize = runtimeCfg.partSize
	var limiter *tokenLimiter
	if d.resolveRequestRole(link) != requestRoleInteractive {
		limiter = d.downloadLimiter
	}
	if link.RangeReadCloser == nil {
		link.RangeReadCloser = newBaiduRangeReadCloser(
			link,
			file.GetSize(),
			limiter,
			runtimeCfg.partConcurrency,
			runtimeCfg.partSize,
			cache,
			cacheFileKey,
			d.BackendID(),
			file.GetPath(),
			d.resolveRequestRole(link),
		)
	}
}

func (d *BaiduNetdisk) manage(ctx context.Context, opera string, filelist any) ([]byte, error) {
	params := map[string]string{
		"method": "filemanager",
		"opera":  opera,
	}
	marshal, _ := utils.Json.MarshalToString(filelist)
	return d.postForm(ctx, "/xpan/file", params, map[string]string{
		"async":    "0",
		"filelist": marshal,
		"ondup":    "fail",
	}, nil)
}

func (d *BaiduNetdisk) create(ctx context.Context, path string, size int64, isdir int, uploadid, block_list string, resp any, mtime, ctime int64) ([]byte, error) {
	params := map[string]string{
		"method": "create",
	}
	form := map[string]string{
		"path":  path,
		"size":  strconv.FormatInt(size, 10),
		"isdir": strconv.Itoa(isdir),
		"rtype": "3",
	}
	if mtime != 0 && ctime != 0 {
		joinTime(form, ctime, mtime)
	}

	if uploadid != "" {
		form["uploadid"] = uploadid
	}
	if block_list != "" {
		form["block_list"] = block_list
	}
	return d.postForm(ctx, "/xpan/file", params, form, resp)
}

func joinTime(form map[string]string, ctime, mtime int64) {
	form["local_mtime"] = strconv.FormatInt(mtime, 10)
	form["local_ctime"] = strconv.FormatInt(ctime, 10)
}

const (
	DefaultSliceSize   int64 = 4 * utils.MB
	VipSliceSize             = 16 * utils.MB
	SVipSliceSize            = 32 * utils.MB
	MaxUploadSliceSize       = 100 * utils.MB
)

func (d *BaiduNetdisk) getSliceSize() int64 {
	size := d.CustomUploadPartSize
	if size <= 0 {
		switch d.vipType {
		case 2:
			size = int64(SVipSliceSize)
		case 1:
			size = int64(VipSliceSize)
		default:
			size = int64(DefaultSliceSize)
		}
	}
	if size < int64(DefaultSliceSize) {
		size = int64(DefaultSliceSize)
	}
	if size > MaxUploadSliceSize {
		size = MaxUploadSliceSize
	}
	return size
}

// func encodeURIComponent(str string) string {
// 	r := url.QueryEscape(str)
// 	r = strings.ReplaceAll(r, "+", "%20")
// 	return r
// }

func DecryptMd5(encryptMd5 string) string {
	if _, err := hex.DecodeString(encryptMd5); err == nil {
		return encryptMd5
	}

	var out strings.Builder
	out.Grow(len(encryptMd5))
	for i, n := 0, int64(0); i < len(encryptMd5); i++ {
		if i == 9 {
			n = int64(unicode.ToLower(rune(encryptMd5[i])) - 'g')
		} else {
			n, _ = strconv.ParseInt(encryptMd5[i:i+1], 16, 64)
		}
		out.WriteString(strconv.FormatInt(n^int64(15&i), 16))
	}

	encryptMd5 = out.String()
	return encryptMd5[8:16] + encryptMd5[:8] + encryptMd5[24:32] + encryptMd5[16:24]
}

func EncryptMd5(originalMd5 string) string {
	reversed := originalMd5[8:16] + originalMd5[:8] + originalMd5[24:32] + originalMd5[16:24]

	var out strings.Builder
	out.Grow(len(reversed))
	for i, n := 0, int64(0); i < len(reversed); i++ {
		n, _ = strconv.ParseInt(reversed[i:i+1], 16, 64)
		n ^= int64(15 & i)
		if i == 9 {
			out.WriteRune(rune(n) + 'g')
		} else {
			out.WriteString(strconv.FormatInt(n, 16))
		}
	}
	return out.String()
}
