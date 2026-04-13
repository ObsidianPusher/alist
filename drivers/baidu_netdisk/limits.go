package baidu_netdisk

import (
	"bytes"
	"container/list"
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/net"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/alist-org/alist/v3/pkg/singleflight"
	"github.com/alist-org/alist/v3/pkg/utils"
)

const (
	// Defaults for Baidu driver.
	defaultListTaskConcurrency     = 4
	defaultDownloadTaskConcurrency = 3
	defaultUploadTaskConcurrency   = 1
	defaultDownloadPartConcurrency = 1
	defaultDownloadPartSizeMB      = 8
	baiduRequestTimeout            = 30 * time.Minute
	baiduLimiterWaitTimeout        = 45 * time.Second
	baiduCachePruneInterval        = 30 * time.Second
	defaultBaiduDlinkCacheTTL      = 30 * time.Minute
	defaultBaiduProxyCacheTTL      = 30 * time.Minute
	baiduInteractiveCacheMinLength = int64(256 * utils.KB)
)

var activeBaiduProxyCaches sync.Map

type baiduMemoryProfile struct {
	proxyCacheBytes         int64
	downloadTaskConcurrency int
}

type keyedSerialLimiter struct {
	mu      sync.Mutex
	holders map[string]chan struct{}
}

func newKeyedSerialLimiter() *keyedSerialLimiter {
	return &keyedSerialLimiter{holders: make(map[string]chan struct{})}
}

func (l *keyedSerialLimiter) acquire(ctx context.Context, key string) (func(), error) {
	if key == "" {
		return func() {}, nil
	}
	l.mu.Lock()
	ch, ok := l.holders[key]
	if !ok {
		ch = make(chan struct{}, 1)
		l.holders[key] = ch
	}
	l.mu.Unlock()
	select {
	case ch <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				select {
				case <-ch:
				default:
				}
			})
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

var baiduFileSegmentLimiter = newKeyedSerialLimiter()

type tokenLimiter struct {
	mu               sync.Mutex
	totalLimit       int
	backgroundLimit  int
	activeTotal      int
	activeBackground int
	notify           chan struct{}
}

func newTokenLimiter(n int) *tokenLimiter {
	limiter := &tokenLimiter{notify: make(chan struct{}, 1)}
	limiter.setLimit(n)
	return limiter
}

func calcBackgroundLimit(total int) int {
	if total <= 0 {
		return 0
	}
	bgCap := total
	if total > 1 {
		bgCap = total - 1
	}
	if bgCap <= 0 {
		bgCap = 1
	}
	return bgCap
}

func (l *tokenLimiter) setLimit(n int) {
	if l == nil {
		return
	}
	if n <= 0 {
		n = 1
	}
	l.mu.Lock()
	l.totalLimit = n
	l.backgroundLimit = calcBackgroundLimit(n)
	l.mu.Unlock()
	l.wake()
}

func (l *tokenLimiter) wake() {
	if l == nil || l.notify == nil {
		return
	}
	select {
	case l.notify <- struct{}{}:
	default:
	}
}

func (l *tokenLimiter) wait(ctx context.Context) error {
	if l == nil || l.notify == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.notify:
		return nil
	}
}

func (l *tokenLimiter) tryAcquireInteractive() bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.totalLimit > 0 && l.activeTotal >= l.totalLimit {
		return false
	}
	l.activeTotal++
	return true
}

func (l *tokenLimiter) tryAcquireBackground() bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.totalLimit > 0 && l.activeTotal >= l.totalLimit {
		return false
	}
	if l.backgroundLimit > 0 && l.activeBackground >= l.backgroundLimit {
		return false
	}
	l.activeTotal++
	l.activeBackground++
	return true
}

func currentBaiduMemoryProfile() baiduMemoryProfile {
	limitBytes := op.GetProcessMemoryLimitBytes()
	limitMB := limitBytes / int64(utils.MB)
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return resolveBaiduMemoryProfile(limitMB, int64(mem.Alloc))
}

