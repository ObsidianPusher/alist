import { Box, Button, HStack, SimpleGrid, Text, VStack } from "@hope-ui/solid"
import {
  createMemo,
  createSignal,
  For,
  Show,
  onCleanup,
  onMount,
} from "solid-js"
import { useFetch, useT } from "~/hooks"
import { PResp } from "~/types"
import { handleResp, r } from "~/utils"

type BaiduTransferAggregate = {
  active_blocks: number
  total_bytes: number
  total_blocks: number
  total_duration_ms: number
  average_block_ms: number
  average_throughput_bps: number
  throughput_1m_bps: number
  throughput_5m_bps: number
  max_block_ms: number
  last_finished_at: number
}

type BaiduRecentBlock = {
  direction: "download" | "upload"
  backend_id: string
  file: string
  offset: number
  size: number
  duration_ms: number
  throughput_bps: number
  finished_at: number
}

type BaiduMetricsSnapshot = {
  generated_at: number
  download: BaiduTransferAggregate
  upload: BaiduTransferAggregate
  recent: BaiduRecentBlock[]
}

const formatBytes = (bytes: number) => {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let idx = 0
  let value = bytes
  while (value >= 1024 && idx < units.length - 1) {
    value /= 1024
    idx++
  }
  return `${value.toFixed(value >= 100 || idx === 0 ? 0 : 1)} ${units[idx]}`
}

const formatSpeed = (bps: number) => {
  if (!Number.isFinite(bps) || bps <= 0) return "0 B/s"
  return `${formatBytes(bps)}/s`
}

