package handles

import (
	"fmt"
	"math"
	stdpath "path"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/internal/sign"
	"github.com/alist-org/alist/v3/internal/thumb"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type ListReq struct {
	model.PageReq
	Path     string `json:"path" form:"path"`
	Password string `json:"password" form:"password"`
	Refresh  bool   `json:"refresh"`
}

type DirReq struct {
	Path      string `json:"path" form:"path"`
	Password  string `json:"password" form:"password"`
	ForceRoot bool   `json:"force_root" form:"force_root"`
}

type ObjResp struct {
	Name        string                     `json:"name"`
	Size        int64                      `json:"size"`
	IsDir       bool                       `json:"is_dir"`
	Modified    time.Time                  `json:"modified"`
	Created     time.Time                  `json:"created"`
	Sign        string                     `json:"sign"`
	Thumb       string                     `json:"thumb"`
	Type        int                        `json:"type"`
	HashInfoStr string                     `json:"hashinfo"`
	HashInfo    map[*utils.HashType]string `json:"hash_info"`
	Progress    float64                    `json:"progress,omitempty"`
	Watched     bool                       `json:"watched,omitempty"`
	LastPlayed  time.Time                  `json:"last_played,omitempty"`
}

type FsListResp struct {
	Content  []ObjResp `json:"content"`
	Total    int64     `json:"total"`
	Readme   string    `json:"readme"`
	Header   string    `json:"header"`
	Write    bool      `json:"write"`
	Provider string    `json:"provider"`
}

func FsList(c *gin.Context) {
	var req ListReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	req.Validate()
	user := c.MustGet("user").(*model.User)
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			common.ErrorResp(c, err, 500, true)
			return
		}
	}
	c.Set("meta", meta)
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}
	if !user.CanWrite() && !common.CanWrite(meta, reqPath) && req.Refresh {
		common.ErrorStrResp(c, "Refresh without permission", 403)
		return
	}
	objs, err := fs.List(c, reqPath, &fs.ListArgs{Refresh: req.Refresh})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	total, objs := pagination(objs, &req.PageReq)
	provider := "unknown"
	storage, err := fs.GetStorage(reqPath, &fs.GetStoragesArgs{})
	if err == nil {
		provider = storage.GetStorage().Driver
	}
	common.SuccessResp(c, FsListResp{
		Content:  toObjsResp(objs, c.Request.URL.Path, reqPath, isEncrypt(meta, reqPath), storage),
		Total:    int64(total),
		Readme:   getReadme(meta, reqPath),
		Header:   getHeader(meta, reqPath),
		Write:    user.CanWrite() || common.CanWrite(meta, reqPath),
		Provider: provider,
	})
}

func FsDirs(c *gin.Context) {
	var req DirReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	reqPath := req.Path
	if req.ForceRoot {
		if !user.IsAdmin() {
			common.ErrorStrResp(c, "Permission denied", 403)
			return
		}
	} else {
		tmp, err := user.JoinPath(req.Path)
		if err != nil {
			common.ErrorResp(c, err, 403)
			return
		}
		reqPath = tmp
	}
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			common.ErrorResp(c, err, 500, true)
			return
		}
	}
	c.Set("meta", meta)
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}
	objs, err := fs.List(c, reqPath, &fs.ListArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	dirs := filterDirs(objs)
	common.SuccessResp(c, dirs)
}

type DirResp struct {
	Name     string    `json:"name"`
	Modified time.Time `json:"modified"`
}

func filterDirs(objs []model.Obj) []DirResp {
	var dirs []DirResp
	for _, obj := range objs {
		if obj.IsDir() {
			dirs = append(dirs, DirResp{
				Name:     obj.GetName(),
				Modified: obj.ModTime(),
			})
		}
	}
	return dirs
}

func getReadme(meta *model.Meta, path string) string {
	if meta != nil && (utils.PathEqual(meta.Path, path) || meta.RSub) {
		return meta.Readme
	}
	return ""
}

func getHeader(meta *model.Meta, path string) string {
	if meta != nil && (utils.PathEqual(meta.Path, path) || meta.HeaderSub) {
		return meta.Header
	}
	return ""
}

func isEncrypt(meta *model.Meta, path string) bool {
	if common.IsStorageSignEnabled(path) {
		return true
	}
	if meta == nil || meta.Password == "" {
		return false
	}
	if !utils.PathEqual(meta.Path, path) && !meta.PSub {
		return false
	}
	return true
}

