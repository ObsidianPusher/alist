package net

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alist-org/alist/v3/pkg/http_range"
)

func TestServeHTTPUsesRequestContext(t *testing.T) {
	type contextKey string
	const key contextKey = "req"
	req := httptest.NewRequest("GET", "http://example.com/file", nil)
	req = req.WithContext(context.WithValue(req.Context(), key, "ctx-value"))
	rec := httptest.NewRecorder()

	ServeHTTP(rec, req, "file.bin", time.Time{}, 3, func(ctx context.Context, hr http_range.Range) (io.ReadCloser, error) {
		if got := ctx.Value(key); got != "ctx-value" {
			t.Fatalf("request context value mismatch: %v", got)
		}
		return io.NopCloser(bytes.NewReader([]byte("abc"))), nil
	})

	if rec.Code != 200 {
		t.Fatalf("unexpected status=%d", rec.Code)
	}
	if body := rec.Body.String(); body != "abc" {
		t.Fatalf("unexpected body=%q", body)
	}
}
