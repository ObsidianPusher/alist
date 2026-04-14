package common

import (
	"strings"

	"github.com/alist-org/alist/v3/cmd/flags"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

const defaultSuccessMessage = "success"

func hidePrivacy(msg string) string {
	for _, r := range conf.PrivacyReg {
		msg = r.ReplaceAllStringFunc(msg, func(s string) string {
			return strings.Repeat("*", len(s))
		})
	}
	return msg
}

func normalizeErrorMessage(err error) string {
	if err == nil {
		return "unknown error"
	}
	return hidePrivacy(err.Error())
}

func respondJSON(c *gin.Context, code int, message string, data interface{}) {
	c.JSON(200, NewResp(code, message, data, GetRequestID(c)))
}

// ErrorResp is used to return error response
// @param l: if true, log error
func ErrorResp(c *gin.Context, err error, code int, l ...bool) {
	ErrorWithDataResp(c, err, code, nil, l...)
	//if len(l) > 0 && l[0] {
	//	if flags.Debug || flags.Dev {
	//		log.Errorf("%+v", err)
	//	} else {
	//		log.Errorf("%v", err)
	//	}
	//}
	//c.JSON(200, Resp[interface{}]{
	//	Code:    code,
	//	Message: hidePrivacy(err.Error()),
	//	Data:    nil,
	//})
	//c.Abort()
}

func ErrorWithDataResp(c *gin.Context, err error, code int, data interface{}, l ...bool) {
	requestID := GetRequestID(c)
	if len(l) > 0 && l[0] {
		if flags.Debug || flags.Dev {
			log.WithField("request_id", requestID).Errorf("%+v", err)
		} else {
			log.WithField("request_id", requestID).Errorf("%v", err)
		}
	}
	respondJSON(c, code, normalizeErrorMessage(err), data)
	c.Abort()
}

func ErrorStrResp(c *gin.Context, str string, code int, l ...bool) {
	requestID := GetRequestID(c)
	if len(l) != 0 && l[0] {
		log.WithField("request_id", requestID).Error(str)
	}
	respondJSON(c, code, hidePrivacy(str), nil)
	c.Abort()
}

func SuccessResp(c *gin.Context, data ...interface{}) {
	if len(data) == 0 {
		respondJSON(c, ResponseCodeSuccess, defaultSuccessMessage, nil)
		return
	}
	respondJSON(c, ResponseCodeSuccess, defaultSuccessMessage, data[0])
}
