import axios, { CancelToken } from "axios"
import {
  PEmptyResp,
  FsGetResp,
  FsListResp,
  Obj,
  PResp,
  FsSearchResp,
  RenameObj,
  VideoProgressResp,
  VideoFavoriteListResp,
  VideoFavoriteStatusResp,
  LocalBrowserStoragesResp,
  LocalBrowserListResp,
  LocalBrowserUsageResp,
  LocalBrowserDirSizesResp,
} from "~/types"
import { r } from "."

export const fsGet = (
  path: string = "/",
  password = "",
): Promise<FsGetResp> => {
  return r.post("/fs/get", {
    path: path,
    password: password,
  })
}
export const fsList = (
  path: string = "/",
  password = "",
  page = 1,
  per_page = 0,
  refresh = false,
  cancelToken?: CancelToken,
): Promise<FsListResp> => {
  return r.post(
    "/fs/list",
    {
      path,
      password,
      page,
      per_page,
      refresh,
    },
    {
      cancelToken: cancelToken,
    },
  )
}

export const fsDirs = (
  path = "/",
  password = "",
  forceRoot = false,
): PResp<Obj[]> => {
  return r.post("/fs/dirs", { path, password, force_root: forceRoot })
}

export const fsMkdir = (path: string): PEmptyResp => {
  return r.post("/fs/mkdir", { path })
}

export const fsRename = (path: string, name: string): PEmptyResp => {
  return r.post("/fs/rename", { path, name })
}

export const fsBatchRename = (
  src_dir: string,
  rename_objects: RenameObj[],
): PEmptyResp => {
  return r.post("/fs/batch_rename", { src_dir, rename_objects })
}

export const fsMove = (
  src_dir: string,
  dst_dir: string,
  names: string[],
): PEmptyResp => {
  return r.post("/fs/move", { src_dir, dst_dir, names })
}

export const fsRecursiveMove = (
  src_dir: string,
  dst_dir: string,
): PEmptyResp => {
  return r.post("/fs/recursive_move", { src_dir, dst_dir })
}

export const fsCopy = (
  src_dir: string,
  dst_dir: string,
  names: string[],
): PEmptyResp => {
  return r.post("/fs/copy", { src_dir, dst_dir, names })
}

export const fsRemove = (dir: string, names: string[]): PEmptyResp => {
  return r.post("/fs/remove", { dir, names })
}

export const fsRemoveEmptyDirectory = (src_dir: string): PEmptyResp => {
  return r.post("/fs/remove_empty_directory", { src_dir })
}

export const fsNewFile = (path: string, password: string): PEmptyResp => {
  return r.put("/fs/put", undefined, {
    headers: {
      "File-Path": encodeURIComponent(path),
      Password: password,
    },
  })
}

export const fetchText = async (
  url: string,
  ts = true,
): Promise<{
  content: ArrayBuffer | string
  contentType?: string
}> => {
  try {
    const resp = await axios.get(url, {
      responseType: "blob",
      params: ts
        ? {
            alist_ts: new Date().getTime(),
          }
        : undefined,
    })
    const content = await resp.data.arrayBuffer()
    const contentType = resp.headers["content-type"]
    return { content, contentType }
  } catch (e) {
    return ts
      ? await fetchText(url, false)
      : {
          content: `Failed to fetch ${url}: ${e}`,
          contentType: "",
        }
  }
}

export const fsSearch = async (
  parent: string,
  keywords: string,
  password = "",
  scope = 0,
  page = 1,
  per_page = 100,
): Promise<FsSearchResp> => {
  return r.post("/fs/search", {
    parent,
    keywords,
    scope,
    page,
    per_page,
    password,
  })
}

export const getVideoProgress = (path: string): Promise<VideoProgressResp> => {
  return r.get("/video/progress", { params: { path } })
}

export const saveVideoProgress = (data: {
  path: string
  file_id?: string
  size?: number
  mod_time?: number
  position: number
  duration: number
}): Promise<VideoProgressResp> => {
  return r.post("/video/progress", data)
}

export const getVideoFavoriteStatus = (
  path: string,
): Promise<VideoFavoriteStatusResp> => {
  return r.get("/video/favorite/status", { params: { path } })
}

export const toggleVideoFavorite = (data: {
  path: string
  favorite?: boolean
}): Promise<VideoFavoriteStatusResp> => {
  return r.post("/video/favorite/toggle", data)
}

export const listVideoFavorites = (data: {
  page?: number
  per_page?: number
  storage_id?: number
}): Promise<VideoFavoriteListResp> => {
  return r.post("/video/favorite/list", data)
}

export const batchCopyVideoFavorites = (data: {
  ids: number[]
  dst_dir: string
}) => {
  return r.post("/video/favorite/batch_copy", data)
}

export const batchRemoveVideoFavorites = (ids: number[]) => {
  return r.post("/video/favorite/batch_remove", { ids })
}

export const localBrowserStorages = (): LocalBrowserStoragesResp => {
  return r.post("/fs/local_browser/storages")
}

export const localBrowserList = (data: {
  path: string
  keyword?: string
  kind?: string
  recursive?: boolean
  page?: number
  per_page?: number
}): LocalBrowserListResp => {
  return r.post("/fs/local_browser/list", data)
}

export const localBrowserUsage = (data: {
  path: string
}): LocalBrowserUsageResp => {
  return r.post("/fs/local_browser/usage", data)
}

export const localBrowserDirSizes = (data: {
  paths: string[]
}): LocalBrowserDirSizesResp => {
  return r.post("/fs/local_browser/dir_sizes", data)
}

export const localBrowserCopy = (data: {
  paths: string[]
  dst_dir: string
}) => {
  return r.post("/fs/local_browser/copy", data)
}

export const localBrowserMove = (data: {
  paths: string[]
  dst_dir: string
}) => {
  return r.post("/fs/local_browser/move", data)
}

export const localBrowserRemove = (data: {
  paths: string[]
}) => {
  return r.post("/fs/local_browser/remove", data)
}

export const localBrowserCompress = (data: {
  dir: string
  names: string[]
  archive_name?: string
  password?: string
}) => {
  return r.post("/fs/local_browser/compress", data)
}

export const localBrowserExtract = (data: {
  archive_path: string
  archive_paths?: string[]
  dst_dir?: string
  password?: string
}) => {
  return r.post("/fs/local_browser/extract", data, {
    timeout: 20000,
  })
}

export const buildIndex = async (paths = ["/"], max_depth = -1): PEmptyResp => {
  return r.post("/admin/index/build", {
    paths,
    max_depth,
  })
}

export const updateIndex = async (paths = [], max_depth = -1): PEmptyResp => {
  return r.post("/admin/index/update", {
    paths,
    max_depth,
  })
}
