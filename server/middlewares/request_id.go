package middlewares

import (
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(common.HeaderRequestID)
		if requestID == "" {
			requestID = uuid.NewString()
		}
		c.Set(common.CtxKeyRequestID, requestID)
		c.Header(common.HeaderRequestID, requestID)
		c.Next()
	}
}
