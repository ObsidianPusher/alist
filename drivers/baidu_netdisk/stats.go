package baidu_netdisk

import (
	"sort"
	"sync"
	"time"
)

const baiduMetricsRecentLimit = 160

type baiduTransferDirection string

const (
	baiduTransferDirectionDownload baiduTransferDirection = "download"
	baiduTransferDirectionUpload   baiduTransferDirection = "upload"
)

type baiduTransferBlock struct {
	BackendID  string
	File       string
	Offset     int64
	Size       int64
	StartedAt  time.Time
	FinishedAt time.Time
}

type BaiduMetricsSnapshot struct {
	GeneratedAt int64                  `json:"generated_at"`
	Download    BaiduTransferAggregate `json:"download"`
	Upload      BaiduTransferAggregate `json:"upload"`
	Recent      []BaiduRecentBlock     `json:"recent"`
}

type BaiduTransferAggregate struct {
	ActiveBlocks      int64   `json:"active_blocks"`
	TotalBytes        int64   `json:"total_bytes"`
	TotalBlocks       int64   `json:"total_blocks"`
	TotalDurationMs   int64   `json:"total_duration_ms"`
	AverageBlockMs    float64 `json:"average_block_ms"`
	AverageThroughput float64 `json:"average_throughput_bps"`
	Throughput1m      float64 `json:"throughput_1m_bps"`
	Throughput5m      float64 `json:"throughput_5m_bps"`
	MaxBlockMs        int64   `json:"max_block_ms"`
	LastFinishedAt    int64   `json:"last_finished_at"`
}

type BaiduRecentBlock struct {
	Direction     baiduTransferDirection `json:"direction"`
	BackendID     string                 `json:"backend_id"`
	File          string                 `json:"file"`
	Offset        int64                  `json:"offset"`
	Size          int64                  `json:"size"`
	DurationMs    int64                  `json:"duration_ms"`
	ThroughputBps float64                `json:"throughput_bps"`
	FinishedAt    int64                  `json:"finished_at"`
}

type baiduMetricsCollector struct {
	mu sync.Mutex

	active map[baiduTransferDirection]int64
	totals map[baiduTransferDirection]BaiduTransferAggregate
	recent []BaiduRecentBlock
}

var baiduMetrics = &baiduMetricsCollector{
	active: map[baiduTransferDirection]int64{
		baiduTransferDirectionDownload: 0,
		baiduTransferDirectionUpload:   0,
	},
	totals: map[baiduTransferDirection]BaiduTransferAggregate{
		baiduTransferDirectionDownload: {},
		baiduTransferDirectionUpload:   {},
	},
	recent: make([]BaiduRecentBlock, 0, baiduMetricsRecentLimit),
}

func recordBaiduTransferBlock(direction baiduTransferDirection, block baiduTransferBlock) {
	if block.Size <= 0 {
		return
	}
	finishedAt := block.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	startedAt := block.StartedAt
	if startedAt.IsZero() || finishedAt.Before(startedAt) {
		startedAt = finishedAt
	}
	duration := finishedAt.Sub(startedAt)
	if duration <= 0 {
		duration = time.Millisecond
	}
	durationMs := duration.Milliseconds()
	if durationMs <= 0 {
		durationMs = 1
	}
	recent := BaiduRecentBlock{
		Direction:     direction,
		BackendID:     block.BackendID,
		File:          block.File,
		Offset:        block.Offset,
		Size:          block.Size,
		DurationMs:    durationMs,
		ThroughputBps: float64(block.Size) / duration.Seconds(),
		FinishedAt:    finishedAt.UnixMilli(),
	}

	baiduMetrics.mu.Lock()
	defer baiduMetrics.mu.Unlock()

	total := baiduMetrics.totals[direction]
	total.TotalBytes += block.Size
	total.TotalBlocks++
	total.TotalDurationMs += durationMs
	if durationMs > total.MaxBlockMs {
		total.MaxBlockMs = durationMs
	}
	total.LastFinishedAt = recent.FinishedAt
	if total.TotalBlocks > 0 {
		total.AverageBlockMs = float64(total.TotalDurationMs) / float64(total.TotalBlocks)
	}
	if total.TotalDurationMs > 0 {
		total.AverageThroughput = float64(total.TotalBytes) / (float64(total.TotalDurationMs) / 1000)
	}
	baiduMetrics.totals[direction] = total

	baiduMetrics.recent = append(baiduMetrics.recent, recent)
	if len(baiduMetrics.recent) > baiduMetricsRecentLimit {
		baiduMetrics.recent = append([]BaiduRecentBlock(nil), baiduMetrics.recent[len(baiduMetrics.recent)-baiduMetricsRecentLimit:]...)
	}
}

func beginBaiduTransferBlock(direction baiduTransferDirection) func() {
	baiduMetrics.mu.Lock()
	baiduMetrics.active[direction]++
	baiduMetrics.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			baiduMetrics.mu.Lock()
			if baiduMetrics.active[direction] > 0 {
				baiduMetrics.active[direction]--
			}
			baiduMetrics.mu.Unlock()
		})
	}
}

func GetBaiduMetricsSnapshot() BaiduMetricsSnapshot {
	baiduMetrics.mu.Lock()
	defer baiduMetrics.mu.Unlock()

	now := time.Now().UnixMilli()
	snapshot := BaiduMetricsSnapshot{
		GeneratedAt: now,
		Download:    baiduMetrics.totals[baiduTransferDirectionDownload],
		Upload:      baiduMetrics.totals[baiduTransferDirectionUpload],
		Recent:      append([]BaiduRecentBlock(nil), baiduMetrics.recent...),
	}
	snapshot.Download.ActiveBlocks = baiduMetrics.active[baiduTransferDirectionDownload]
	snapshot.Upload.ActiveBlocks = baiduMetrics.active[baiduTransferDirectionUpload]

	var download1mBytes, upload1mBytes int64
	var download5mBytes, upload5mBytes int64
	oneMinuteAgo := now - int64(time.Minute/time.Millisecond)
	fiveMinutesAgo := now - int64((5*time.Minute)/time.Millisecond)
	for _, block := range snapshot.Recent {
		if block.FinishedAt >= oneMinuteAgo {
			if block.Direction == baiduTransferDirectionDownload {
				download1mBytes += block.Size
			} else {
				upload1mBytes += block.Size
			}
		}
		if block.FinishedAt >= fiveMinutesAgo {
			if block.Direction == baiduTransferDirectionDownload {
				download5mBytes += block.Size
			} else {
				upload5mBytes += block.Size
			}
		}
	}
	snapshot.Download.Throughput1m = float64(download1mBytes) / 60
	snapshot.Upload.Throughput1m = float64(upload1mBytes) / 60
	snapshot.Download.Throughput5m = float64(download5mBytes) / 300
	snapshot.Upload.Throughput5m = float64(upload5mBytes) / 300

	sort.Slice(snapshot.Recent, func(i, j int) bool {
		return snapshot.Recent[i].FinishedAt > snapshot.Recent[j].FinishedAt
	})
	return snapshot
}
