package crypt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/alist-org/alist/v3/pkg/utils"
)

type testCloser struct {
	closed atomic.Int32
}

func (t *testCloser) Close() error {
	t.closed.Add(1)
	return nil
}

func TestCryptRangeReadCloserDoesNotRetainReturnedReaders(t *testing.T) {
	base := &testCloser{}
	returned := &testCloser{}
	rc := newCryptRangeReadCloser(func(ctx context.Context, hr http_range.Range) (io.ReadCloser, error) {
		return utils.NewReadCloser(io.NopCloser(bytes.NewReader([]byte("abc"))), returned.Close), nil
	}, utils.NewClosers(base))

	reader, err := rc.RangeRead(context.Background(), http_range.Range{Start: 0, Length: 3})
	if err != nil {
		t.Fatalf("RangeRead failed: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader close failed: %v", err)
	}
	if got := returned.closed.Load(); got != 1 {
		t.Fatalf("returned reader close count=%d, want 1", got)
	}
	if got := base.closed.Load(); got != 0 {
		t.Fatalf("base closer should not be touched before Close, got=%d", got)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("range closer close failed: %v", err)
	}
	if got := base.closed.Load(); got != 1 {
		t.Fatalf("base close count=%d, want 1", got)
	}
	if got := returned.closed.Load(); got != 1 {
		t.Fatalf("returned reader should not be retained for second close, got=%d", got)
	}
}

func TestCryptHTTPRangeReadCloserReadsRequestedRange(t *testing.T) {
	conf.Conf = conf.DefaultConfig()
	source := bytes.Repeat([]byte("crypt-range-"), 200)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(source)))
			_, _ = w.Write(source)
			return
		}
		ranges, err := http_range.ParseRange(rangeHeader, int64(len(source)))
		if err != nil || len(ranges) == 0 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		ra := ranges[0]
		end := ra.Start + ra.Length
		if end > int64(len(source)) {
			end = int64(len(source))
		}
		part := source[ra.Start:end]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", ra.Start, end-1, len(source)))
		w.Header().Set("Content-Length", strconv.Itoa(len(part)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(part)
	}))
	defer server.Close()

	rrc, err := newCryptHTTPRangeReadCloser(int64(len(source)), &model.Link{URL: server.URL})
	if err != nil {
		t.Fatalf("newCryptHTTPRangeReadCloser failed: %v", err)
	}
	rc, err := rrc.RangeRead(context.Background(), http_range.Range{Start: 5, Length: 32})
	if err != nil {
		t.Fatalf("RangeRead failed: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !bytes.Equal(data, source[5:37]) {
		t.Fatalf("unexpected range data")
	}
}
