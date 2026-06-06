import { Resp } from "~/types"
import { bus, notify } from "."

type HandleRespOptions = {
  auth?: boolean
  notifyError?: boolean
  notifySuccess?: boolean
}

const redirectToLogin = () => {
  const redirect = `${location.pathname}${location.search}${location.hash}`
  bus.emit("to", `/@login?redirect=${encodeURIComponent(redirect)}`)
}

export const handleResp = <T>(
  resp: Resp<T>,
  success?: (data: T) => void,
  fail?: (message: string, code: number) => void,
  options: HandleRespOptions = {},
) => {
  const {
    auth = true,
    notifyError = true,
    notifySuccess = false,
  } = options
  if (resp.code === 200) {
    notifySuccess && notify.success(resp.message)
    success?.(resp.data)
    return true
  } else {
    notifyError && notify.error(resp.message)
    if (auth && resp.code === 401) {
      redirectToLogin()
      return false
    }
    fail?.(resp.message, resp.code)
    return false
  }
}

export const handleRespWithoutAuth = <T>(
  resp: Resp<T>,
  success?: (data: T) => void,
  fail?: (message: string, code?: number) => void,
  notifyError: boolean = true,
) => {
  return handleResp(resp, success, fail as ((message: string, code: number) => void) | undefined, {
    auth: false,
    notifyError,
  })
}

export const handleRespWithoutNotify = <T>(
  resp: Resp<T>,
  success?: (data: T) => void,
  fail?: (message: string, code?: number) => void,
  auth: boolean = true,
) => {
  return handleResp(resp, success, fail as ((message: string, code: number) => void) | undefined, {
    auth,
    notifyError: false,
  })
}

export const handleRespWithoutAuthAndNotify = <T>(
  resp: Resp<T>,
  success?: (data: T) => void,
  fail?: (message: string, code?: number) => void,
) => {
  return handleResp(resp, success, fail as ((message: string, code: number) => void) | undefined, {
    auth: false,
    notifyError: false,
  })
}

export const handleRespWithNotifySuccess = <T>(
  resp: Resp<T>,
  success?: (data: T) => void,
  fail?: (message: string, code?: number) => void,
  auth: boolean = true,
  notifyError: boolean = true,
) => {
  return handleResp(resp, success, fail as ((message: string, code: number) => void) | undefined, {
    auth,
    notifyError,
    notifySuccess: true,
  })
}
