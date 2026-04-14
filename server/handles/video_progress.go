package handles

import (
	"time"

	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
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
	access, ok := resolveVideoAccess(c, req.Path)
	if !ok {
		return
	}
	vp, err := op.GetVideoProgress(access.storage.ID, access.reqPath, "")
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
	access, ok := resolveVideoAccess(c, req.Path)
	if !ok {
		return
	}
	vp, err := op.UpsertVideoProgress(access.storage.ID, access.reqPath, req.FileID, req.Size, req.ModTime, req.Position, req.Duration)
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
