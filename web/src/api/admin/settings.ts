import type {
  BaiduMetricsSnapshot,
  EmptyResp,
  MemoryStats,
  PEmptyResp,
  PResp,
  SettingItem,
} from "~/types"
import { r } from "~/utils"

export const listSettingsByGroup = (group: string | number): PResp<SettingItem[]> => {
  return r.get(`/admin/setting/list?group=${group}`)
}

export const listSettingsByGroups = (groups: string | number): PResp<SettingItem[]> => {
  return r.get(`/admin/setting/list?groups=${groups}`)
}

export const listAllSettings = (): PResp<SettingItem[]> => {
  return r.get("/admin/setting/list")
}

export const saveSettings = (items: SettingItem[]): PEmptyResp => {
  return r.post("/admin/setting/save", items)
}

export const deleteSetting = (key: string): Promise<EmptyResp> => {
  return r.post(`/admin/setting/delete?key=${key}`)
}

export const resetSettingToken = (): PResp<string> => {
  return r.post("/admin/setting/reset_token")
}

export const getSystemMemoryStats = (): PResp<MemoryStats> => {
  return r.get("/admin/system/memory")
}

export const getSystemBaiduMetrics = (): PResp<BaiduMetricsSnapshot> => {
  return r.get("/admin/system/baidu_metrics")
}
