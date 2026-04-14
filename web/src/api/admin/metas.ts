import type { Meta, PEmptyResp, PPageResp, PResp } from "~/types"
import { r } from "~/utils"

export const listMetas = (): PPageResp<Meta> => {
  return r.get("/admin/meta/list")
}

export const getMeta = (id: string | number): PResp<Meta> => {
  return r.get(`/admin/meta/get?id=${id}`)
}

export const createMeta = (meta: Meta): PEmptyResp => {
  return r.post("/admin/meta/create", meta)
}

export const updateMeta = (meta: Meta): PEmptyResp => {
  return r.post("/admin/meta/update", meta)
}

export const saveMeta = (id: string | number | undefined, meta: Meta): PEmptyResp => {
  return id ? updateMeta(meta) : createMeta(meta)
}

export const deleteMeta = (id: number): PEmptyResp => {
  return r.post(`/admin/meta/delete?id=${id}`)
}
