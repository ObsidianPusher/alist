package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
)

func TestRequestIDGeneratedWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/ping", func(c *gin.Context) {
		if common.GetRequestID(c) == "" {
			t.Fatalf("request id not set in context")
		}
		c.String(http.StatusOK, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	if got := w.Header().Get(common.HeaderRequestID); got == "" {
		t.Fatalf("missing %s header", common.HeaderRequestID)
	}
}

func TestRequestIDKeepsIncomingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestID())
	r.GET("/ping", func(c *gin.Context) {
		if got := common.GetRequestID(c); got != "test-request-id" {
			t.Fatalf("unexpected request id in context: %q", got)
		}
		c.String(http.StatusOK, "ok")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(common.HeaderRequestID, "test-request-id")
	r.ServeHTTP(w, req)
	if got := w.Header().Get(common.HeaderRequestID); got != "test-request-id" {
		t.Fatalf("header %s = %q, want test-request-id", common.HeaderRequestID, got)
	}
}
