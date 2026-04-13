package baidu_netdisk

import (
	"testing"
	"time"
)

func TestBaiduMetricsSnapshotAggregatesRecentThroughput(t *testing.T) {
	now := time.Now()
	recordBaiduTransferBlock(baiduTransferDirectionDownload, baiduTransferBlock{
		BackendID:  "baidu:test",
		File:       "/a.bin",
		Offset:     0,
		Size:       4 * 1024,
		StartedAt:  now.Add(-3 * time.Second),
		FinishedAt: now.Add(-2 * time.Second),
	})
	recordBaiduTransferBlock(baiduTransferDirectionUpload, baiduTransferBlock{
		BackendID:  "baidu:test",
		File:       "/b.bin",
		Offset:     0,
		Size:       2 * 1024,
		StartedAt:  now.Add(-2 * time.Second),
		FinishedAt: now.Add(-time.Second),
	})

	snapshot := GetBaiduMetricsSnapshot()
	if snapshot.Download.TotalBlocks < 1 {
		t.Fatalf("expected download blocks to be recorded")
	}
	if snapshot.Upload.TotalBlocks < 1 {
		t.Fatalf("expected upload blocks to be recorded")
	}
	if snapshot.Download.Throughput1m <= 0 {
		t.Fatalf("expected positive 1m download throughput")
	}
	if snapshot.Upload.Throughput1m <= 0 {
		t.Fatalf("expected positive 1m upload throughput")
	}
	if len(snapshot.Recent) < 2 {
		t.Fatalf("expected recent blocks, got=%d", len(snapshot.Recent))
	}
}
