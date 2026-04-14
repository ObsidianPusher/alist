import type { EmptyResp, PEmptyResp, PResp } from "~/types"
import { r } from "~/utils"

export type IndexProgress = {
  obj_count: number
  is_done: boolean
  last_done_time: string
  error: string
}

export const getIndexProgress = (): PResp<IndexProgress> => {
  return r.get("/admin/index/progress")
}

export const buildIndex = (paths = ["/"], maxDepth = -1): PEmptyResp => {
  return r.post("/admin/index/build", {
    paths,
    max_depth: maxDepth,
  })
}

export const updateIndex = (paths = ["/"], maxDepth = -1): PEmptyResp => {
  return r.post("/admin/index/update", {
    paths,
    max_depth: maxDepth,
  })
}

export const clearIndex = (): PEmptyResp => {
  return r.post("/admin/index/clear")
}

export const stopIndexBuild = (): PEmptyResp => {
  return r.post("/admin/index/stop")
}

export const clearIndexResp = (): Promise<EmptyResp> => {
  return clearIndex()
}