func resolveBaiduMemoryProfile(limitMB, allocBytes int64) baiduMemoryProfile {
	const (
		minCacheBytes     = int64(16 * utils.MB)
		defaultCacheBytes = int64(256 * utils.MB)
		reservedBytes     = int64(96 * utils.MB)
	)
	if limitMB <= 0 {
		return baiduMemoryProfile{
			proxyCacheBytes:         defaultCacheBytes,
			downloadTaskConcurrency: defaultDownloadTaskConcurrency,
		}
	}

	profile := baiduMemoryProfile{
		proxyCacheBytes:         defaultCacheBytes,
		downloadTaskConcurrency: defaultDownloadTaskConcurrency,
	}

	switch {
	case limitMB <= 512:
		profile.proxyCacheBytes = int64(32 * utils.MB)
		profile.downloadTaskConcurrency = 1
	case limitMB <= 1024:
		profile.proxyCacheBytes = int64(64 * utils.MB)
		profile.downloadTaskConcurrency = 2
	case limitMB <= 2048:
		profile.proxyCacheBytes = int64(128 * utils.MB)
		profile.downloadTaskConcurrency = 2
	default:
		profile.proxyCacheBytes = int64(192 * utils.MB)
		profile.downloadTaskConcurrency = 3
	}

	limitBytes := limitMB * int64(utils.MB)
	if limitBytes <= minCacheBytes {
		profile.proxyCacheBytes = minCacheBytes
		profile.downloadTaskConcurrency = 1
		return profile
	}

	available := limitBytes - allocBytes - reservedBytes
	if available < minCacheBytes {
		available = minCacheBytes
	}
	target := available / 2
	if target < minCacheBytes {
		target = minCacheBytes
	}
	upper := limitBytes / 4
	if upper < minCacheBytes {
		upper = minCacheBytes
	}
	if target > upper {
		target = upper
	}
	if target < profile.proxyCacheBytes {
		profile.proxyCacheBytes = target
	}

	switch {
	case allocBytes > limitBytes*3/4:
		profile.downloadTaskConcurrency = 1
	case allocBytes > limitBytes/2:
		if profile.downloadTaskConcurrency > 1 {
			profile.downloadTaskConcurrency = 1
		}
	}

	if profile.proxyCacheBytes < minCacheBytes {
		profile.proxyCacheBytes = minCacheBytes
	}
	if profile.downloadTaskConcurrency < 1 {
		profile.downloadTaskConcurrency = 1
	}
	return profile
}

func resolveBaiduProxyCacheMaxBytes() (maxBytes int64) {
	return currentBaiduMemoryProfile().proxyCacheBytes
}

func (l *tokenLimiter) acquireInteractive(ctx context.Context) error {
	if l == nil {
		return nil
	}
	for {
		if l.tryAcquireInteractive() {
			return nil
		}
		if err := l.wait(ctx); err != nil {
			return err
		}
	}
}

func (l *tokenLimiter) acquireBackground(ctx context.Context) error {
	if l == nil {
		return nil
	}
	for {
		if l.tryAcquireBackground() {
			return nil
		}
		if err := l.wait(ctx); err != nil {
			return err
		}
	}
}

func (l *tokenLimiter) acquire(ctx context.Context) error {
	return l.acquireInteractive(ctx)
}

func (l *tokenLimiter) releaseInteractive() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.activeTotal > 0 {
		l.activeTotal--
	}
	l.mu.Unlock()
	l.wake()
}

func (l *tokenLimiter) releaseBackground() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.activeTotal > 0 {
		l.activeTotal--
	}
	if l.activeBackground > 0 {
		l.activeBackground--
	}
	l.mu.Unlock()
	l.wake()
}

func (l *tokenLimiter) release() {
	l.releaseInteractive()
}

type requestRole uint8

const (
	requestRoleBackground requestRole = iota
	requestRoleInteractive
)

type baiduRangeReadCloser struct {
	link         *model.Link
	size         int64
	limiter      *tokenLimiter
	concurrency  int
	partSize     int
	cache        *baiduProxyRangeCache
	cacheFileKey string
	backendID    string
	metricsFile  string
	role         requestRole
	utils.Closers
}

