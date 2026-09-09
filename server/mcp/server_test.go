package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alist-org/alist/v3/internal/conf"
)

func TestMCPRejectsMissingBearerToken(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	h := auth(conf.MCPConfig{TokenHash: hex.EncodeToString(sum[:])}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRuntimeCloseStopsMaintenance(t *testing.T) {
	stop := make(chan struct{})
	state := runtimeState{maintenanceStop: stop}
	state.close()
	select {
	case <-stop:
	default:
		t.Fatal("maintenance loop was not stopped")
	}
}
