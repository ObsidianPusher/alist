import { Obj } from "."

export interface Resp<T> {
  code: number
  message: string
  data: T
}

export type PageResp<T> = Resp<{
  content: T[]
  total: number
}>

export type FsListResp = Resp<{
  content: Obj[]
  total: number
  readme: string
  header: string
  write: boolean
  provider: string
}>

export type SearchNode = {
  parent: string
  name: string
  is_dir: boolean
  size: number
  path: string
  type: number
}

export type FsSearchResp = PageResp<SearchNode>

export type FsGetResp = Resp<
  Obj & {
    raw_url: string
    readme: string
    header: string
    provider: string
    related: Obj[]
  }
>

export type EmptyResp = Resp<{}>

export type PResp<T> = Promise<Resp<T>>
export type PPageResp<T> = Promise<PageResp<T>>
export type PEmptyResp = Promise<EmptyResp>

export type VideoProgress = {
  path: string
  file_id?: string
  position: number
  duration: number
  finished: boolean
  updated_at: string
}

export type VideoProgressResp = Resp<VideoProgress | null>

export type VideoFavoriteStatus = {
  path: string
  favorite: boolean
  id?: number
  at?: string
}

export type VideoFavoriteItem = {
  id: number
  path: string
  name: string
  size: number
  modified: string
  type: number
  storage_id: number
  storage_name: string
  favorited_at: string
  progress?: number
  watched?: boolean
  last_played?: string
}

export type VideoFavoriteStorageFilter = {
  storage_id: number
  storage_name: string
}

export type VideoFavoriteList = {
  content: VideoFavoriteItem[]
  total: number
  storage_filters: VideoFavoriteStorageFilter[]
  invalid_pruned: number
  permission_prune: number
}

export type VideoFavoriteStatusResp = Resp<VideoFavoriteStatus>
export type VideoFavoriteListResp = Resp<VideoFavoriteList>

export type LocalBrowserStorage = {
  mount_path: string
  name: string
}

export type LocalBrowserEntry = {
  name: string
  path: string
  is_dir: boolean
  size: number
  modified: string
  kind: string
  extractable: boolean
  archive_format?: string
  archive_group?: string
}

export type LocalBrowserList = {
  storage_mount: string
  current_path: string
  content: LocalBrowserEntry[]
  total: number
  page: number
  per_page: number
}

export type LocalBrowserUsage = {
  path: string
  total: number
  used: number
  free: number
  used_percent: number
}

export type LocalBrowserDirSizeItem = {
  path: string
  status: string
  size: number
  updated_at?: string
}

export type LocalBrowserDirSizes = {
  items: LocalBrowserDirSizeItem[]
}

export type LocalBrowserStoragesResp = Resp<LocalBrowserStorage[]>
export type LocalBrowserListResp = Resp<LocalBrowserList>
export type LocalBrowserUsageResp = Resp<LocalBrowserUsage>
export type LocalBrowserDirSizesResp = Resp<LocalBrowserDirSizes>