func newBaiduRangeReadCloser(link *model.Link, size int64, limiter *tokenLimiter, concurrency int, partSize int, cache *baiduProxyRangeCache, cacheFileKey, backendID, metricsFile string, role requestRole) model.RangeReadCloserIF {
	return &baiduRangeReadCloser{
		link:         link,
		size:         size,
		limiter:      limiter,
		concurrency:  concurrency,
		partSize:     partSize,
		cache:        cache,
		cacheFileKey: cacheFileKey,
		backendID:    backendID,
		metricsFile:  metricsFile,
		role:         role,
	}
}

func (b *baiduRangeReadCloser) RangeRead(ctx context.Context, r http_range.Range) (io.ReadCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reqRange, err := b.normalizeRequestRange(r)
	if err != nil {
		return nil, err
	}
	if reqRange.Length == 0 {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	releaseLimiter, err := b.acquireLimiter(ctx)
	if err != nil {
		return nil, err
	}
	if b.cache == nil || b.cacheFileKey == "" {
		return b.rangeReadDirect(ctx, reqRange, releaseLimiter)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	rcPipe, wcPipe := io.Pipe()
	rc := &releaseReadCloser{
		ReadCloser: rcPipe,
		release:    releaseLimiter,
		cancel:     cancel,
	}
	b.Add(rc)
	go func() {
		defer releaseLimiter()
		defer cancel()
		if err := b.streamRangeWithCache(streamCtx, wcPipe, reqRange); err != nil {
			_ = wcPipe.CloseWithError(err)
			return
		}
		_ = wcPipe.Close()
	}()
	return rc, nil
}

func (b *baiduRangeReadCloser) acquireLimiter(ctx context.Context) (func(), error) {
	if b.limiter == nil {
		return func() {}, nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, baiduLimiterWaitTimeout)
	defer cancel()
	if err := b.limiter.acquire(waitCtx); err != nil {
		return nil, err
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			b.limiter.release()
		})
	}
	return release, nil
}

func (b *baiduRangeReadCloser) rangeReadDirect(ctx context.Context, r http_range.Range, releaseLimiter func()) (io.ReadCloser, error) {
	rc, cancel, err := b.openRangeReaderFromSource(ctx, r)
	if err != nil {
		releaseLimiter()
		return nil, err
	}
	rc = &releaseReadCloser{
		ReadCloser: rc,
		release:    releaseLimiter,
		cancel:     cancel,
	}
	b.Add(rc)
	return rc, nil
}

func (b *baiduRangeReadCloser) openRangeReaderFromSource(ctx context.Context, r http_range.Range) (io.ReadCloser, context.CancelFunc, error) {
	if b.link == nil || b.link.URL == "" {
		return nil, nil, fmt.Errorf("empty baidu link")
	}
	downloadCtx, cancel := context.WithTimeout(ctx, baiduRequestTimeout)
	header := net.ProcessHeader(http.Header{}, b.link.Header)
	header = http_range.ApplyRangeToHttpHeader(r, header)
	resp, err := net.RequestHttp(downloadCtx, http.MethodGet, header, b.link.URL)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if resp.StatusCode == http.StatusOK && r.Start > 0 {
		readCloser, rangeErr := net.GetRangedHttpReader(resp.Body, r.Start, r.Length)
		if rangeErr != nil {
			_ = resp.Body.Close()
			cancel()
			return nil, nil, fmt.Errorf("upstream range fallback failed at offset %d: %w", r.Start, rangeErr)
		}
		return readCloser, cancel, nil
	}
	if resp.StatusCode == http.StatusOK && r.Length >= 0 && b.size > 0 && r.Length < b.size {
		readCloser, rangeErr := net.GetRangedHttpReader(resp.Body, r.Start, r.Length)
		if rangeErr != nil {
			_ = resp.Body.Close()
			cancel()
			return nil, nil, fmt.Errorf("upstream range fallback failed at offset %d length %d: %w", r.Start, r.Length, rangeErr)
		}
		return readCloser, cancel, nil
	}
	return resp.Body, cancel, nil
}

