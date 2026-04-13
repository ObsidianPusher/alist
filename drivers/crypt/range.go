package crypt

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/model"
	internalnet "github.com/alist-org/alist/v3/internal/net"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/pkg/http_range"
	"github.com/alist-org/alist/v3/pkg/utils"
	log "github.com/sirupsen/logrus"
)

type cryptRangeReadCloser struct {
	rangeReader model.RangeReaderFunc
	baseClosers utils.Closers
}

func newCryptRangeReadCloser(rangeReader model.RangeReaderFunc, baseClosers utils.Closers) model.RangeReadCloserIF {
	return &cryptRangeReadCloser{
		rangeReader: rangeReader,
		baseClosers: baseClosers,
	}
}

func (c *cryptRangeReadCloser) RangeRead(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
	return c.rangeReader(ctx, httpRange)
}

func (c *cryptRangeReadCloser) GetClosers() utils.Closers {
	return c.baseClosers
}

func (c *cryptRangeReadCloser) Add(closer io.Closer) {
	c.baseClosers.Add(closer)
}

func (c *cryptRangeReadCloser) AddClosers(closers utils.Closers) {
	c.baseClosers.AddClosers(closers)
}

func (c *cryptRangeReadCloser) Close() error {
	return c.baseClosers.Close()
}

func newCryptHTTPRangeReadCloser(size int64, link *model.Link) (model.RangeReadCloserIF, error) {
	if link == nil || link.URL == "" {
		return nil, fmt.Errorf("empty remote link url")
	}
	header := internalnet.ProcessHeader(http.Header{}, link.Header)
	rangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
		if link.Concurrency != 0 || link.PartSize != 0 {
			down := internalnet.NewDownloader(func(d *internalnet.Downloader) {
				d.Concurrency = link.Concurrency
				d.PartSize = link.PartSize
			})
			req := &internalnet.HttpRequestParams{
				URL:       link.URL,
				Range:     httpRange,
				Size:      size,
				HeaderRef: header,
			}
			rc, err := down.Download(ctx, req)
			if err != nil {
				return nil, errs.NewErr(err, "crypt remote range read failed")
			}
			return rc, nil
		}
		response, err := stream.RequestRangedHttp(ctx, link, httpRange.Start, httpRange.Length)
		if err != nil {
			if response == nil {
				return nil, fmt.Errorf("http request failure, err:%s", err)
			}
			return nil, fmt.Errorf("http request failure,status: %d err:%s", response.StatusCode, err)
		}
		if (httpRange.Start == 0 && (httpRange.Length == -1 || httpRange.Length == size)) ||
			response.StatusCode == http.StatusPartialContent ||
			checkContentRange(&response.Header, httpRange.Start) {
			return response.Body, nil
		}
		if response.StatusCode == http.StatusOK {
			log.Warnf("crypt remote http server not supporting range request, expect low performance")
			readCloser, rangeErr := internalnet.GetRangedHttpReader(response.Body, httpRange.Start, httpRange.Length)
			if rangeErr != nil {
				_ = response.Body.Close()
				return nil, rangeErr
			}
			return readCloser, nil
		}
		return response.Body, nil
	}
	return newCryptRangeReadCloser(rangeReader, utils.EmptyClosers()), nil
}

func checkContentRange(header *http.Header, offset int64) bool {
	start, _, err := http_range.ParseContentRange(header.Get("Content-Range"))
	if err != nil {
		log.Warnf("exception trying to parse Content-Range, will ignore,err=%s", err)
	}
	return start == offset
}
