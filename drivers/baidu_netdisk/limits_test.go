package baidu_netdisk

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/alist-org/alist/v3/pkg/utils"
)

func TestBaiduRangeReadCloserSegmentCacheHit(t *testing.T) {
	const chunkSize = int64(utils.MB)
	source := bytes.Repeat([]byte("alist-proxy-cache-"), 200000) // ~3.4MB
	var upstreamCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		dataLen := int64(len(source))
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(dataLen, 10))
			_, _ = w.Write(source)
			return
		}
		ranges, err := http_range.ParseRange(rangeHeader, dataLen)
		if err != nil || len(ranges) == 0 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		ra := ranges[0]
		end := ra.Start + ra.Length
		if end > dataLen {
			end = dataLen
		}
		if ra.Start < 0 || ra.Start >= dataLen || end < ra.Start {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		part := source[int(ra.Start):int(end)]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", ra.Start, end-1, dataLen))
		w.Header().Set("Content-Length", strconv.Itoa(len(part)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(part)
	}))
	defer server.Close()

	cache := newBaiduProxyRangeCache(32*int64(utils.MB), time.Minute, false)
	defer cache.Close()
	conf.Conf = conf.DefaultConfig()
	br := &baiduRangeReadCloser{
		link:         &model.Link{URL: server.URL, Header: http.Header{}},
		size:         int64(len(source)),
		partSize:     int(chunkSize),
		cache:        cache,
		cacheFileKey: "test-file",
	}

	readRange := func(start, length int64) []byte {
		t.Helper()
		rc, err := br.RangeRead(context.Background(), http_range.Range{Start: start, Length: length})
		if err != nil {
			t.Fatalf("RangeRead failed: %v", err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read body failed: %v", err)
		}
		return data
	}

	// 第一次读取：队列模式下会回源一次。
	part1 := readRange(100, 200)
	if !bytes.Equal(part1, source[100:300]) {
		t.Fatalf("first range data mismatch")
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("expect 1 upstream call after first range, got=%d", got)
	}

	// 第二次读取：命中同一分段，不应再次回源。
	part2 := readRange(20, 80)
	if !bytes.Equal(part2, source[20:100]) {
		t.Fatalf("second range data mismatch")
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("expect no extra upstream call on segment cache hit, got=%d", got)
	}

	// 第三次读取：跨分段，应再次回源一次。
	part3 := readRange(chunkSize+10, 256)
	if !bytes.Equal(part3, source[int(chunkSize+10):int(chunkSize+266)]) {
		t.Fatalf("third range data mismatch")
	}
	if got := upstreamCalls.Load(); got != 2 {
		t.Fatalf("expect one more upstream call for new segment, got=%d", got)
	}
}

func TestBaiduRangeReadCloserDirectReadUsesSingleRangeRequest(t *testing.T) {
	source := bytes.Repeat([]byte("force-chunked-"), 1000)
	var ranges []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dataLen := int64(len(source))
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			http.Error(w, "range required", http.StatusBadRequest)
			return
		}
		parsed, err := http_range.ParseRange(rangeHeader, dataLen)
		if err != nil || len(parsed) == 0 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		ra := parsed[0]
		end := ra.Start + ra.Length
		if end > dataLen {
			end = dataLen
		}
		part := source[int(ra.Start):int(end)]
		mu.Lock()
		ranges = append(ranges, fmt.Sprintf("%d-%d", ra.Start, ra.Length))
		mu.Unlock()
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", ra.Start, end-1, dataLen))
		w.Header().Set("Content-Length", strconv.Itoa(len(part)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(part)
	}))
	defer server.Close()

	br := &baiduRangeReadCloser{
		link:     &model.Link{URL: server.URL, Header: http.Header{}},
		size:     int64(len(source)),
		partSize: int(4 * utils.KB),
	}

	rc, err := br.RangeRead(context.Background(), http_range.Range{Start: 0, Length: int64(len(source))})
	if err != nil {
		t.Fatalf("RangeRead failed: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if !bytes.Equal(data, source) {
		t.Fatalf("direct read data mismatch")
	}
	if len(ranges) != 1 {
		t.Fatalf("expected single upstream range request, got=%v", ranges)
	}
	if got := ranges[0]; got != fmt.Sprintf("%d-%d", 0, len(source)) {
		t.Fatalf("unexpected requested range=%s", got)
	}
}