func (b *baiduRangeReadCloser) segmentLoadKey() string {
	if b.cacheFileKey != "" {
		return b.cacheFileKey
	}
	return b.metricsFile
}

func (b *baiduRangeReadCloser) acquireSegmentSerial(ctx context.Context) (func(), error) {
	waitCtx, cancel := context.WithTimeout(ctx, baiduLimiterWaitTimeout)
	defer cancel()
	return baiduFileSegmentLimiter.acquire(waitCtx, b.segmentLoadKey())
}

func (b *baiduRangeReadCloser) recordDownloadBlock(offset, size int64, startedAt, finishedAt time.Time) {
	recordBaiduTransferBlock(baiduTransferDirectionDownload, baiduTransferBlock{
		BackendID:  b.backendID,
		File:       b.metricsFile,
		Offset:     offset,
		Size:       size,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	})
}

func (b *baiduRangeReadCloser) loadSegmentFromSource(ctx context.Context, start, length int64) ([]byte, error) {
	if length <= 0 {
		return []byte{}, nil
	}
	release, err := b.acquireSegmentSerial(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	finishActive := beginBaiduTransferBlock(baiduTransferDirectionDownload)
	defer finishActive()
	segmentRange := http_range.Range{Start: start, Length: length}
	startedAt := time.Now()
	upstream, cancel, err := b.openRangeReaderFromSource(ctx, segmentRange)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer upstream.Close()
	data := make([]byte, int(length))
	if _, err := io.ReadFull(upstream, data); err != nil {
		return nil, err
	}
	if int64(len(data)) != length {
		return nil, fmt.Errorf("segment short read: start=%d expected=%d got=%d", start, length, len(data))
	}
	b.recordDownloadBlock(start, length, startedAt, time.Now())
	return data, nil
}

func (b *baiduRangeReadCloser) normalizeRequestRange(r http_range.Range) (http_range.Range, error) {
	if r.Start < 0 {
		return http_range.Range{}, fmt.Errorf("invalid range start: %d", r.Start)
	}
	req := r
	if req.Length == 0 {
		return req, nil
	}
	if req.Length < 0 {
		if b.size <= 0 {
			return http_range.Range{}, fmt.Errorf("unable to resolve open-ended range: start=%d, size=%d", req.Start, b.size)
		}
		req.Length = b.size - req.Start
	}
	if b.size > 0 {
		if req.Start >= b.size {
			req.Length = 0
			return req, nil
		}
		maxLen := b.size - req.Start
		if req.Length > maxLen {
			req.Length = maxLen
		}
	}
	if req.Length < 0 {
		return http_range.Range{}, fmt.Errorf("invalid range length: %d", req.Length)
	}
	return req, nil
}

func (b *baiduRangeReadCloser) streamRangeWithCache(ctx context.Context, w *io.PipeWriter, r http_range.Range) error {
	if !b.shouldUseSegmentCache(r) {
		return b.streamRangeFromSourceIntoWriter(ctx, w, r)
	}
	segmentSize := b.cacheSegmentSize()
	end := r.Start + r.Length
	for pos := r.Start; pos < end; {
		segment := b.segmentRange(pos/segmentSize, segmentSize)
		if segment.length <= 0 {
			return fmt.Errorf("invalid segment length for index=%d", segment.index)
		}
		data, err, _ := b.cache.getOrLoad(ctx, segment.cacheKey(b.cacheFileKey), func() ([]byte, error) {
			return b.loadSegmentFromSource(ctx, segment.start, segment.length)
		})
		if err != nil {
			return b.streamRangeFromSourceIntoWriter(ctx, w, http_range.Range{Start: pos, Length: end - pos})
		}
		offset := pos - segment.start
		if offset < 0 || offset >= int64(len(data)) {
			return b.streamRangeFromSourceIntoWriter(ctx, w, http_range.Range{Start: pos, Length: end - pos})
		}
		available := int64(len(data)) - offset
		toWrite := end - pos
		if toWrite > available {
			toWrite = available
		}
		if toWrite <= 0 {
			return b.streamRangeFromSourceIntoWriter(ctx, w, http_range.Range{Start: pos, Length: end - pos})
		}
		if err := writeAll(w, data[offset:offset+toWrite]); err != nil {
			return err
		}
		pos += toWrite
	}
	return nil
}

func (b *baiduRangeReadCloser) streamRangeFromSourceIntoWriter(ctx context.Context, w io.Writer, r http_range.Range) error {
	upstream, cancel, err := b.openRangeReaderFromSource(ctx, r)
	if err != nil {
		return err
	}
	defer cancel()
	defer upstream.Close()
	readBuf := utils.IoBuffPool.Get().([]byte)
	defer utils.IoBuffPool.Put(readBuf)
	var written int64
	for {
		n, readErr := upstream.Read(readBuf)
		if n > 0 {
			remaining := r.Length - written
			if remaining <= 0 {
				return nil
			}
			chunk := readBuf[:n]
			if int64(len(chunk)) > remaining {
				chunk = chunk[:remaining]
			}
			if err := writeAll(w, chunk); err != nil {
				return err
			}
			written += int64(len(chunk))
			if written >= r.Length {
				return nil
			}
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			if written != r.Length {
				return fmt.Errorf("source range short read: expected=%d got=%d", r.Length, written)
			}
			return nil
		}
		return readErr
	}
}

func (b *baiduRangeReadCloser) shouldUseSegmentCache(r http_range.Range) bool {
	if b.cache == nil || b.cacheFileKey == "" || r.Length <= 0 {
		return false
	}
	segmentSize := b.cacheSegmentSize()
	if segmentSize <= 0 {
		return false
	}
	if b.cache.maxBytes > 0 {
		if segmentSize > b.cache.maxBytes {
			return false
		}
		// Skip tiny probing requests in interactive playback.
		if b.role == requestRoleInteractive && r.Length < baiduInteractiveCacheMinLength {
			return false
		}
		// Avoid polluting cache with oversized one-off requests.
		if r.Length > b.cache.maxBytes/2 {
			return false
		}
	}
	return true
}

func (b *baiduRangeReadCloser) currentProfile() baiduMemoryProfile {
	if b.cache != nil {
		return b.cache.currentProfile()
	}
	return currentBaiduMemoryProfile()
}

func (b *baiduRangeReadCloser) cacheSegmentSize() int64 {
	if b.partSize > 0 {
		return int64(b.partSize)
	}
	return int64(defaultDownloadPartSizeMB) * int64(utils.MB)
}

func (b *baiduRangeReadCloser) segmentRange(index int64, segmentSize int64) baiduCacheSegment {
	start := index * segmentSize
	length := segmentSize
	if b.size > 0 && start+length > b.size {
		length = b.size - start
	}
	if length < 0 {
		length = 0
	}
	return baiduCacheSegment{
		index:  index,
		start:  start,
		length: length,
	}
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func (b *baiduRangeReadCloser) Close() error {
	return b.Closers.Close()
}

type releaseReadCloser struct {
	io.ReadCloser
	released bool
	release  func()
	cancel   context.CancelFunc
}

func (r *releaseReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if !r.released && r.release != nil {
		r.release()
		r.released = true
	}
	return err
}

type baiduProxyRangeCache struct {
	mu       sync.Mutex
	maxBytes int64
	ttl      time.Duration
	auto     bool

	usedBytes int64
	ll        *list.List
	entries   map[string]*list.Element

	loader    singleflight.Group[[]byte]
	stopCh    chan struct{}
	closeOnce sync.Once
}

type baiduCacheSegment struct {
	index  int64
	start  int64
	length int64
}

func (s baiduCacheSegment) cacheKey(fileKey string) string {
	return fmt.Sprintf("%s:%d", fileKey, s.index)
}

type baiduProxyRangeCacheEntry struct {
	key      string
	data     []byte
	size     int64
	expireAt time.Time
}

func newBaiduProxyRangeCache(maxBytes int64, ttl time.Duration, autoAdjust bool) *baiduProxyRangeCache {
	if maxBytes <= 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = defaultBaiduProxyCacheTTL
	}
	cache := &baiduProxyRangeCache{
		maxBytes: maxBytes,
		ttl:      ttl,
		auto:     autoAdjust,
		ll:       list.New(),
		entries:  make(map[string]*list.Element),
		stopCh:   make(chan struct{}),
	}
	go cache.pruneLoop()
	return cache
}

func (c *baiduProxyRangeCache) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		close(c.stopCh)
	})
}

