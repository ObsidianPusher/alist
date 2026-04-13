package op

import (
	"runtime"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/pkg/utils"
)

type MemoryPressureLevel uint8

const (
	MemoryPressureNone MemoryPressureLevel = iota
	MemoryPressureModerate
	MemoryPressureHigh
	MemoryPressureCritical
)

const (
	defaultGCPercent          = 100
	moderatePressureGCPercent = 60
	highPressureGCPercent     = 40
	criticalPressureGCPercent = 25
	memoryGovernorInterval    = 5 * time.Second
	freeOSMemoryMinInterval   = 15 * time.Second
	moderatePressureThreshold = 70
	highPressureThreshold     = 85
	criticalPressureThreshold = 95
)

type MemoryPressureSnapshot struct {
	LimitBytes   int64
	AllocBytes   uint64
	HeapInuse    uint64
	HeapIdle     uint64
	HeapReleased uint64
	Level        MemoryPressureLevel
}

type MemoryPressureHook func(snapshot MemoryPressureSnapshot)

var (
	memoryPressureHooksMu sync.RWMutex
	memoryPressureHooks   []MemoryPressureHook
	memoryGovernorOnce    sync.Once
	lastFreeOSMemoryUnix  atomic.Int64
)

func RegisterMemoryPressureHook(hook MemoryPressureHook) {
	if hook == nil {
		return
	}
	memoryPressureHooksMu.Lock()
	defer memoryPressureHooksMu.Unlock()
	memoryPressureHooks = append(memoryPressureHooks, hook)
}

func StartMemoryGovernor() {
	memoryGovernorOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(memoryGovernorInterval)
			defer ticker.Stop()
			for range ticker.C {
				EnforceProcessMemoryLimit()
			}
		}()
	})
}

func EnforceProcessMemoryLimit() {
	snapshot := ReadMemoryPressureSnapshot()
	debug.SetGCPercent(targetGCPercent(snapshot.Level))
	if snapshot.Level == MemoryPressureNone {
		return
	}
	callMemoryPressureHooks(snapshot)
	if snapshot.Level >= MemoryPressureHigh {
		runtime.GC()
		snapshot = ReadMemoryPressureSnapshot()
	}
	if snapshot.Level >= MemoryPressureCritical && shouldFreeOSMemory() {
		debug.FreeOSMemory()
		snapshot = ReadMemoryPressureSnapshot()
	}
	callMemoryPressureHooks(snapshot)
}

func ReadMemoryPressureSnapshot() MemoryPressureSnapshot {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	limitBytes := GetProcessMemoryLimitBytes()
	return MemoryPressureSnapshot{
		LimitBytes:   limitBytes,
		AllocBytes:   mem.Alloc,
		HeapInuse:    mem.HeapInuse,
		HeapIdle:     mem.HeapIdle,
		HeapReleased: mem.HeapReleased,
		Level:        classifyMemoryPressure(limitBytes, int64(mem.Alloc)),
	}
}

func GetProcessMemoryLimitBytes() int64 {
	if db.GetDb() == nil {
		return 0
	}
	item, err := GetSettingItemByKey(conf.ProcessMemoryLimitMB)
	if err != nil || item == nil {
		return 0
	}
	limitMB, err := strconv.ParseInt(item.Value, 10, 64)
	if err != nil || limitMB <= 0 {
		return 0
	}
	return limitMB * int64(utils.MB)
}

func classifyMemoryPressure(limitBytes, allocBytes int64) MemoryPressureLevel {
	if limitBytes <= 0 || allocBytes <= 0 {
		return MemoryPressureNone
	}
	usagePercent := allocBytes * 100 / limitBytes
	switch {
	case usagePercent >= criticalPressureThreshold:
		return MemoryPressureCritical
	case usagePercent >= highPressureThreshold:
		return MemoryPressureHigh
	case usagePercent >= moderatePressureThreshold:
		return MemoryPressureModerate
	default:
		return MemoryPressureNone
	}
}

func targetGCPercent(level MemoryPressureLevel) int {
	switch level {
	case MemoryPressureCritical:
		return criticalPressureGCPercent
	case MemoryPressureHigh:
		return highPressureGCPercent
	case MemoryPressureModerate:
		return moderatePressureGCPercent
	default:
		return defaultGCPercent
	}
}

func shouldFreeOSMemory() bool {
	now := time.Now().Unix()
	last := lastFreeOSMemoryUnix.Load()
	if last != 0 && now-last < int64(freeOSMemoryMinInterval/time.Second) {
		return false
	}
	lastFreeOSMemoryUnix.Store(now)
	return true
}

func callMemoryPressureHooks(snapshot MemoryPressureSnapshot) {
	memoryPressureHooksMu.RLock()
	hooks := append([]MemoryPressureHook(nil), memoryPressureHooks...)
	memoryPressureHooksMu.RUnlock()
	for _, hook := range hooks {
		func(h MemoryPressureHook) {
			defer utils.RecoverPanic("memory-pressure-hook")
			h(snapshot)
		}(hook)
	}
}