func TestBaiduProxyRangeCacheEvictLRU(t *testing.T) {
	cache := newBaiduProxyRangeCache(4, time.Minute, false)
	defer cache.Close()

	mustLoad := func(key string, val []byte) {
		t.Helper()
		got, err, _ := cache.getOrLoad(context.Background(), key, func() ([]byte, error) {
			return val, nil
		})
		if err != nil {
			t.Fatalf("load key=%s failed: %v", key, err)
		}
		if !bytes.Equal(got, val) {
			t.Fatalf("load key=%s mismatch", key)
		}
	}

	mustLoad("k1", []byte("aa"))
	mustLoad("k2", []byte("bb"))

	if _, ok := cache.getLockedless("k1", time.Now()); !ok {
		t.Fatalf("expect k1 cache hit before eviction")
	}

	mustLoad("k3", []byte("cc"))

	if _, ok := cache.getLockedless("k2", time.Now()); ok {
		t.Fatalf("expect k2 evicted as LRU")
	}
	if _, ok := cache.getLockedless("k1", time.Now()); !ok {
		t.Fatalf("expect k1 still in cache")
	}
	if _, ok := cache.getLockedless("k3", time.Now()); !ok {
		t.Fatalf("expect k3 in cache")
	}
}

func TestBaiduProxyRangeCacheTTL(t *testing.T) {
	cache := newBaiduProxyRangeCache(16*int64(utils.MB), 50*time.Millisecond, false)
	defer cache.Close()

	_, err, _ := cache.getOrLoad(context.Background(), "ttl-key", func() ([]byte, error) {
		return []byte("alive"), nil
	})
	if err != nil {
		t.Fatalf("initial load failed: %v", err)
	}
	if _, ok := cache.getLockedless("ttl-key", time.Now()); !ok {
		t.Fatalf("expect ttl-key cache hit immediately")
	}
	time.Sleep(90 * time.Millisecond)
	if _, ok := cache.getLockedless("ttl-key", time.Now()); ok {
		t.Fatalf("expect ttl-key expired")
	}
}

func TestBaiduProxyRangeCacheSingleflight(t *testing.T) {
	cache := newBaiduProxyRangeCache(16*int64(utils.MB), time.Minute, false)
	defer cache.Close()

	var calls atomic.Int32
	loader := func() ([]byte, error) {
		calls.Add(1)
		time.Sleep(40 * time.Millisecond)
		return []byte("v"), nil
	}

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			got, err, _ := cache.getOrLoad(context.Background(), "same-key", loader)
			if err != nil {
				errCh <- err
				return
			}
			if string(got) != "v" {
				errCh <- fmt.Errorf("unexpected value: %s", string(got))
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("parallel getOrLoad failed: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expect loader called once, got=%d", calls.Load())
	}
}

func TestResolveBaiduMemoryProfileLowLimit(t *testing.T) {
	profile := resolveBaiduMemoryProfile(512, 0)
	if got, want := profile.proxyCacheBytes, int64(32*utils.MB); got != want {
		t.Fatalf("proxyCacheBytes = %d, want %d", got, want)
	}
	if got, want := profile.downloadTaskConcurrency, 1; got != want {
		t.Fatalf("downloadTaskConcurrency = %d, want %d", got, want)
	}
}

func TestResolveBaiduMemoryProfileUnderPressure(t *testing.T) {
	profile := resolveBaiduMemoryProfile(1024, int64(700*utils.MB))
	if got, want := profile.downloadTaskConcurrency, 1; got != want {
		t.Fatalf("downloadTaskConcurrency = %d, want %d", got, want)
	}
}

func TestBaiduRangeReadCloserSerializesSegmentsForSameFile(t *testing.T) {
	source := bytes.Repeat([]byte("serial-segment-"), 200000)
	var current atomic.Int32
	var maxConcurrent atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inflight := current.Add(1)
		for {
			seen := maxConcurrent.Load()
			if inflight <= seen || maxConcurrent.CompareAndSwap(seen, inflight) {
				break
			}
		}
		defer current.Add(-1)
		time.Sleep(40 * time.Millisecond)

		dataLen := int64(len(source))
		rangeHeader := r.Header.Get("Range")
		ranges, err := http_range.ParseRange(rangeHeader, dataLen)
		if err != nil || len(ranges) == 0 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		ra := ranges[0]
		end := ra.Start + ra.Length
		if end > dataLen {
			end = dataLen
		}
		part := source[int(ra.Start):int(end)]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", ra.Start, end-1, dataLen))
		w.Header().Set("Content-Length", strconv.Itoa(len(part)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(part)
	}))
	defer server.Close()

	cache := newBaiduProxyRangeCache(32*int64(utils.MB), time.Minute, false)
	defer cache.Close()
	br := &baiduRangeReadCloser{
		link:         &model.Link{URL: server.URL, Header: http.Header{}},
		size:         int64(len(source)),
		partSize:     256 * 1024,
		cache:        cache,
		cacheFileKey: "serial-file",
		backendID:    "baidu:test",
		metricsFile:  "/serial-file",
	}

	readFn := func(start int64) error {
		rc, err := br.RangeRead(context.Background(), http_range.Range{Start: start, Length: 128})
		if err != nil {
			return err
		}
		defer rc.Close()
		_, err = io.ReadAll(rc)
		return err
	}

	errCh := make(chan error, 2)
	go func() { errCh <- readFn(0) }()
	go func() { errCh <- readFn(300 * 1024) }()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("parallel RangeRead failed: %v", err)
		}
	}
	if got := maxConcurrent.Load(); got != 1 {
		t.Fatalf("expected same-file segment loads to serialize, max concurrent=%d", got)
	}
}