func (c *baiduProxyRangeCache) getOrLoad(ctx context.Context, key string, loader func() ([]byte, error)) ([]byte, error, bool) {
	if c == nil {
		data, err := loader()
		return data, err, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	if data, ok := c.getLockedless(key, now); ok {
		return data, nil, true
	}
	ch := c.loader.DoChan(key, func() ([]byte, error) {
		nowInner := time.Now()
		if data, ok := c.getLockedless(key, nowInner); ok {
			return data, nil
		}
		data, err := loader()
		if err != nil {
			return nil, err
		}
		c.setLockedless(key, data, time.Now())
		return data, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err(), false
	case res := <-ch:
		return res.Val, res.Err, false
	}
}

func (c *baiduProxyRangeCache) getLockedless(key string, now time.Time) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		return nil, false
	}
	element, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	entry := element.Value.(*baiduProxyRangeCacheEntry)
	if c.isExpired(entry, now) {
		c.removeElementLocked(element)
		return nil, false
	}
	if c.ttl > 0 {
		entry.expireAt = now.Add(c.ttl)
	}
	c.ll.MoveToFront(element)
	return entry.data, true
}

func (c *baiduProxyRangeCache) setLockedless(key string, data []byte, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.refreshCapacityLocked(now)
	maxBytes := c.maxBytes
	c.mu.Unlock()
	size := int64(len(data))
	if size <= 0 || size > maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.ll = list.New()
		c.entries = make(map[string]*list.Element)
		c.usedBytes = 0
	}
	c.refreshCapacityLocked(now)
	if oldElement, ok := c.entries[key]; ok {
		c.removeElementLocked(oldElement)
	}
	entry := &baiduProxyRangeCacheEntry{
		key:      key,
		data:     data,
		size:     size,
		expireAt: now.Add(c.ttl),
	}
	element := c.ll.PushFront(entry)
	c.entries[key] = element
	c.usedBytes += size
	c.pruneExpiredLocked(now)
	c.evictLocked()
}

