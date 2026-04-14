import type {
  LocalBrowserDirSizesResp,
  LocalBrowserListResp,
  LocalBrowserStoragesResp,
  LocalBrowserUsageResp,
  PEmptyResp,
} from "~/types"
import { r } from "~/utils"

export type LocalBrowserListPayload = {
  path: string
  keyword?: string
  kind?: string
  recursive?: boolean
  page?: number
  per_page?: number
}

export type LocalBrowserUsagePayload = {
  path: string
}

export type LocalBrowserDirSizesPayload = {
  paths: string[]
}

export type LocalBrowserTransferPayload = {
  paths: string[]
  dst_dir: string
}

export type LocalBrowserRemovePayload = {
  paths: string[]
}

export type LocalBrowserCompressPayload = {
  dir: string
  names: string[]
  archive_name?: string
  password?: string
}

export type LocalBrowserExtractPayload = {
  archive_path: string
  archive_paths?: string[]
  dst_dir?: string
  password?: string
}

export const localBrowserStorages = (): LocalBrowserStoragesResp => {
  return r.post("/fs/local_browser/storages") as LocalBrowserStoragesResp
}

export const localBrowserList = (
  data: LocalBrowserListPayload,
): LocalBrowserListResp => {
  return r.post("/fs/local_browser/list", data) as LocalBrowserListResp
}

export const localBrowserUsage = (
  data: LocalBrowserUsagePayload,
): LocalBrowserUsageResp => {
  return r.post("/fs/local_browser/usage", data) as LocalBrowserUsageResp
}

export const localBrowserDirSizes = (
  data: LocalBrowserDirSizesPayload,
): LocalBrowserDirSizesResp => {
  return r.post("/fs/local_browser/dir_sizes", data) as LocalBrowserDirSizesResp
}

export const localBrowserCopy = (
  data: LocalBrowserTransferPayload,
): PEmptyResp => {
  return r.post("/fs/local_browser/copy", data)
}

export const localBrowserMove = (
  data: LocalBrowserTransferPayload,
): PEmptyResp => {
  return r.post("/fs/local_browser/move", data)
}

export const localBrowserRemove = (
  data: LocalBrowserRemovePayload,
): PEmptyResp => {
  return r.post("/fs/local_browser/remove", data)
}

export const localBrowserCompress = (
  data: LocalBrowserCompressPayload,
): PEmptyResp => {
  return r.post("/fs/local_browser/compress", data)
}

export const localBrowserExtract = (
  data: LocalBrowserExtractPayload,
): PEmptyResp => {
  return r.post("/fs/local_browser/extract", data, {
    timeout: 20000,
  })
}
