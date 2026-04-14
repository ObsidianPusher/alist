import type { PEmptyResp } from "~/types"
import { r } from "~/utils"

export const saveFsContent = (
  path: string,
  content: string,
  contentType?: string,
): PEmptyResp => {
  return r.put("/fs/put", content, {
    headers: {
      "File-Path": encodeURIComponent(path),
      "Content-Type": contentType || "text/plain",
    },
  })
}
