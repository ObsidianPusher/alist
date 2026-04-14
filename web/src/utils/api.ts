import { CancelToken } from "axios"
import {
  PEmptyResp,
  FsGetResp,
  FsListResp,
  FsSearchResp,
  Obj,
  PResp,
} from "~/types"
import { getFsObject, listFsDirs, listFsObjects, searchFsObjects } from "~/api/fs"
import {
  createEmptyFileFs,
  mkdirFs,
} from "~/api/fs-ops"
import { fetchTextContent } from "~/api/text"
import { r } from "."

export const fsGet = (
  path: string = "/",
  password = "",
): Promise<FsGetResp> => {
  return getFsObject(path, password)
}
export const fsList = (
  path: string = "/",
  password = "",
  page = 1,
  per_page = 0,
  refresh = false,
  cancelToken?: CancelToken,
): Promise<FsListResp> => {
  return listFsObjects({
    path,
    password,
    page,
    perPage: per_page,
    refresh,
    cancelToken,
  })
}

export const fsDirs = (
  path = "/",
  password = "",
  forceRoot = false,
): PResp<Obj[]> => {
  return listFsDirs(path, password, forceRoot)
}

export const fsNewFile = (path: string, password: string): PEmptyResp => {
  return createEmptyFileFs(path, password)
}

export const fetchText = async (
  url: string,
  ts = true,
): Promise<{
  content: ArrayBuffer | string
  contentType?: string
}> => {
  return fetchTextContent(url, ts)
}

export const fsSearch = async (
  parent: string,
  keywords: string,
  password = "",
  scope = 0,
  page = 1,
  per_page = 100,
): Promise<FsSearchResp> => {
  return searchFsObjects(parent, keywords, password, scope, page, per_page)
}
