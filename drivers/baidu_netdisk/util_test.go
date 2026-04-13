package baidu_netdisk

import (
	"testing"
	"time"

	"github.com/Xhofe/go-cache"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/pkg/utils"
)

func TestBaiduNetdiskDlinkURLCache(t *testing.T) {
	d := &BaiduNetdisk{
		dlinkURLCache: cache.NewMemCache[string](cache.WithShards[string](1)),
	}

	d.setCachedLinkURL("cached", "https://example.com/dlink")

	got, ok := d.getCachedLinkURL("cached")
	if !ok || got != "https://example.com/dlink" {
		t.Fatalf("expected cached dlink url, got=%q ok=%v", got, ok)
	}
}

func TestBaiduNetdiskApplyDownloadPolicyUsesRolePolicy(t *testing.T) {
	backgroundLimiter := newTokenLimiter(3)
	d := &BaiduNetdisk{
		Addition: Addition{
			DownloadPartSizeMB: 40,
		},
		downloadLimiter:         backgroundLimiter,
		downloadPartConcurrency: defaultDownloadPartConcurrency,
		downloadPartSize:        40 * int(utils.MB),
	}
	file := &model.Object{
		Path:     "/dir/file.txt",
		Name:     "file.txt",
		Size:     1024,
		Modified: time.Unix(100, 0),
		IsFolder: false,
	}

	link := &model.Link{
		URL:         "https://example.com/file",
		RequestRole: "interactive",
	}
	d.applyDownloadPolicy(link, file)
	if link.Concurrency != defaultDownloadPartConcurrency {
		t.Fatalf("expected part concurrency=%d, got=%d", defaultDownloadPartConcurrency, link.Concurrency)
	}
	if link.PartSize != 40*int(utils.MB) {
		t.Fatalf("expected part size=%d, got=%d", 40*int(utils.MB), link.PartSize)
	}
	rc, ok := link.RangeReadCloser.(*baiduRangeReadCloser)
	if !ok {
		t.Fatalf("unexpected range read closer type: %T", link.RangeReadCloser)
	}
	if rc.limiter != nil {
		t.Fatalf("expected interactive requests to bypass task limiter")
	}
	if rc.cache == nil || rc.cacheFileKey == "" {
		t.Fatalf("expected interactive requests to use fixed segment cache")
	}

	link = &model.Link{
		URL: "https://example.com/file",
	}
	d.applyDownloadPolicy(link, file)
	rc, ok = link.RangeReadCloser.(*baiduRangeReadCloser)
	if !ok {
		t.Fatalf("unexpected range read closer type: %T", link.RangeReadCloser)
	}
	if rc.limiter != backgroundLimiter {
		t.Fatalf("expected background limiter to be used")
	}
	if rc.cache == nil || rc.cacheFileKey == "" {
		t.Fatalf("expected background requests to use fixed segment cache")
	}
}