const formatDuration = (ms: number) => {
  if (!Number.isFinite(ms) || ms <= 0) return "0 ms"
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(ms >= 10000 ? 0 : 2)} s`
}

const formatTimeAgo = (ts: number) => {
  if (!Number.isFinite(ts) || ts <= 0) return "-"
  const diff = Date.now() - ts
  if (!Number.isFinite(diff) || diff < 0) return "-"
  if (diff < 1000) return "1s"
  if (diff < 60_000) return `${Math.round(diff / 1000)}s`
  if (diff < 3_600_000) return `${Math.round(diff / 60_000)}m`
  return `${Math.round(diff / 3_600_000)}h`
}

const shortenFile = (file: string) => {
  if (!file) return "-"
  return file.length > 56 ? `...${file.slice(-56)}` : file
}

const BaiduMetrics = () => {
  const t = useT()
  const [stats, setStats] = createSignal<BaiduMetricsSnapshot>()
  const [loading, fetchStats] = useFetch(
    (): PResp<BaiduMetricsSnapshot> => r.get("/admin/system/baidu_metrics"),
  )

  const refresh = async () => {
    const resp = await fetchStats()
    handleResp(resp, setStats)
  }

  onMount(() => {
    refresh()
    const timer = setInterval(refresh, 5000)
    onCleanup(() => clearInterval(timer))
  })

  const summaryCards = createMemo(() => {
    const data = stats()
    if (!data) return []
    return [
      {
        title: t("settings.baidu_metric_download"),
        accent: "#2563eb",
        throughput: formatSpeed(data.download.throughput_1m_bps),
        secondary: t("settings.baidu_metric_throughput_5m", {
          value: formatSpeed(data.download.throughput_5m_bps),
        }),
        total: formatBytes(data.download.total_bytes),
        blocks: data.download.total_blocks,
        avgBlock: formatDuration(data.download.average_block_ms),
        maxBlock: formatDuration(data.download.max_block_ms),
        active: data.download.active_blocks,
        last: formatTimeAgo(data.download.last_finished_at),
      },
      {
        title: t("settings.baidu_metric_upload"),
        accent: "#0f766e",
        throughput: formatSpeed(data.upload.throughput_1m_bps),
        secondary: t("settings.baidu_metric_throughput_5m", {
          value: formatSpeed(data.upload.throughput_5m_bps),
        }),
        total: formatBytes(data.upload.total_bytes),
        blocks: data.upload.total_blocks,
        avgBlock: formatDuration(data.upload.average_block_ms),
        maxBlock: formatDuration(data.upload.max_block_ms),
        active: data.upload.active_blocks,
        last: formatTimeAgo(data.upload.last_finished_at),
      },
    ]
  })

  return (
    <Box
      w="$full"
      p="$4"
      borderRadius="$xl"
      border="1px solid $neutral5"
      bg="linear-gradient(145deg, rgba(37, 99, 235, 0.06), rgba(15, 118, 110, 0.02))"
      boxShadow="0 16px 44px rgba(15, 23, 42, 0.08)"
    >
      <HStack
        w="$full"
        justifyContent="space-between"
        mb="$3"
        alignItems="start"
        wrap="wrap"
      >
        <VStack alignItems="start" spacing="$1">
          <Text fontSize="$lg" fontWeight="$semibold">
            {t("settings.baidu_metric_panel")}
          </Text>
          <Text fontSize="$sm" color="$neutral11">
            {t("settings.baidu_metric_panel_tip")}
          </Text>
        </VStack>
        <Button size="sm" loading={loading()} onClick={refresh}>
          {t("settings.memory_refresh")}
        </Button>
      </HStack>

      <SimpleGrid columns={{ "@initial": 1, "@lg": 2 }} gap="$4">
        <For each={summaryCards()}>
          {(card) => (
            <Box
              borderRadius="$xl"
              p="$4"
              bg="rgba(255,255,255,0.92)"
              border="1px solid $neutral4"
              boxShadow="0 10px 28px rgba(15, 23, 42, 0.06)"
            >
              <HStack justifyContent="space-between" alignItems="start" mb="$3">
                <VStack alignItems="start" spacing="$1">
                  <Text fontSize="$sm" color="$neutral11">
                    {card.title}
                  </Text>
                  <Text fontSize="$3xl" fontWeight="$bold" color={card.accent}>
                    {card.throughput}
                  </Text>
                  <Text fontSize="$sm" color="$neutral11">
                    {card.secondary}
                  </Text>
                </VStack>
                <Box
                  px="$3"
                  py="$1"
                  borderRadius="$full"
                  bg="rgba(15, 23, 42, 0.06)"
                >
                  <Text
                    fontSize="$xs"
                    fontWeight="$semibold"
                    color="$neutral11"
                  >
                    {t("settings.baidu_metric_active", {
                      value: `${card.active}`,
                    })}
                  </Text>
                </Box>
              </HStack>

              <SimpleGrid columns={{ "@initial": 2, "@sm": 4 }} gap="$3">
                <VStack alignItems="start" spacing="$1">
                  <Text fontSize="$xs" color="$neutral10">
                    {t("settings.baidu_metric_total")}
                  </Text>
                  <Text fontWeight="$semibold">{card.total}</Text>
                </VStack>
                <VStack alignItems="start" spacing="$1">
                  <Text fontSize="$xs" color="$neutral10">
                    {t("settings.baidu_metric_blocks")}
                  </Text>
                  <Text fontWeight="$semibold">{card.blocks}</Text>
                </VStack>
                <VStack alignItems="start" spacing="$1">
                  <Text fontSize="$xs" color="$neutral10">
                    {t("settings.baidu_metric_avg_block")}
                  </Text>
                  <Text fontWeight="$semibold">{card.avgBlock}</Text>
                </VStack>
                <VStack alignItems="start" spacing="$1">
                  <Text fontSize="$xs" color="$neutral10">
                    {t("settings.baidu_metric_max_block")}
                  </Text>
                  <Text fontWeight="$semibold">{card.maxBlock}</Text>
                </VStack>
              </SimpleGrid>

              <Text mt="$3" fontSize="$sm" color="$neutral11">
                {t("settings.baidu_metric_last_block", { value: card.last })}
              </Text>
            </Box>
          )}
        </For>
      </SimpleGrid>

      <Box mt="$4">
        <HStack
          justifyContent="space-between"
          mb="$2"
          alignItems="center"
          wrap="wrap"
        >
          <Text fontSize="$md" fontWeight="$semibold">
            {t("settings.baidu_metric_recent")}
          </Text>
          <Text fontSize="$sm" color="$neutral11">
            {t("settings.baidu_metric_recent_tip")}
          </Text>
        </HStack>

        <Show
          when={(stats()?.recent.length || 0) > 0}
          fallback={
            <Box
              p="$4"
              borderRadius="$xl"
              border="1px dashed $neutral6"
              bg="rgba(255,255,255,0.72)"
            >
              <Text color="$neutral11">{t("settings.baidu_metric_empty")}</Text>
            </Box>
          }
        >
          <VStack spacing="$2" alignItems="stretch">
            <For each={stats()?.recent || []}>
              {(item) => (
                <Box
                  p="$3"
                  borderRadius="$xl"
                  border="1px solid $neutral4"
                  bg="rgba(255,255,255,0.9)"
                >
                  <SimpleGrid columns={{ "@initial": 1, "@md": 5 }} gap="$3">
                    <VStack alignItems="start" spacing="$1">
                      <Text fontSize="$xs" color="$neutral10">
                        {t("settings.baidu_metric_direction")}
                      </Text>
                      <Text fontWeight="$semibold">
                        {item.direction === "download"
                          ? t("settings.baidu_metric_download")
                          : t("settings.baidu_metric_upload")}
                      </Text>
                    </VStack>
                    <VStack alignItems="start" spacing="$1">
                      <Text fontSize="$xs" color="$neutral10">
                        {t("settings.baidu_metric_file")}
                      </Text>
                      <Text
                        fontWeight="$medium"
                        css={{ "word-break": "break-all" }}
                      >
                        {shortenFile(item.file)}
                      </Text>
                    </VStack>
                    <VStack alignItems="start" spacing="$1">
                      <Text fontSize="$xs" color="$neutral10">
                        {t("settings.baidu_metric_block")}
                      </Text>
                      <Text fontWeight="$medium">
                        {formatBytes(item.size)} @ {formatBytes(item.offset)}
                      </Text>
                    </VStack>
                    <VStack alignItems="start" spacing="$1">
                      <Text fontSize="$xs" color="$neutral10">
                        {t("settings.baidu_metric_cost")}
                      </Text>
                      <Text fontWeight="$medium">
                        {formatDuration(item.duration_ms)}
                      </Text>
                    </VStack>
                    <VStack alignItems="start" spacing="$1">
                      <Text fontSize="$xs" color="$neutral10">
                        {t("settings.baidu_metric_speed")}
                      </Text>
                      <Text fontWeight="$medium">
                        {formatSpeed(item.throughput_bps)}
                      </Text>
                      <Text fontSize="$xs" color="$neutral11">
                        {t("settings.baidu_metric_last_block", {
                          value: formatTimeAgo(item.finished_at),
                        })}
                      </Text>
                    </VStack>
                  </SimpleGrid>
                </Box>
              )}
            </For>
          </VStack>
        </Show>
      </Box>
    </Box>
  )
}

export default BaiduMetrics
