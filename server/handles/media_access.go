package handles

import (
	"net/http"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

type videoAccessContext struct {
	user    *model.User
	reqPath string
	storage *model.Storage
}

func resolveVideoAccess(c *gin.Context, rawPath string) (*videoAccessContext, bool) {
	user := c.MustGet("user").(*model.User)
	reqPath, err := user.JoinPath(rawPath)
	if err != nil {
		common.ErrorResp(c, err, http.StatusForbidden)
		return nil, false
	}
	meta, err := op.GetNearestMeta(reqPath)
	if err != nil && !errors.Is(errors.Cause(err), errs.MetaNotFound) {
		common.ErrorResp(c, err, http.StatusInternalServerError)
		return nil, false
	}
	if !common.CanAccess(user, meta, reqPath, "") {
		common.ErrorStrResp(c, "permission denied", http.StatusForbidden)
		return nil, false
	}
	storage, _, err := op.GetStorageAndActualPath(reqPath)
	if err != nil {
		common.ErrorResp(c, err, http.StatusNotFound)
		return nil, false
	}
	return &videoAccessContext{
		user:    user,
		reqPath: reqPath,
		storage: storage.GetStorage(),
	}, true
}
