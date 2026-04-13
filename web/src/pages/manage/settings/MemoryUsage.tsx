import { Box, Button, HStack, SimpleGrid, Text, VStack } from "@hope-ui/solid"
import { createMemo, createSignal, onCleanup, onMount } from "solid-js"
import { useFetch, useT } from "~/hooks"
import { PResp } from "~/types"
import { handleResp, r } from "~/utils"

type MemoryStats = {
  now: number
  limit_mb: number
  limit_bytes: number
  heap_alloc: number
  heap_inuse: number
  heap_idle: number
  heap_released: number
  heap_sys: number
  stack_inuse: number
  stack_sys: number
  mspan_inuse: number
  mspan_sys: number
  mcache_inuse: number
  mcache_sys: number
  gc_sys: number
  other_sys: number
  sys: number
  num_gc: number
  last_gc: number
  pause_total_ns: number
  next_gc: number
}

const formatBytes = (bytes: number) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let idx = 0
  let val = bytes
  while (val >= 1024 && idx < units.length - 1) {
    val /= 1024
    idx++
  }
  return `${val.toFixed(val >= 100 || idx === 0 ? 0 : 1)} ${units[idx]}`
}

const toPercent = (value: number, total: number) => {
  if (!Number.isFinite(value) || !Number.isFinite(total) || total <= 0) return "0%"
  return `${((value / total) * 100).toFixed(1)}%`
}

const describeArc = (
  cx: number,
  cy: number,
  r: number,
  startAngle: number,
  endAngle: number,
) => {
  const toCartesian = (angle: number) => {
    const rad = ((angle - 90) * Math.PI) / 180
    return {
      x: cx + r * Math.cos(rad),
      y: cy + r * Math.sin(rad),
    }
  }
  const start = toCartesian(endAngle)
  const end = toCartesian(startAngle)
  const largeArc = endAngle - startAngle <= 180 ? "0" : "1"
  return `M ${start.x} ${start.y} A ${r} ${r} 0 ${largeArc} 0 ${end.x} ${end.y} L ${cx} ${cy} Z`
}

