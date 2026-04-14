import type { PResp } from "~/types"
import { r } from "~/utils"

export const logout = (): PResp<unknown> => {
  return r.get("/auth/logout")
}
