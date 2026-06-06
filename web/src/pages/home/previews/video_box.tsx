import {
  VStack,
  HStack,
  Switch,
  Button,
  Text,
} from "@hope-ui/solid"
import { JSXElement } from "solid-js"
import { useT } from "~/hooks"
import Artplayer from "artplayer"

Artplayer.PLAYBACK_RATE = [0.5, 0.75, 1, 1.25, 1.5, 2, 3, 4]

export const AutoHeightPlugin = (player: Artplayer) => {
  const { $container, $video } = player.template
  const $videoBox = $container.parentElement!
  let raf = 0
  let resizeTimer = 0

  const applyAutoHeight = () => {
    if (player.fullscreen) return
    if (raf) cancelAnimationFrame(raf)
    raf = requestAnimationFrame(() => {
      player.autoHeight()
    })
  }

  player.on("ready", () => {
    const offsetBottom = "1.75rem" // position bottom of "More" button + padding
    $videoBox.style.maxHeight = `calc(100vh - ${$videoBox.offsetTop}px - ${offsetBottom})`
    $videoBox.style.minHeight = "320px" // min width of mobie phone
    applyAutoHeight()
  })
  // Re-calc only when viewport changes, avoid frequent in-play layout jitter.
  const onWindowResize = () => {
    if (resizeTimer) window.clearTimeout(resizeTimer)
    resizeTimer = window.setTimeout(() => {
      applyAutoHeight()
    }, 120)
  }
  window.addEventListener("resize", onWindowResize, { passive: true })
  player.on("video:loadedmetadata", () => {
    applyAutoHeight()
  })
  player.on("error", () => {
    if ($video.style.height) return
    $container.style.height = "60vh"
    $video.style.height = "100%"
  })
  player.on("destroy", () => {
    window.removeEventListener("resize", onWindowResize)
    if (resizeTimer) window.clearTimeout(resizeTimer)
    if (raf) cancelAnimationFrame(raf)
  })
}

export const VideoBox = (props: {
  playlist: { name: string }[]
  current: string
  onSelect: (name: string) => void
  children: JSXElement
  onAutoNextChange: (v: boolean) => void
  actions?: JSXElement
}) => {
  const t = useT()
  let autoNext = localStorage.getItem("video_auto_next")
  if (!autoNext) {
    autoNext = "true"
  }
  props.onAutoNextChange(autoNext === "true")
  const currentIndex = () =>
    props.playlist.findIndex((obj) => obj.name === props.current)
  const hasPrev = () => currentIndex() > 0
  const hasNext = () =>
    currentIndex() >= 0 && currentIndex() < props.playlist.length - 1
  return (
    <VStack w="$full" spacing="$2">
      {props.children}
      <HStack
        spacing="$3"
        w="$full"
        direction={{ "@initial": "column", "@md": "row" }}
        alignItems={{ "@initial": "stretch", "@md": "center" }}
        justifyContent="space-between"
      >
        <VStack w="$full" alignItems="stretch" spacing="$1">
          <Text fontSize="$xs" color="var(--hope-colors-neutral11)">
            当前目录视频
          </Text>
          <select
            value={props.current}
            onChange={(e) => props.onSelect(e.currentTarget.value)}
            style={{
              width: "100%",
              "min-height": "2.75rem",
              "border-radius": "0.75rem",
              border: "1px solid var(--hope-colors-neutral7)",
              background: "var(--hope-colors-background)",
              color: "var(--hope-colors-text)",
              padding: "0 0.875rem",
              boxShadow: "0 1px 2px rgba(15, 23, 42, 0.04)",
            }}
            aria-label="Select video in current directory"
          >
            {props.playlist.map((obj, i) => (
              <option value={obj.name} selected={obj.name === props.current}>
                {`${i + 1}. ${obj.name}`}
              </option>
            ))}
          </select>
        </VStack>
        <HStack
          spacing="$2"
          w={{ "@initial": "$full", "@md": "auto" }}
          justifyContent={{ "@initial": "space-between", "@md": "flex-end" }}
          alignItems="center"
          flexWrap="wrap"
        >
          <Button
            onClick={() => {
              const idx = currentIndex()
              if (idx <= 0) return
              props.onSelect(props.playlist[idx - 1].name)
            }}
            disabled={!hasPrev()}
            variant="outline"
            aria-label="Play previous video"
            title="上一个"
            size="sm"
          >
            上一个
          </Button>
          <Button
            onClick={() => {
              const idx = currentIndex()
              if (idx < 0 || idx >= props.playlist.length - 1) return
              props.onSelect(props.playlist[idx + 1].name)
            }}
            disabled={!hasNext()}
            variant="outline"
            aria-label="Play next video"
            title="下一个"
            size="sm"
          >
            下一个
          </Button>
          <Switch
            css={{
              whiteSpace: "nowrap",
            }}
            defaultChecked={autoNext === "true"}
            aria-label="Toggle auto play next video"
            onChange={(e: Event & { currentTarget: HTMLInputElement }) => {
              props.onAutoNextChange(e.currentTarget.checked)
              localStorage.setItem(
                "video_auto_next",
                e.currentTarget.checked.toString(),
              )
            }}
          >
            {t("home.preview.auto_next")}
          </Switch>
          {props.actions}
        </HStack>
      </HStack>
      <HStack w="$full" justifyContent="flex-end">
        <Button
          size="xs"
          variant="subtle"
          aria-label="Keyboard shortcuts hint"
          title="Space/K 播放暂停，←/→ 快退快进，↑/↓ 音量，F 全屏，M 静音，[/] 倍速"
        >
          快捷键
        </Button>
      </HStack>
    </VStack>
  )
}