const MemoryUsage = () => {
  const t = useT()
  const [stats, setStats] = createSignal<MemoryStats>()
  const [prevStats, setPrevStats] = createSignal<MemoryStats>()
  const [loading, fetchStats] = useFetch(
    (): PResp<MemoryStats> => r.get("/admin/system/memory"),
  )

  const refresh = async () => {
    const resp = await fetchStats()
    handleResp(resp, (data) => {
      setPrevStats(stats())
      setStats(data)
    })
  }

  onMount(() => {
    refresh()
    const timer = setInterval(refresh, 5000)
    onCleanup(() => clearInterval(timer))
  })

  const slices = createMemo(() => {
    const data = stats()
    if (!data) return []
    const heap = data.heap_inuse
    const stack = data.stack_inuse
    const meta = data.mspan_inuse + data.mcache_inuse
    const gc = data.gc_sys
    const other = data.other_sys
    const used = heap + stack + meta + gc + other
    const total = data.limit_bytes > 0 ? data.limit_bytes : data.sys
    const free = total > used ? total - used : 0
    const idleLabel = data.limit_bytes > 0 ? t("settings.memory_free") : t("settings.memory_idle")
    return [
      { label: t("settings.memory_heap"), value: heap, color: "#2563eb" },
      { label: t("settings.memory_stack"), value: stack, color: "#10b981" },
      { label: t("settings.memory_meta"), value: meta, color: "#f59e0b" },
      { label: t("settings.memory_gc"), value: gc, color: "#8b5cf6" },
      { label: t("settings.memory_other"), value: other, color: "#64748b" },
      { label: idleLabel, value: free, color: "#e2e8f0" },
    ].filter((item) => item.value > 0)
  })

  const usedBytes = createMemo(() => {
    const data = stats()
    if (!data) return 0
    return (
      data.heap_inuse +
      data.stack_inuse +
      data.mspan_inuse +
      data.mcache_inuse +
      data.gc_sys +
      data.other_sys
    )
  })

  const totalBytes = createMemo(() => {
    const data = stats()
    if (!data) return 0
    return data.limit_bytes > 0 ? data.limit_bytes : data.sys
  })

  const gcDelta = createMemo(() => {
    const cur = stats()
    const prev = prevStats()
    if (!cur || !prev) return 0
    return Math.max(0, cur.num_gc - prev.num_gc)
  })

  const pauseDelta = createMemo(() => {
    const cur = stats()
    const prev = prevStats()
    if (!cur || !prev) return 0
    return Math.max(0, cur.pause_total_ns - prev.pause_total_ns)
  })

  const windowSeconds = createMemo(() => {
    const cur = stats()
    const prev = prevStats()
    if (!cur || !prev) return 0
    return Math.max(1, Math.round((cur.now - prev.now) / 1000))
  })

  const lastGCAgo = createMemo(() => {
    const data = stats()
    if (!data || !data.last_gc) return ""
    const lastMs = data.last_gc / 1e6
    const diff = Date.now() - lastMs
    if (!Number.isFinite(diff) || diff < 0) return ""
    if (diff < 1000) return t("settings.memory_gc_recent", { value: "1s" })
    if (diff < 60 * 1000)
      return t("settings.memory_gc_recent", {
        value: `${Math.round(diff / 1000)}s`,
      })
    return t("settings.memory_gc_recent", {
      value: `${Math.round(diff / 60000)}m`,
    })
  })

  return (
    <Box
      w="$full"
      p="$4"
      borderRadius="$xl"
      border="1px solid $neutral5"
      bg="linear-gradient(135deg, rgba(15, 23, 42, 0.03), rgba(15, 23, 42, 0.0))"
      boxShadow="0 10px 30px rgba(15, 23, 42, 0.08)"
    >
      <HStack w="$full" justifyContent="space-between" mb="$3">
        <VStack alignItems="start" spacing="$1">
          <Text fontSize="$lg" fontWeight="$semibold">
            {t("settings.memory_panel")}
          </Text>
          <Text fontSize="$sm" color="$neutral11">
            {t("settings.memory_panel_tip")}
          </Text>
        </VStack>
        <Button size="sm" loading={loading()} onClick={refresh}>
          {t("settings.memory_refresh")}
        </Button>
      </HStack>

      <SimpleGrid columns={{ "@initial": 1, "@md": 2 }} gap="$4">
        <Box
          borderRadius="$lg"
          p="$3"
          bg="white"
          border="1px solid $neutral4"
        >
          <Box w="$full" display="flex" alignItems="center" justifyContent="center">
            <svg width="220" height="220" viewBox="0 0 220 220">
              <circle cx="110" cy="110" r="96" fill="#f8fafc" />
              {(() => {
                const total = totalBytes()
                if (total <= 0) return null
                let cursor = 0
                return slices().map((slice) => {
                  const angle = (slice.value / total) * 360
                  const start = cursor
                  const end = cursor + angle
                  cursor = end
                  return (
                    <path
                      d={describeArc(110, 110, 96, start, end)}
                      fill={slice.color}
                    />
                  )
                })
              })()}
              <circle cx="110" cy="110" r="62" fill="white" />
              <text
                x="110"
                y="105"
                textAnchor="middle"
                fontSize="16"
                fontWeight="600"
                fill="#0f172a"
              >
                {formatBytes(usedBytes())}
              </text>
              <text
                x="110"
                y="126"
                textAnchor="middle"
                fontSize="12"
                fill="#64748b"
              >
                {totalBytes() > 0 ? `/${formatBytes(totalBytes())}` : ""}
              </text>
            </svg>
          </Box>
          <VStack alignItems="start" spacing="$2" mt="$3">
            {slices().map((slice) => (
              <HStack w="$full" spacing="$2" alignItems="center">
                <Box
                  w="10px"
                  h="10px"
                  borderRadius="$full"
                  bg={slice.color}
                />
                <Text fontSize="$sm" flex="1">
                  {slice.label}
                </Text>
                <Text fontSize="$sm" color="$neutral11">
                  {formatBytes(slice.value)}
                </Text>
              </HStack>
            ))}
          </VStack>
        </Box>

        <SimpleGrid columns={{ "@initial": 1, "@sm": 2 }} gap="$3">
          <Box
            borderRadius="$lg"
            p="$3"
            bg="white"
            border="1px solid $neutral4"
          >
            <Text fontSize="$sm" color="$neutral11">
              {t("settings.memory_limit")}
            </Text>
            <Text fontSize="$lg" fontWeight="$semibold">
              {stats()?.limit_bytes
                ? `${formatBytes(stats()?.limit_bytes || 0)}`
                : t("settings.memory_unlimited")}
            </Text>
            <Text fontSize="$xs" color="$neutral10">
              {t("settings.memory_limit_hint", {
                value: `${stats()?.limit_mb ?? 0}`,
              })}
            </Text>
          </Box>
          <Box
            borderRadius="$lg"
            p="$3"
            bg="white"
            border="1px solid $neutral4"
          >
            <Text fontSize="$sm" color="$neutral11">
              {t("settings.memory_used")}
            </Text>
            <Text fontSize="$lg" fontWeight="$semibold">
              {formatBytes(usedBytes())}
            </Text>
            <Text fontSize="$xs" color="$neutral10">
              {totalBytes() > 0
                ? t("settings.memory_used_ratio", {
                    value: `${toPercent(usedBytes(), totalBytes())}`,
                  })
                : t("settings.memory_used_ratio", { value: "0%" })}
            </Text>
          </Box>
          <Box
            borderRadius="$lg"
            p="$3"
            bg="white"
            border="1px solid $neutral4"
          >
            <Text fontSize="$sm" color="$neutral11">
              {t("settings.memory_heap_detail")}
            </Text>
            <Text fontSize="$lg" fontWeight="$semibold">
              {formatBytes(stats()?.heap_alloc || 0)}
            </Text>
            <Text fontSize="$xs" color="$neutral10">
              {t("settings.memory_heap_hint", {
                inuse: `${formatBytes(stats()?.heap_inuse || 0)}`,
                idle: `${formatBytes(stats()?.heap_idle || 0)}`,
              })}
            </Text>
          </Box>
          <Box
            borderRadius="$lg"
            p="$3"
            bg="white"
            border="1px solid $neutral4"
          >
            <Text fontSize="$sm" color="$neutral11">
              {t("settings.memory_gc_detail")}
            </Text>
            <Text fontSize="$lg" fontWeight="$semibold">
              {stats()?.num_gc ?? 0}
            </Text>
            <Text fontSize="$xs" color="$neutral10">
              {t("settings.memory_gc_hint", {
                pause: `${((stats()?.pause_total_ns || 0) / 1e9).toFixed(2)}`,
              })}
            </Text>
            <Text fontSize="$xs" color="$neutral10">
              {t("settings.memory_gc_window", {
                window: `${windowSeconds()}s`,
                count: `${gcDelta()}`,
                pause: `${(pauseDelta() / 1e9).toFixed(3)}`,
              })}
            </Text>
            <Text fontSize="$xs" color="$neutral10">
              {lastGCAgo() || t("settings.memory_gc_recent", { value: "-" })}
            </Text>
          </Box>
        </SimpleGrid>
      </SimpleGrid>
    </Box>
  )
}

export default MemoryUsage
