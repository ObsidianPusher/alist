import type { CredentialCreationOptionsJSON } from "@github/webauthn-json/browser-ponyfill"
import { Obj } from "."

export interface Resp<T> {
  code: number
  message: string
  data: T
  request_id?: string
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

export type WebauthnCredential = {
  fingerprint: string
  id: string
}

export type WebauthnRegistrationSession = {
  session: string
  options: CredentialCreationOptionsJSON
}

export type Generate2FA = {
  qr: string
  secret: string
}

export type MemoryStats = {
  now: number
  limit_mb: number
  limit_bytes: number
  heap_alloc: number
  heap_inuse: number
  heap_idle: number
  heap_released: number
  heap_sys: number
  stack_inuse: number
  stack_sys: number
  mspan_inuse: number
  mspan_sys: number
  mcache_inuse: number
  mcache_sys: number
  gc_sys: number
  other_sys: number
  sys: number
  num_gc: number
  last_gc: number
  pause_total_ns: number
  next_gc: number
}

export type BaiduTransferAggregate = {
  active_blocks: number
  total_bytes: number
  total_blocks: number
  total_duration_ms: number
  average_block_ms: number
  average_throughput_bps: number
  throughput_1m_bps: number
  throughput_5m_bps: number
  max_block_ms: number
  last_finished_at: number
}

export type BaiduRecentBlock = {
  direction: "download" | "upload"
  backend_id: string
  file: string
  offset: number
  size: number
  duration_ms: number
  throughput_bps: number
  finished_at: number
}

export type BaiduMetricsSnapshot = {
  generated_at: number
  download: BaiduTransferAggregate
  upload: BaiduTransferAggregate
  recent: BaiduRecentBlock[]
}

export type LocalBrowserStoragesResp = PResp<LocalBrowserStorage[]>
export type LocalBrowserListResp = PResp<LocalBrowserList>
export type LocalBrowserUsageResp = PResp<LocalBrowserUsage>
export type LocalBrowserDirSizesResp = PResp<LocalBrowserDirSizes>
