import type { BaiduMetricsSnapshot, MemoryStats, PResp } from "~/types"
import { r } from "~/utils"

export const getMemoryStats = (): PResp<MemoryStats> => {
  return r.get("/admin/system/memory")
}

export const getBaiduMetrics = (): PResp<BaiduMetricsSnapshot> => {
  return r.get("/admin/system/baidu_metrics")
}
