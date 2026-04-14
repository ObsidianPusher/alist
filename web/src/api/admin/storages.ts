import type { EmptyResp, PEmptyResp, PPageResp, PResp, Storage } from "~/types"
import { r } from "~/utils"

export type DriverInfo = {
  common: any[]
  additional: any[]
  config: {
    alert?: string
    need_ms?: boolean
  }
}

export type Drivers = Record<string, DriverInfo>

export const listStorages = (): PPageResp<Storage> => {
  return r.get("/admin/storage/list")
}

export const getStorage = (id: string | number): PResp<Storage> => {
  return r.get(`/admin/storage/get?id=${id}`)
}

export const listDrivers = (): PResp<Drivers> => {
  return r.get("/admin/driver/list")
}

export const listDriverNames = (): PResp<string[]> => {
  return r.get("/admin/driver/names")
}

export const getDriverInfo = (driver: string): PResp<DriverInfo> => {
  return r.get(`/admin/driver/info?driver=${driver}`)
}

export const createStorage = (storage: Storage): PResp<{ id: number }> => {
  return r.post("/admin/storage/create", storage)
}

export const updateStorage = (storage: Storage): PResp<{ id: number }> => {
  return r.post("/admin/storage/update", storage)
}

export const saveStorage = (
  id: string | number | undefined,
  storage: Storage,
): PResp<{ id: number }> => {
  return id ? updateStorage(storage) : createStorage(storage)
}

export const deleteStorage = (id: number): PEmptyResp => {
  return r.post(`/admin/storage/delete?id=${id}`)
}

export const toggleStorage = (
  id: number,
  disabled: boolean,
): PEmptyResp => {
  return r.post(`/admin/storage/${disabled ? "enable" : "disable"}?id=${id}`)
}

export const loadAllStorages = (): Promise<EmptyResp> => {
  return r.post("/admin/storage/load_all")
}
