import { password } from "~/store"
import { EmptyResp } from "~/types"
import { SetUpload, Upload } from "./types"
import { uploadFsStream } from "~/api/uploads"
export const StreamUpload: Upload = async (
  uploadPath: string,
  file: File,
  setUpload: SetUpload,
  asTask = false,
  taskGroup?: string,
): Promise<Error | undefined> => {
  let oldTimestamp = new Date().valueOf()
  let oldLoaded = 0
  const resp: EmptyResp = await uploadFsStream(file, {
    uploadPath,
    asTask,
    taskGroup,
    password: password(),
    lastModified: file.lastModified,
    onUploadProgress: (progressEvent) => {
      if (progressEvent.total) {
        const complete =
          ((progressEvent.loaded / progressEvent.total) * 100) | 0
        setUpload("progress", complete)

        const timestamp = new Date().valueOf()
        const duration = (timestamp - oldTimestamp) / 1000
        if (duration > 1) {
          const loaded = progressEvent.loaded - oldLoaded
          const speed = loaded / duration
          setUpload("speed", speed)

          oldTimestamp = timestamp
          oldLoaded = progressEvent.loaded
        }

        if (complete === 100) {
          setUpload("status", "backending")
        }
      }
    },
  })
  if (resp.code === 200) {
    return
  } else {
    return new Error(resp.message)
  }
}
