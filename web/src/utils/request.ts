import axios, { AxiosHeaders, AxiosResponseHeaders } from "axios"
import type { Resp } from "~/types"
import { api, log } from "."

const NETWORK_ERROR_CODE = 500
const defaultNetworkErrorMessage = "network request failed"
export const DEFAULT_REQUEST_TIMEOUT_MS = 30000
type HeaderBag = AxiosResponseHeaders | AxiosHeaders | Record<string, unknown>

const instance = axios.create({
  baseURL: api + "/api",
  timeout: DEFAULT_REQUEST_TIMEOUT_MS,
  headers: {
    "Content-Type": "application/json;charset=utf-8",
    // 'Authorization': localStorage.getItem("admin-token") || "",
  },
  withCredentials: false,
})

const getHeaderValue = (
  headers: HeaderBag | undefined,
  key: string,
) => {
  if (!headers) return undefined
  if (typeof (headers as AxiosHeaders).get === "function") {
    const value = (headers as AxiosHeaders).get(key)
    return Array.isArray(value) ? value[0] : value?.toString()
  }
  const plainHeaders = headers as Record<string, unknown>
  const matchedKey = Object.keys(plainHeaders).find(
    (headerKey) => headerKey.toLowerCase() === key.toLowerCase(),
  )
  const value = matchedKey ? plainHeaders[matchedKey] : undefined
  if (Array.isArray(value)) return String(value[0])
  if (value == null) return undefined
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
    return String(value)
  }
  if (typeof value === "object" && "toString" in value) {
    return String((value as { toString: () => string }).toString())
  }
  return undefined
}

const isApiResp = <T>(value: unknown): value is Resp<T> => {
  if (!value || typeof value !== "object") return false
  const candidate = value as Record<string, unknown>
  return (
    typeof candidate.code === "number" &&
    typeof candidate.message === "string" &&
    "data" in candidate
  )
}

const normalizeApiResp = <T>(
  data: unknown,
  requestId?: string,
  fallbackCode = 200,
): Resp<T> => {
  if (isApiResp<T>(data)) {
    return {
      ...data,
      request_id: data.request_id ?? requestId,
    }
  }
  return {
    code: fallbackCode,
    message: fallbackCode === 200 ? "success" : defaultNetworkErrorMessage,
    data: data as T,
    request_id: requestId,
  }
}

instance.interceptors.request.use(
  (config) => {
    // do something before request is sent
    return config
  },
  (error) => {
    // do something with request error
    console.error("request setup error:", error)
    return Promise.reject(error)
  },
)

// response interceptor
instance.interceptors.response.use(
  ((response: any) => {
    const resp = normalizeApiResp(response.data, getHeaderValue(response.headers, "x-request-id"))
    log(resp)
    return resp
  }) as any,
  ((error: any) => {
    console.error(error)
    const status = error?.response?.status ?? 500
    const requestId = getHeaderValue(error?.response?.headers, "x-request-id")
    const responseData = error?.response?.data as
      | { message?: string; request_id?: string }
      | undefined
    if (isApiResp(responseData)) {
      return {
        ...responseData,
        request_id: responseData.request_id ?? requestId,
      }
    }
    const message =
      responseData?.message ??
      error?.message ??
      defaultNetworkErrorMessage
    return {
      code: status || NETWORK_ERROR_CODE,
      message,
      data: null,
      request_id: requestId,
    }
  }) as any,
)

instance.defaults.headers.common["Authorization"] =
  localStorage.getItem("token") || ""

export const changeToken = (token?: string) => {
  instance.defaults.headers.common["Authorization"] = token ?? ""
  localStorage.setItem("token", token ?? "")
}

export { instance as r }
