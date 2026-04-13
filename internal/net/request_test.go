package net

//no http range
//

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
)

var buf22MB = make([]byte, 1024*1024*22)

func dummyHttpRequest(data []byte, p http_range.Range) io.ReadCloser {

	end := p.Start + p.Length - 1

	if end >= int64(len(data)) {
		end = int64(len(data))
	}

	bodyBytes := data[p.Start:end]
	return io.NopCloser(bytes.NewReader(bodyBytes))
}

func TestDownloadOrder(t *testing.T) {
	buff := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	downloader, invocations, ranges := newDownloadRangeClient(buff)
	con, partSize := 3, 3
	d := NewDownloader(func(d *Downloader) {
		d.Concurrency = con
		d.PartSize = partSize
		d.HttpClient = downloader.HttpRequest
	})

	var start, length int64 = 2, 10
	length2 := length
	if length2 == -1 {
		length2 = int64(len(buff)) - start
	}
	req := &HttpRequestParams{
		Range: http_range.Range{Start: start, Length: length},
		Size:  int64(len(buff)),
	}
	readCloser, err := d.Download(context.Background(), req)

	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	resultBuf, err := io.ReadAll(readCloser)
	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	if exp, a := int(length), len(resultBuf); exp != a {
		t.Errorf("expect  buffer length=%d, got %d", exp, a)
	}
	chunkSize := int(length)/partSize + 1
	if int(length)%partSize == 0 {
		chunkSize--
	}
	if e, a := chunkSize, *invocations; e != a {
		t.Errorf("expect %v API calls, got %v", e, a)
	}

	expectRngs := []string{"2-3", "5-3", "8-3", "11-1"}
	for _, rng := range expectRngs {
		if !slices.Contains(*ranges, rng) {
			t.Errorf("expect range %v, but absent in return", rng)
		}
	}
	if e, a := expectRngs, *ranges; len(e) != len(a) {
		t.Errorf("expect %v ranges, got %v", e, a)
	}
}
func init() {
	Formatter := new(logrus.TextFormatter)
	Formatter.TimestampFormat = "2006-01-02T15:04:05.999999999"
	Formatter.FullTimestamp = true
	Formatter.ForceColors = true
	logrus.SetFormatter(Formatter)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.Debugf("Download start")
}

func TestDownloadSingle(t *testing.T) {
	buff := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	downloader, invocations, ranges := newDownloadRangeClient(buff)
	con, partSize := 1, 3
	d := NewDownloader(func(d *Downloader) {
		d.Concurrency = con
		d.PartSize = partSize
		d.HttpClient = downloader.HttpRequest
	})

	var start, length int64 = 2, 10
	req := &HttpRequestParams{
		Range: http_range.Range{Start: start, Length: length},
		Size:  int64(len(buff)),
	}

	readCloser, err := d.Download(context.Background(), req)

	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	resultBuf, err := io.ReadAll(readCloser)
	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	if exp, a := int(length), len(resultBuf); exp != a {
		t.Errorf("expect  buffer length=%d, got %d", exp, a)
	}
	if e, a := 1, *invocations; e != a {
		t.Errorf("expect %v API calls, got %v", e, a)
	}

	expectRngs := []string{"2-10"}
	for _, rng := range expectRngs {
		if !slices.Contains(*ranges, rng) {
			t.Errorf("expect range %v, but absent in return", rng)
		}
	}
	if e, a := expectRngs, *ranges; len(e) != len(a) {
		t.Errorf("expect %v ranges, got %v", e, a)
	}
}

func TestDownloadSingleForceChunked(t *testing.T) {
	buff := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	downloader, invocations, ranges := newDownloadRangeClient(buff)
	d := NewDownloader(func(d *Downloader) {
		d.Concurrency = 1
		d.PartSize = 3
		d.ForceChunked = true
		d.HttpClient = downloader.HttpRequest
	})

	req := &HttpRequestParams{
		Range: http_range.Range{Start: 0, Length: int64(len(buff))},
		Size:  int64(len(buff)),
	}
	readCloser, err := d.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	resultBuf, err := io.ReadAll(readCloser)
	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	if !bytes.Equal(resultBuf, buff) {
		t.Fatalf("unexpected read result: %v", resultBuf)
	}
	if e, a := 4, *invocations; e != a {
		t.Errorf("expect %v API calls, got %v", e, a)
	}
	expectRngs := []string{"0-3", "3-3", "6-3", "9-3"}
	for _, rng := range expectRngs {
		if !slices.Contains(*ranges, rng) {
			t.Errorf("expect range %v, but absent in return", rng)
		}
	}
}

