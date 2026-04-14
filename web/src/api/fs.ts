import axios, { Canceler, CancelToken } from "axios"
import type { FsGetResp, FsListResp, FsSearchResp, Obj, PResp } from "~/types"
import { r } from "~/utils"

export type FsListParams = {
  path?: string
  password?: string
  page?: number
  perPage?: number
  refresh?: boolean
  cancelToken?: CancelToken
}

export const getFsObject = (
  path: string = "/",
  password = "",
): Promise<FsGetResp> => {
  return r.post("/fs/get", {
    path,
    password,
  })
}

export const listFsObjects = ({
  path = "/",
  password = "",
  page = 1,
  perPage = 0,
  refresh = false,
  cancelToken,
}: FsListParams = {}): Promise<FsListResp> => {
  return r.post(
    "/fs/list",
    {
      path,
      password,
      page,
      per_page: perPage,
      refresh,
    },
    {
      cancelToken,
    },
  )
}

export const newFsListCancelToken = (cancel: (canceler: Canceler) => void) =>
  new axios.CancelToken((canceler) => cancel(canceler))

export const listFsDirs = (
  path = "/",
  password = "",
  forceRoot = false,
): PResp<Obj[]> => {
  return r.post("/fs/dirs", { path, password, force_root: forceRoot })
}

export const searchFsObjects = (
  parent: string,
  keywords: string,
  password = "",
  scope = 0,
  page = 1,
  perPage = 100,
): Promise<FsSearchResp> => {
  return r.post("/fs/search", {
    parent,
    keywords,
    scope,
    page,
    per_page: perPage,
    password,
  })
}
