import {
  PEmptyResp,
  PResp,
  VideoFavoriteList,
  VideoFavoriteStatus,
  VideoProgress,
} from "~/types"
import { r } from "~/utils"

export type SaveVideoProgressPayload = {
  path: string
  file_id?: string
  size?: number
  mod_time?: number
  position: number
  duration: number
}

export type ToggleVideoFavoritePayload = {
  path: string
  favorite?: boolean
}

export type ListVideoFavoritesPayload = {
  page?: number
  per_page?: number
  storage_id?: number
}

export type BatchCopyVideoFavoritesPayload = {
  ids: number[]
  dst_dir: string
}

export const getVideoProgress = (path: string): PResp<VideoProgress | null> => {
  return r.get("/video/progress", { params: { path } })
}

export const saveVideoProgress = (
  data: SaveVideoProgressPayload,
): PResp<VideoProgress | null> => {
  return r.post("/video/progress", data)
}

export const getVideoFavoriteStatus = (
  path: string,
): PResp<VideoFavoriteStatus> => {
  return r.get("/video/favorite/status", { params: { path } })
}

export const toggleVideoFavorite = (
  data: ToggleVideoFavoritePayload,
): PResp<VideoFavoriteStatus> => {
  return r.post("/video/favorite/toggle", data)
}

export const listVideoFavorites = (
  data: ListVideoFavoritesPayload,
): PResp<VideoFavoriteList> => {
  return r.post("/video/favorite/list", data)
}

export const batchCopyVideoFavorites = (
  data: BatchCopyVideoFavoritesPayload,
): PEmptyResp => {
  return r.post("/video/favorite/batch_copy", data)
}

export const batchRemoveVideoFavorites = (ids: number[]): PEmptyResp => {
  return r.post("/video/favorite/batch_remove", { ids })
}