func TestDownloadForceChunkedDoesNotCapPartSize(t *testing.T) {
	buff := make([]byte, 100)
	for i := range buff {
		buff[i] = byte(i)
	}
	downloader, invocations, ranges := newDownloadRangeClient(buff)
	d := NewDownloader(func(d *Downloader) {
		d.Concurrency = 1
		d.PartSize = 40
		d.ForceChunked = true
		d.HttpClient = downloader.HttpRequest
	})

	req := &HttpRequestParams{
		Range: http_range.Range{Start: 0, Length: int64(len(buff))},
		Size:  int64(len(buff)),
	}
	readCloser, err := d.Download(context.Background(), req)
	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	resultBuf, err := io.ReadAll(readCloser)
	if err != nil {
		t.Fatalf("expect no error, got %v", err)
	}
	if !bytes.Equal(resultBuf, buff) {
		t.Fatalf("unexpected read result length=%d", len(resultBuf))
	}
	if e, a := 3, *invocations; e != a {
		t.Errorf("expect %v API calls, got %v", e, a)
	}
	expectRngs := []string{"0-40", "40-40", "80-20"}
	for _, rng := range expectRngs {
		if !slices.Contains(*ranges, rng) {
			t.Errorf("expect range %v, but absent in return", rng)
		}
	}
}

func TestBufCloseReleasesLargeBackingStore(t *testing.T) {
	buf := NewBuf(context.Background(), 9*1024*1024, 0)
	payload := bytes.Repeat([]byte("a"), 1024)
	if _, err := buf.Write(payload); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if cap(buf.buffer) == 0 {
		t.Fatalf("expected backing store allocated")
	}
	buf.Close()
	if cap(buf.buffer) != 0 {
		t.Fatalf("expected backing store released, got cap=%d", cap(buf.buffer))
	}
}

func TestMultiReadCloserReleasesConsumedBuffer(t *testing.T) {
	first := NewBuf(context.Background(), 9*1024*1024, 0)
	first.Reset(1)
	if _, err := first.Write([]byte("a")); err != nil {
		t.Fatalf("write first failed: %v", err)
	}
	second := NewBuf(context.Background(), 1, 1)
	second.Reset(1)
	if _, err := second.Write([]byte("b")); err != nil {
		t.Fatalf("write second failed: %v", err)
	}
	rc := NewMultiReadCloser(first, func() error { return nil }, func(id int) (bool, *Buf) {
		return false, second
	})
	defer rc.Close()

	tmp := make([]byte, 1)
	if _, err := rc.Read(tmp); err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if _, err := rc.Read(tmp); err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if cap(first.buffer) != 0 {
		t.Fatalf("expected first buffer released after consumption, got cap=%d", cap(first.buffer))
	}
}

type downloadCaptureClient struct {
	mockedHttpRequest    func(params *HttpRequestParams) (*http.Response, error)
	GetObjectInvocations int

	RetrievedRanges []string

	lock sync.Mutex
}

func (c *downloadCaptureClient) HttpRequest(ctx context.Context, params *HttpRequestParams) (*http.Response, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.GetObjectInvocations++

	if &params.Range != nil {
		c.RetrievedRanges = append(c.RetrievedRanges, fmt.Sprintf("%d-%d", params.Range.Start, params.Range.Length))
	}

	return c.mockedHttpRequest(params)
}

func newDownloadRangeClient(data []byte) (*downloadCaptureClient, *int, *[]string) {
	capture := &downloadCaptureClient{}

	capture.mockedHttpRequest = func(params *HttpRequestParams) (*http.Response, error) {
		start, fin := params.Range.Start, params.Range.Start+params.Range.Length
		if params.Range.Length == -1 || fin >= int64(len(data)) {
			fin = int64(len(data))
		}
		bodyBytes := data[start:fin]

		header := &http.Header{}
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, fin-1, len(data)))
		return &http.Response{
			Body:          io.NopCloser(bytes.NewReader(bodyBytes)),
			Header:        *header,
			ContentLength: int64(len(bodyBytes)),
		}, nil
	}

	return capture, &capture.GetObjectInvocations, &capture.RetrievedRanges
}