func (c *baiduProxyRangeCache) pruneLoop() {
	interval := baiduCachePruneInterval
	if c.ttl > 0 && c.ttl < interval {
		interval = c.ttl
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.pruneExpired(time.Now())
		}
	}
}

func (c *baiduProxyRangeCache) pruneExpired(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshCapacityLocked(now)
}

func (c *baiduProxyRangeCache) refreshCapacityLocked(now time.Time) {
	if c == nil {
		return
	}
	if c.auto {
		desired := resolveBaiduProxyCacheMaxBytes()
		if desired > 0 && desired != c.maxBytes {
			c.maxBytes = desired
		}
	}
	c.pruneExpiredLocked(now)
	c.evictLocked()
}

func (c *baiduProxyRangeCache) updateSoftMaxBytes(maxBytes int64, now time.Time) {
	if c == nil || maxBytes <= 0 {
		return
	}
	c.mu.Lock()
	if maxBytes < c.maxBytes || c.maxBytes <= 0 {
		c.maxBytes = maxBytes
	}
	c.pruneExpiredLocked(now)
	c.evictLocked()
	c.mu.Unlock()
}

func (c *baiduProxyRangeCache) applyMemoryPressure(level op.MemoryPressureLevel, desiredMaxBytes int64, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch level {
	case op.MemoryPressureCritical:
		c.clearLocked()
		if desiredMaxBytes > 0 {
			c.maxBytes = desiredMaxBytes
		}
	case op.MemoryPressureHigh:
		if desiredMaxBytes > 0 && (c.maxBytes <= 0 || desiredMaxBytes < c.maxBytes) {
			c.maxBytes = desiredMaxBytes
		}
		c.pruneExpiredLocked(now)
		c.evictLocked()
	case op.MemoryPressureModerate:
		if desiredMaxBytes > 0 && desiredMaxBytes < c.maxBytes {
			c.maxBytes = desiredMaxBytes
		}
		c.pruneExpiredLocked(now)
		c.evictLocked()
	}
}

