import type { PResp } from "~/types"
import { r } from "~/utils"

export type FsOtherPayload = {
  path: string
  password?: string
  method: string
}

export type AliyunDocPreviewData = {
  access_token: string
  preview_url: string
}

export type AliyunVideoPreviewData = {
  drive_id: string
  file_id: string
  video_preview_play_info: {
    category: string
    live_transcoding_task_list: Array<{
      stage: string
      status: string
      template_height: number
      template_id: string
      template_name: string
      template_width: number
      url: string
    }>
    meta: {
      duration: number
      height: number
      width: number
    }
  }
}

export const callFsOther = <T>(data: FsOtherPayload): PResp<T> => {
  return r.post("/fs/other", data)
}

export const getAliyunDocPreview = (path: string, filePassword: string): PResp<AliyunDocPreviewData> => {
  return callFsOther<AliyunDocPreviewData>({
    path,
    password: filePassword,
    method: "doc_preview",
  })
}

export const getAliyunVideoPreview = (path: string, filePassword: string): PResp<AliyunVideoPreviewData> => {
  return callFsOther<AliyunVideoPreviewData>({
    path,
    password: filePassword,
    method: "video_preview",
  })
}
