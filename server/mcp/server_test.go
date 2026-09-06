package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/gin-gonic/gin"
)

func TestMCPRejectsMissingBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sum := sha256.Sum256([]byte("secret"))
	r := gin.New()
	if err := Register(r.Group("/mcp"), conf.MCPConfig{
		Enabled: true, DocsRoot: "/docs", TokenHash: hex.EncodeToString(sum[:]), IndexDir: t.TempDir(),
		MaxReadBytes: 1024, MaxWriteBytes: 1024, HistoryLimit: 1,
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