func (c *baiduProxyRangeCache) pruneExpiredLocked(now time.Time) {
	if c.entries == nil {
		return
	}
	for element := c.ll.Back(); element != nil; {
		prev := element.Prev()
		entry := element.Value.(*baiduProxyRangeCacheEntry)
		if c.isExpired(entry, now) {
			c.removeElementLocked(element)
		}
		element = prev
	}
}

func (c *baiduProxyRangeCache) evictLocked() {
	for c.usedBytes > c.maxBytes {
		element := c.ll.Back()
		if element == nil {
			break
		}
		c.removeElementLocked(element)
	}
}

func (c *baiduProxyRangeCache) removeElementLocked(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(*baiduProxyRangeCacheEntry)
	c.ll.Remove(element)
	delete(c.entries, entry.key)
	c.usedBytes -= entry.size
	if c.usedBytes < 0 {
		c.usedBytes = 0
	}
}

func (c *baiduProxyRangeCache) clearLocked() {
	if c.entries == nil {
		return
	}
	c.entries = make(map[string]*list.Element)
	c.ll.Init()
	c.usedBytes = 0
}

func (c *baiduProxyRangeCache) isExpired(entry *baiduProxyRangeCacheEntry, now time.Time) bool {
	if c.ttl <= 0 {
		return false
	}
	return !entry.expireAt.IsZero() && now.After(entry.expireAt)
}

func (c *baiduProxyRangeCache) currentProfile() baiduMemoryProfile {
	return currentBaiduMemoryProfile()
}

func registerActiveBaiduProxyCache(cache *baiduProxyRangeCache) {
	if cache == nil {
		return
	}
	activeBaiduProxyCaches.Store(cache, struct{}{})
}

func unregisterActiveBaiduProxyCache(cache *baiduProxyRangeCache) {
	if cache == nil {
		return
	}
	activeBaiduProxyCaches.Delete(cache)
}

func handleBaiduMemoryPressure(snapshot op.MemoryPressureSnapshot) {
	if snapshot.LimitBytes <= 0 || snapshot.Level == op.MemoryPressureNone {
		return
	}
	limitMB := snapshot.LimitBytes / int64(utils.MB)
	profile := resolveBaiduMemoryProfile(limitMB, int64(snapshot.AllocBytes))
	targetBytes := profile.proxyCacheBytes
	switch snapshot.Level {
	case op.MemoryPressureHigh:
		targetBytes /= 2
	case op.MemoryPressureCritical:
		targetBytes = 8 * int64(utils.MB)
	}
	now := time.Now()
	activeBaiduProxyCaches.Range(func(key, value any) bool {
		cache, ok := key.(*baiduProxyRangeCache)
		if ok {
			cache.applyMemoryPressure(snapshot.Level, targetBytes, now)
		}
		return true
	})
}

func init() {
	op.RegisterMemoryPressureHook(handleBaiduMemoryPressure)
}

func buildBaiduProxyFileCacheKey(backendID string, file model.Obj) string {
	if backendID == "" {
		backendID = "baidu"
	}
	if file == nil {
		return fmt.Sprintf("%s:unknown", backendID)
	}
	return fmt.Sprintf("%s:%s:%s:%d:%d", backendID, file.GetID(), file.GetPath(), file.GetSize(), file.ModTime().Unix())
}
