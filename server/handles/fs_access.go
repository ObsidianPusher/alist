package handles

import (
	stdpath "path"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

func bindRequest(c *gin.Context, req any) bool {
	if err := c.ShouldBind(req); err != nil {
		common.ErrorResp(c, err, 400)
		return false
	}
	return true
}

func currentUser(c *gin.Context) *model.User {
	return c.MustGet("user").(*model.User)
}

func joinUserPathOrAbort(c *gin.Context, user *model.User, rawPath string) (string, bool) {
	path, err := user.JoinPath(rawPath)
	if err != nil {
		common.ErrorResp(c, err, 403)
		return "", false
	}
	return path, true
}

func nearestMetaOrAbort(c *gin.Context, path string, expose bool) (*model.Meta, bool) {
	meta, err := op.GetNearestMeta(path)
	if err != nil {
		if !errors.Is(errors.Cause(err), errs.MetaNotFound) {
			common.ErrorResp(c, err, 500, expose)
			return nil, false
		}
	}
	c.Set("meta", meta)
	return meta, true
}

func ensureCanAccessPath(c *gin.Context, user *model.User, path, password string) (*model.Meta, bool) {
	meta, ok := nearestMetaOrAbort(c, path, true)
	if !ok {
		return nil, false
	}
	if !common.CanAccess(user, meta, path, password) {
		common.ErrorStrResp(c, "password is incorrect or you have no permission", 403)
		return nil, false
	}
	return meta, true
}

func ensureCanWritePath(c *gin.Context, user *model.User, path string) bool {
	if user.CanWrite() {
		return true
	}
	meta, ok := nearestMetaOrAbort(c, stdpath.Dir(path), true)
	if !ok {
		return false
	}
	if !common.CanWrite(meta, path) {
		common.ErrorResp(c, errs.PermissionDenied, 403)
		return false
	}
	return true
}

func requireUserCapability(c *gin.Context, allowed bool) bool {
	if allowed {
		return true
	}
	common.ErrorResp(c, errs.PermissionDenied, 403)
	return false
}

func prepareScopedPath(c *gin.Context, user *model.User, rawPath string) (string, *model.Meta, bool) {
	path, ok := joinUserPathOrAbort(c, user, rawPath)
	if !ok {
		return "", nil, false
	}
	meta, ok := nearestMetaOrAbort(c, path, true)
	if !ok {
		return "", nil, false
	}
	return path, meta, true
}
