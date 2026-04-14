import type { Resp } from "~/types"

export const isSuccessResp = <T>(resp: Resp<T>) => resp.code === 200

export const withDefault = <T>(value: T | null | undefined, fallback: T): T => {
  return value ?? fallback
}
