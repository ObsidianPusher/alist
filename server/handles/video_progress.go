package handles

import (
	"net/http"
	"time"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type VideoProgressGetReq struct {
	Path string `form:"path"`
}

type VideoProgressPostReq struct {
	Path     string  `json:"path"`
	FileID   string  `json:"file_id"`
	Size     int64   `json:"size"`
	ModTime  int64   `json:"mod_time"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
}

type VideoProgressResp struct {
	Path      string    `json:"path"`
	FileID    string    `json:"file_id"`
	Position  float64   `json:"position"`
	Duration  float64   `json:"duration"`
	Finished  bool      `json:"finished"`
	UpdatedAt time.Time `json:"updated_at"`
}

func GetVideoProgress(c *gin.Context) {
	var req VideoProgressGetReq
	if err := c.ShouldBind(&req); err != nil || req.Path == "" {
		common.ErrorStrResp(c, "path required", 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500)
		return
	}
	if !common.CanAccess(user, meta, reqPath, "") {
		common.ErrorStrResp(c, "permission denied", http.StatusForbidden)
		return
	}
	storage, _, err := op.GetStorageAndActualPath(reqPath)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return
	}
	vp, err := op.GetVideoProgress(storage.GetStorage().ID, reqPath, "")
	if err != nil || vp == nil {
		common.SuccessResp(c, nil)
		return
	}
	common.SuccessResp(c, VideoProgressResp{
		Path:      vp.Path,
		FileID:    vp.FileID,
		Position:  vp.Position,
		Duration:  vp.Duration,
		Finished:  vp.Finished,
		UpdatedAt: vp.UpdatedAt,
	})
}

func SaveVideoProgress(c *gin.Context) {
	var req VideoProgressPostReq
	if err := c.ShouldBindJSON(&req); err != nil || req.Path == "" {
		common.ErrorStrResp(c, "invalid payload", 400)
		return
	}
	user := c.MustGet("user").(*model.User)
	reqPath, err := user.JoinPath(req.Path)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return
	}
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, 500)
		return
	}
	if !common.CanAccess(user, meta, reqPath, "") {
		common.ErrorStrResp(c, "permission denied", http.StatusForbidden)
		return
	}
	storage, _, err := op.GetStorageAndActualPath(reqPath)
	if err != nil {
		common.ErrorResp(c, err, 404)
		return
	}
	vp, err := op.UpsertVideoProgress(storage.GetStorage().ID, reqPath, req.FileID, req.Size, req.ModTime, req.Position, req.Duration)
	if err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, VideoProgressResp{
		Path:      vp.Path,
		FileID:    vp.FileID,
		Position:  vp.Position,
		Duration:  vp.Duration,
		Finished:  vp.Finished,
		UpdatedAt: vp.UpdatedAt,
	})
}
