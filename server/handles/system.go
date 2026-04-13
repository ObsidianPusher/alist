package handles

import (
	"runtime"
	"time"

	"github.com/alist-org/alist/v3/drivers/baidu_netdisk"
	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
)

type MemoryStatsResp struct {
	Now          int64  `json:"now"`
	LimitMB      int64  `json:"limit_mb"`
	LimitBytes   int64  `json:"limit_bytes"`
	HeapAlloc    uint64 `json:"heap_alloc"`
	HeapInuse    uint64 `json:"heap_inuse"`
	HeapIdle     uint64 `json:"heap_idle"`
	HeapReleased uint64 `json:"heap_released"`
	HeapSys      uint64 `json:"heap_sys"`
	StackInuse   uint64 `json:"stack_inuse"`
	StackSys     uint64 `json:"stack_sys"`
	MSpanInuse   uint64 `json:"mspan_inuse"`
	MSpanSys     uint64 `json:"mspan_sys"`
	MCacheInuse  uint64 `json:"mcache_inuse"`
	MCacheSys    uint64 `json:"mcache_sys"`
	GCSys        uint64 `json:"gc_sys"`
	OtherSys     uint64 `json:"other_sys"`
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"num_gc"`
	LastGC       uint64 `json:"last_gc"`
	PauseTotalNs uint64 `json:"pause_total_ns"`
	NextGC       uint64 `json:"next_gc"`
}

func GetMemoryStats(c *gin.Context) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	limitMB := int64(setting.GetInt(conf.ProcessMemoryLimitMB, 0))
	limitBytes := int64(0)
	if limitMB > 0 {
		limitBytes = limitMB * int64(utils.MB)
	}
	common.SuccessResp(c, MemoryStatsResp{
		Now:          time.Now().UnixMilli(),
		LimitMB:      limitMB,
		LimitBytes:   limitBytes,
		HeapAlloc:    ms.HeapAlloc,
		HeapInuse:    ms.HeapInuse,
		HeapIdle:     ms.HeapIdle,
		HeapReleased: ms.HeapReleased,
		HeapSys:      ms.HeapSys,
		StackInuse:   ms.StackInuse,
		StackSys:     ms.StackSys,
		MSpanInuse:   ms.MSpanInuse,
		MSpanSys:     ms.MSpanSys,
		MCacheInuse:  ms.MCacheInuse,
		MCacheSys:    ms.MCacheSys,
		GCSys:        ms.GCSys,
		OtherSys:     ms.OtherSys,
		Sys:          ms.Sys,
		NumGC:        ms.NumGC,
		LastGC:       ms.LastGC,
		PauseTotalNs: ms.PauseTotalNs,
		NextGC:       ms.NextGC,
	})
}

func GetBaiduMetrics(c *gin.Context) {
	common.SuccessResp(c, baidu_netdisk.GetBaiduMetricsSnapshot())
}
