import type { AxiosProgressEvent, AxiosRequestConfig } from "axios"
import type { EmptyResp } from "~/types"
import { r } from "~/utils"

export type UploadProgressHandler = (progressEvent: AxiosProgressEvent) => void

type UploadOptions = {
  uploadPath: string
  asTask?: boolean
  taskGroup?: string
  password?: string
  lastModified?: number
  onUploadProgress?: UploadProgressHandler
}

const buildUploadHeaders = (
  options: UploadOptions,
  contentType: string,
) => ({
  "File-Path": encodeURIComponent(options.uploadPath),
  "As-Task": options.asTask ?? false,
  ...(options.taskGroup ? { "Task-Group": options.taskGroup } : {}),
  "Content-Type": contentType,
  ...(options.lastModified ? { "Last-Modified": options.lastModified } : {}),
  ...(options.password ? { Password: options.password } : {}),
})

const buildUploadConfig = (
  options: UploadOptions,
  contentType: string,
): AxiosRequestConfig => ({
  headers: buildUploadHeaders(options, contentType),
  onUploadProgress: options.onUploadProgress,
})

export const uploadFsForm = (
  form: FormData,
  options: UploadOptions,
): Promise<EmptyResp> => {
  return r.put("/fs/form", form, buildUploadConfig(options, "multipart/form-data"))
}

export const uploadFsStream = (
  file: File,
  options: UploadOptions,
): Promise<EmptyResp> => {
  return r.put(
    "/fs/put",
    file,
    buildUploadConfig(options, file.type || "application/octet-stream"),
  )
}
