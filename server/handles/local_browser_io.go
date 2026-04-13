package handles

import (
	"context"
	"io"
	"sync"
)

const localBrowserCopyBufferSize = 8 * 1024 * 1024
const localBrowserCopyNoProgressLimit = 64

var localBrowserCopyBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, localBrowserCopyBufferSize)
	},
}

func localBrowserCopyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := localBrowserCopyBufferPool.Get().([]byte)
	defer localBrowserCopyBufferPool.Put(buf)

	var written int64
	noProgressReads := 0
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return written, err
			}
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			noProgressReads = 0
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return written, ew
			}
			if nw != nr {
				return written, io.ErrShortWrite
			}
		} else if er == nil {
			noProgressReads++
			if noProgressReads >= localBrowserCopyNoProgressLimit {
				return written, io.ErrNoProgress
			}
		}
		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			return written, er
		}
	}
}
