package baidu_netdisk

import (
	"context"
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

func TestBaiduNetdiskCurrentDownloadRuntimeConfigUsesConfiguredConcurrency(t *testing.T) {
	d := &BaiduNetdisk{
		Addition: Addition{
			DownloadTaskConcurrency: 6,
			DownloadPartSizeMB:      12,
		},
	}
	cfg := d.currentDownloadRuntimeConfig()
	if cfg.partConcurrency != defaultDownloadPartConcurrency {
		t.Fatalf("expected fixed part concurrency %d, got %d", defaultDownloadPartConcurrency, cfg.partConcurrency)
	}
	if cfg.partSize != 12*int(utils.MB) {
		t.Fatalf("expected part size %d, got %d", 12*int(utils.MB), cfg.partSize)
	}
}

func TestBaiduNetdiskInitKeepsFixedDownloadPartConcurrency(t *testing.T) {
	d := &BaiduNetdisk{
		Addition: Addition{
			DownloadTaskConcurrency: 6,
			DownloadPartSizeMB:      12,
		},
	}
	if err := d.Init(context.Background()); err == nil {
		t.Fatal("expected init to fail without auth, got nil")
	}
	if d.downloadPartConcurrency != defaultDownloadPartConcurrency {
		t.Fatalf("expected fixed part concurrency %d, got %d", defaultDownloadPartConcurrency, d.downloadPartConcurrency)
	}
}

func TestCloneManagedObjPreservesConcreteTypeAndMetadata(t *testing.T) {
	now := time.Unix(1710000000, 0)
	src := &model.ObjThumb{
		Object: model.Object{
			ID:       "123",
			Path:     "/src/file.txt",
			Name:     "file.txt",
			Size:     2048,
			Modified: now,
			Ctime:    now.Add(-time.Hour),
			HashInfo: utils.NewHashInfo(utils.MD5, "abc"),
		},
		Thumbnail: model.Thumbnail{Thumbnail: "thumb"},
	}

	got := cloneManagedObj(src, "/dst/file.txt", "file.txt")
	obj, ok := got.(*model.ObjThumb)
	if !ok {
		t.Fatalf("expected *model.ObjThumb, got %T", got)
	}
	if obj.Path != "/dst/file.txt" || obj.Name != "file.txt" {
		t.Fatalf("unexpected object location: path=%s name=%s", obj.Path, obj.Name)
	}
	if obj.Size != src.Size || obj.Ctime != src.Ctime {
		t.Fatalf("expected metadata to be preserved")
	}
	if thumb, ok := model.GetThumb(obj); !ok || thumb != "thumb" {
		t.Fatalf("expected thumbnail to be preserved")
	}
}

func TestCloneManagedObjSupportsPlainObject(t *testing.T) {
	now := time.Unix(1710000000, 0)
	src := &model.Object{
		ID:       "456",
		Path:     "/src/plain.txt",
		Name:     "plain.txt",
		Size:     128,
		Modified: now,
		Ctime:    now.Add(-time.Minute),
		IsFolder: false,
		HashInfo: utils.NewHashInfo(utils.MD5, "def"),
	}

	got := cloneManagedObj(src, "/dst/plain.txt", "plain.txt")
	obj, ok := got.(*model.Object)
	if !ok {
		t.Fatalf("expected *model.Object, got %T", got)
	}
	if obj.Path != "/dst/plain.txt" || obj.Name != "plain.txt" {
		t.Fatalf("unexpected cloned object location: path=%s name=%s", obj.Path, obj.Name)
	}
	if obj.ID != src.ID || obj.Size != src.Size || obj.Ctime != src.Ctime {
		t.Fatalf("expected plain object metadata to be preserved")
	}
}
