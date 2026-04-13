package common

import "github.com/gin-gonic/gin"

const (
	CtxKeyRequestID = "request_id"
	HeaderRequestID = "X-Request-ID"
)

func GetRequestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Get(CtxKeyRequestID); ok {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}