func pagination(objs []model.Obj, req *model.PageReq) (int, []model.Obj) {
	pageIndex, pageSize := req.Page, req.PerPage
	total := len(objs)
	start := (pageIndex - 1) * pageSize
	if start > total {
		return total, []model.Obj{}
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return total, objs[start:end]
}

func detectAPIPrefix(reqPath string) string {
	if reqPath == "" {
		return ""
	}
	idx := strings.LastIndex(reqPath, "/fs/")
	if idx <= 0 {
		return ""
	}
	prefix := strings.TrimSuffix(reqPath[:idx], "/")
	if prefix == "" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return prefix
}

func shouldDisableVideoThumb(storage driver.Driver, obj model.Obj) bool {
	if obj == nil || obj.IsDir() {
		return false
	}
	return utils.GetFileType(obj.GetName()) == conf.VIDEO && isBaiduStorage(storage)
}

func buildThumbURL(apiReqPath string, parent string, obj model.Obj, encrypt bool, storage driver.Driver) string {
	if obj == nil || obj.IsDir() {
		return ""
	}
	if shouldDisableVideoThumb(storage, obj) {
		return ""
	}
	objPath := stdpath.Join(parent, obj.GetName())
	query := "type=thumb"
	if signStr := common.Sign(obj, parent, encrypt); signStr != "" {
		query += "&sign=" + signStr
	}
	return fmt.Sprintf("%s/d%s?%s", detectAPIPrefix(apiReqPath), utils.EncodePath(objPath, true), query)
}

func toObjsResp(objs []model.Obj, apiReqPath string, parent string, encrypt bool, storage driver.Driver) []ObjResp {
	progressMap := map[string]op.VideoProgressInfo{}
	if storage != nil && storage.GetStorage() != nil {
		var paths []string
		for _, obj := range objs {
			if obj == nil || obj.IsDir() {
				continue
			}
			if utils.GetFileType(obj.GetName()) != conf.VIDEO {
				continue
			}
			objPath := stdpath.Join(parent, obj.GetName())
			paths = append(paths, objPath)
		}
		if len(paths) > 0 {
			if m, err := op.GetVideoProgressMap(storage.GetStorage().ID, paths); err == nil {
				progressMap = m
			}
		}
	}
	var resp []ObjResp
	for _, obj := range objs {
		thumbURL, _ := model.GetThumb(obj)
		objType := utils.GetObjType(obj.GetName(), obj.IsDir())
		if thumb.IsThumbCandidate(obj) {
			thumbURL = buildThumbURL(apiReqPath, parent, obj, encrypt, storage)
		}
		objPath := stdpath.Join(parent, obj.GetName())
		progress := 0.0
		watched := false
		lastPlayed := time.Time{}
		if info, ok := progressMap[objPath]; ok {
			if info.Duration > 0 {
				progress = math.Min(100, math.Max(0, (info.Position/info.Duration)*100))
			}
			watched = info.Finished
			lastPlayed = info.UpdatedAt
		}
		resp = append(resp, ObjResp{
			Name:        obj.GetName(),
			Size:        obj.GetSize(),
			IsDir:       obj.IsDir(),
			Modified:    obj.ModTime(),
			Created:     obj.CreateTime(),
			HashInfoStr: obj.GetHash().String(),
			HashInfo:    obj.GetHash().Export(),
			Sign:        common.Sign(obj, parent, encrypt),
			Thumb:       thumbURL,
			Type:        objType,
			Progress:    progress,
			Watched:     watched,
			LastPlayed:  lastPlayed,
		})
	}
	return resp
}

type FsGetReq struct {
	Path     string `json:"path" form:"path"`
	Password string `json:"password" form:"password"`
}

type FsGetResp struct {
	ObjResp
	RawURL   string    `json:"raw_url"`
	Readme   string    `json:"readme"`
	Header   string    `json:"header"`
	Provider string    `json:"provider"`
	Related  []ObjResp `json:"related"`
}

func FsGet(c *gin.Context) {
	var req FsGetReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			common.ErrorResp(c, err, 500)
			return
		}
	}
	c.Set("meta", meta)
	if !common.CanAccess(user, meta, reqPath, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}
	obj, err := fs.Get(c, reqPath, &fs.GetArgs{})
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	var rawURL string

	storage, err := fs.GetStorage(reqPath, &fs.GetStoragesArgs{})
	provider := "unknown"
	if err == nil {
		provider = storage.Config().Name
	}
	if !obj.IsDir() {
		if err != nil {
			common.ErrorResp(c, err, 500)
			return
		}
		isVideoObj := utils.GetFileType(obj.GetName()) == conf.VIDEO
		if isVideoObj {
			query := ""
			if isEncrypt(meta, reqPath) || setting.GetBool(conf.SignAll) {
				query = "?sign=" + sign.Sign(reqPath)
			}
			rawURL = fmt.Sprintf("%s/d%s%s",
				common.GetApiUrl(c.Request),
				utils.EncodePath(reqPath, true),
				query)
		} else if storage.Config().MustProxy() || storage.GetStorage().WebProxy {
			query := ""
			if isEncrypt(meta, reqPath) || setting.GetBool(conf.SignAll) {
				query = "?sign=" + sign.Sign(reqPath)
			}
			if storage.GetStorage().DownProxyUrl != "" {
				rawURL = fmt.Sprintf("%s%s?sign=%s",
					strings.Split(storage.GetStorage().DownProxyUrl, "\n")[0],
					utils.EncodePath(reqPath, true),
					sign.Sign(reqPath))
			} else {
				rawURL = fmt.Sprintf("%s/p%s%s",
					common.GetApiUrl(c.Request),
					utils.EncodePath(reqPath, true),
					query)
			}
		} else {
			// file have raw url
			if url, ok := model.GetUrl(obj); ok {
				rawURL = url
			} else {
				// if storage is not proxy, use raw url by fs.Link
				link, _, err := fs.Link(c, reqPath, model.LinkArgs{
					IP:      c.ClientIP(),
					Header:  c.Request.Header,
					HttpReq: c.Request,
				})
				if err != nil {
					common.ErrorResp(c, err, 500)
					return
				}
				if link.URL != "" {
					rawURL = link.URL
				} else {
					query := ""
					if isEncrypt(meta, reqPath) || setting.GetBool(conf.SignAll) {
						query = "?sign=" + sign.Sign(reqPath)
					}
					rawURL = fmt.Sprintf("%s/d%s%s",
						common.GetApiUrl(c.Request),
						utils.EncodePath(reqPath, true),
						query)
				}
			}
		}
	}
	var related []model.Obj
	parentPath := stdpath.Dir(reqPath)
	sameLevelFiles, err := fs.List(c, parentPath, &fs.ListArgs{})
	if err == nil {
		related = filterRelated(sameLevelFiles, obj)
	}
	parentMeta, _ := op.GetNearestMeta(parentPath)
	parentStorage, _ := fs.GetStorage(parentPath, &fs.GetStoragesArgs{})
	thumbURL, _ := model.GetThumb(obj)
	if thumb.IsThumbCandidate(obj) {
		thumbURL = buildThumbURL(c.Request.URL.Path, parentPath, obj, isEncrypt(meta, reqPath), parentStorage)
	}
	progress := 0.0
	watched := false
	lastPlayed := time.Time{}
	if storage != nil && storage.GetStorage() != nil && utils.GetFileType(obj.GetName()) == conf.VIDEO {
		if vp, err := op.GetVideoProgress(storage.GetStorage().ID, reqPath, obj.GetID()); err == nil && vp != nil {
			if vp.Duration > 0 {
				progress = math.Min(100, math.Max(0, (vp.Position/vp.Duration)*100))
			}
			watched = vp.Finished
			lastPlayed = vp.UpdatedAt
		}
	}
	common.SuccessResp(c, FsGetResp{
		ObjResp: ObjResp{
			Name:        obj.GetName(),
			Size:        obj.GetSize(),
			IsDir:       obj.IsDir(),
			Modified:    obj.ModTime(),
			Created:     obj.CreateTime(),
			HashInfoStr: obj.GetHash().String(),
			HashInfo:    obj.GetHash().Export(),
			Sign:        common.Sign(obj, parentPath, isEncrypt(meta, reqPath)),
			Type:        utils.GetFileType(obj.GetName()),
			Thumb:       thumbURL,
			Progress:    progress,
			Watched:     watched,
			LastPlayed:  lastPlayed,
		},
		RawURL:   rawURL,
		Readme:   getReadme(meta, reqPath),
		Header:   getHeader(meta, reqPath),
		Provider: provider,
		Related:  toObjsResp(related, c.Request.URL.Path, parentPath, isEncrypt(parentMeta, parentPath), parentStorage),
	})
}

func filterRelated(objs []model.Obj, obj model.Obj) []model.Obj {
	var related []model.Obj
	nameWithoutExt := strings.TrimSuffix(obj.GetName(), stdpath.Ext(obj.GetName()))
	for _, o := range objs {
		if o.GetName() == obj.GetName() {
			continue
		}
		if strings.HasPrefix(o.GetName(), nameWithoutExt) {
			related = append(related, o)
		}
	}
	return related
}

type FsOtherReq struct {
	model.FsOtherArgs
	Password string `json:"password" form:"password"`
}

func FsOther(c *gin.Context) {
	var req FsOtherReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	var err error
	req.Path, err = user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	meta, err := op.GetNearestMeta(req.Path)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			common.ErrorResp(c, err, 500)
			return
		}
	}
	c.Set("meta", meta)
	if !common.CanAccess(user, meta, req.Path, req.Password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return
	}
	res, err := fs.Other(c, req.FsOtherArgs)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, res)
}