func TestBaiduProxyRangeCacheApplyMemoryPressureCriticalClearsEntries(t *testing.T) {
	cache := newBaiduProxyRangeCache(16*int64(utils.MB), time.Minute, false)
	defer cache.Close()

	_, err, _ := cache.getOrLoad(context.Background(), "k1", func() ([]byte, error) {
		return []byte("alive"), nil
	})
	if err != nil {
		t.Fatalf("initial load failed: %v", err)
	}
	cache.applyMemoryPressure(op.MemoryPressureCritical, 8*int64(utils.MB), time.Now())
	if _, ok := cache.getLockedless("k1", time.Now()); ok {
		t.Fatalf("expect cache cleared under critical memory pressure")
	}
}

func TestTokenLimiterDynamicShrinkBlocksNewAcquire(t *testing.T) {
	limiter := newTokenLimiter(3)
	if err := limiter.acquireInteractive(context.Background()); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if err := limiter.acquireInteractive(context.Background()); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}
	limiter.setLimit(1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := limiter.acquireInteractive(ctx); err == nil {
		t.Fatalf("expected acquire to block after shrinking limit")
	}

	limiter.releaseInteractive()
	limiter.releaseInteractive()
}

func TestBaiduRangeReadCloserIgnoreRangeAtZeroCompatible(t *testing.T) {
	source := bytes.Repeat([]byte("ignore-range-zero-"), 200000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dataLen := int64(len(source))
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(dataLen, 10))
			_, _ = w.Write(source)
			return
		}
		ranges, err := http_range.ParseRange(rangeHeader, dataLen)
		if err != nil || len(ranges) == 0 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		ra := ranges[0]
		// Simulate upstream that ignores range when start=0 and returns full body.
		if ra.Start == 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(dataLen, 10))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(source)
			return
		}
		end := ra.Start + ra.Length
		if end > dataLen {
			end = dataLen
		}
		part := source[int(ra.Start):int(end)]
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", ra.Start, end-1, dataLen))
		w.Header().Set("Content-Length", strconv.Itoa(len(part)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(part)
	}))
	defer server.Close()

	cache := newBaiduProxyRangeCache(32*int64(utils.MB), time.Minute, false)
	defer cache.Close()
	br := &baiduRangeReadCloser{
		link:         &model.Link{URL: server.URL, Header: http.Header{}},
		size:         int64(len(source)),
		partSize:     2 * int(utils.MB),
		cache:        cache,
		cacheFileKey: "compat-file",
	}
	rc, err := br.RangeRead(context.Background(), http_range.Range{Start: 0, Length: 512 * int64(utils.KB)})
	if err != nil {
		t.Fatalf("RangeRead failed: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if len(data) != 512*utils.KB {
		t.Fatalf("unexpected data len=%d", len(data))
	}
	if !bytes.Equal(data, source[:512*utils.KB]) {
		t.Fatalf("data mismatch")
	}
}

func TestBaiduRangeReadCloserIgnoreRangeAtNonZeroCompatible(t *testing.T) {
	source := bytes.Repeat([]byte("ignore-range-nonzero-"), 120000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dataLen := int64(len(source))
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(dataLen, 10))
			_, _ = w.Write(source)
			return
		}
		ranges, err := http_range.ParseRange(rangeHeader, dataLen)
		if err != nil || len(ranges) == 0 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		// Simulate upstream that always ignores range and returns 200 full body.
		w.Header().Set("Content-Length", strconv.FormatInt(dataLen, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(source)
	}))
	defer server.Close()

	cache := newBaiduProxyRangeCache(32*int64(utils.MB), time.Minute, false)
	defer cache.Close()
	br := &baiduRangeReadCloser{
		link:         &model.Link{URL: server.URL, Header: http.Header{}},
		size:         int64(len(source)),
		partSize:     int(2 * utils.MB),
		cache:        cache,
		cacheFileKey: "compat-file-nonzero",
		role:         requestRoleInteractive,
	}
	offset := int64(512 * utils.KB)
	length := int64(768 * utils.KB)
	rc, err := br.RangeRead(context.Background(), http_range.Range{Start: offset, Length: length})
	if err != nil {
		t.Fatalf("RangeRead failed: %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if len(data) != int(length) {
		t.Fatalf("unexpected data len=%d", len(data))
	}
	if !bytes.Equal(data, source[offset:offset+length]) {
		t.Fatalf("data mismatch")
	}
}
