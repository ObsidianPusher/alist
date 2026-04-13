import { Center, Icon, Text, VStack } from "@hope-ui/solid"
import { IoImageOutline } from "solid-icons/io"
import { createEffect } from "solid-js"
import { notify } from "~/utils"

const seen = new Set<string>()
let windowStart = 0
let windowCount = 0
const MAX_NOTIFY_PER_WINDOW = 3
const WINDOW_MS = 20000

const shouldNotify = () => {
  const now = Date.now()
  if (now-windowStart > WINDOW_MS) {
    windowStart = now
    windowCount = 0
  }
  if (windowCount >= MAX_NOTIFY_PER_WINDOW) return false
  windowCount += 1
  return true
}

type ThumbDiag = {
  status?: number
  message?: string
  traceId?: string
  shouldNotify?: boolean
}

const diagnoseThumb = async (src: string): Promise<ThumbDiag> => {
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), 8000)
  try {
    const resp = await fetch(src, {
      method: "HEAD",
      credentials: "include",
      signal: ctrl.signal,
    })
    const traceId = resp.headers.get("X-Alist-Trace-Id") || undefined
    const ct = (resp.headers.get("Content-Type") || "").toLowerCase()
    if (resp.ok) {
      // Successful image responses (including 206/HEAD) are not actionable UI errors.
      if (ct.includes("image/")) {
        return { status: resp.status, traceId, message: "ok-image-response", shouldNotify: false }
      }
      return {
        status: resp.status,
        traceId,
        message: `invalid content-type: ${ct || "unknown"}`,
        shouldNotify: true,
      }
    }
    let bodyMsg = ""
    try {
      const body = await resp.clone().json()
      bodyMsg = body?.message || body?.data?.reason || ""
    } catch {
      bodyMsg = await resp.text()
    }
    return {
      status: resp.status,
      traceId,
      message: bodyMsg || "thumbnail request failed",
      shouldNotify: true,
    }
  } catch (e: any) {
    const msg = String(e?.message || "thumbnail request failed")
    if (msg.toLowerCase().includes("failed to fetch")) {
      return { message: "network/cors blocked or remote url expired", shouldNotify: true }
    }
    return { message: msg, shouldNotify: true }
  } finally {
    clearTimeout(timer)
  }
}

export const ThumbLoadError = (props: { src: string; name: string; report?: boolean }) => {
  createEffect(() => {
    if (props.report === false) return
    const src = props.src?.trim()
    if (!src || seen.has(src)) return
    if (!shouldNotify()) return
    seen.add(src)
    void diagnoseThumb(src).then((d) => {
      if (!d.shouldNotify) {
        console.warn("[thumb] non-actionable failure", { name: props.name, src, ...d })
        return
      }
      const parts = [`缩略图加载失败: ${props.name}`]
      if (d.status) parts.push(`status=${d.status}`)
      if (d.traceId) parts.push(`trace_id=${d.traceId}`)
      if (d.message) parts.push(d.message.slice(0, 180))
      notify.error(parts.join(" | "))
      console.error("[thumb] load failed", { name: props.name, src, ...d })
    })
  })

  return (
    <Center w="$full" h="$full">
      <VStack spacing="$1" maxW="90%" color="$neutral10">
        <Icon as={IoImageOutline} boxSize="$6" opacity={0.7} />
        <Text fontSize="$xs" textAlign="center" css={{ wordBreak: "break-all" }}>
          {props.name}
        </Text>
      </VStack>
    </Center>
  )
}
