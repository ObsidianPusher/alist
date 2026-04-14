import type { PEmptyResp, PResp } from "~/types"
import { r } from "~/utils"

export interface Message {
  type: string
  content: any
}

export const getMessage = (): PResp<Message> => {
  return r.post("/admin/message/get")
}

export const sendMessage = (message: string): PEmptyResp => {
  return r.post("/admin/message/send", {
    message,
  })
}
