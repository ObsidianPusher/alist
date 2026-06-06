import { Alert, AlertDescription, AlertIcon, Box, Button, HStack, Text } from "@hope-ui/solid"
import { useNavigate } from "@solidjs/router"
import { createMemo, createSignal, onCleanup, onMount, Show } from "solid-js"
import { useLink, useVideoProgress } from "~/hooks"
import { getSettingBool, local, useCurrentObj, useCurrentObjs } from "~/store"
import { ObjType } from "~/types"
import { pathDir, pathJoin, trimBase } from "~/utils"
import View360, { ControlBar, EquirectProjection } from "@egjs/view360"
import "@egjs/view360/css/view360.min.css"
import "./video360.css"
import { VideoBox } from "./video_box"

const Preview = () => {
  const navigate = useNavigate()
  const { currentObjLink } = useLink()
  const currentObj = useCurrentObj()
  const objs = useCurrentObjs()
  const [autoNext, setAutoNext] = createSignal(false)
  const [err, setErr] = createSignal("")
  const autoResumeEnabled = () => local["video_auto_resume"] === "true"
  const currentPath = () => trimBase(location.pathname)
  const {
    resumeVisible,
    resumeAt,
    resumeApplied,
    setResumeApplied,
    setResumeVisible,
    loadResume,
    persistProgress,
  } = useVideoProgress(() => ({
    path: currentPath(),
    size: currentObj().size,
    modTime: Date.parse(currentObj().modified) || 0,
  }))
  let lastReport = 0
  let lastReportedPos = 0
  let viewer: View360 | undefined
  let videoEl: HTMLVideoElement | undefined

  const currentPathKey = createMemo(() =>
    currentObj().name,
  )
  const videos = createMemo(() => {
    const list = objs().filter((obj) => obj.type === ObjType.VIDEO)
    if (list.length === 0) return [currentObj()]
    return list
  })
  const currentIndex = createMemo(() =>
    videos().findIndex((f) => f.name === currentPathKey()),
  )
  const gotoIndex = (idx: number) => {
    if (idx < 0 || idx >= videos().length) return
    const target = videos()[idx]
    navigate(pathJoin(pathDir(location.pathname), target.name))
  }
  const nextVideo = () => gotoIndex(currentIndex() + 1)
  const selectVideo = (name: string) => {
    const idx = videos().findIndex((f) => f.name === name)
    gotoIndex(idx)
  }

  onMount(() => {
    const container = document.getElementById("view360-container")
    const video = document.getElementById("view360-video") as HTMLVideoElement | null
    if (!container || !video) {
      setErr("Video360 初始化失败：找不到播放器容器。")
      return
    }
    videoEl = video
    // Use local /d link for better auth/CORS compatibility in 360 renderer.
    video.src = currentObjLink(true)
    void loadResume()
    video.onended = () => {
      void persistProgress(
        Number.isFinite(video.duration) ? video.duration : video.currentTime,
        Number.isFinite(video.duration) ? video.duration : 0,
      )
      if (autoNext()) nextVideo()
    }
    video.onerror = () => {
      setErr("Video360 加载失败，请切换到普通 Video 预览。")
    }
    video.onloadedmetadata = () => {
      if (resumeApplied()) return
      const position = resumeAt()
      if (!Number.isFinite(position) || position <= 5) return
      const duration = video.duration || 0
      if (Number.isFinite(duration) && duration > 0 && position >= duration - 3) {
        return
      }
      if (autoResumeEnabled()) {
        video.currentTime = position
        setResumeApplied(true)
        setResumeVisible(false)
      } else {
        setResumeVisible(true)
      }
    }
    video.ontimeupdate = () => {
      if (video.paused) return
      const now = video.currentTime || 0
      if (!Number.isFinite(now) || now <= 1) return
      const ts = Date.now()
      if (ts - lastReport < 10000 && Math.abs(now - lastReportedPos) < 5) return
      lastReport = ts
      lastReportedPos = now
      void persistProgress(now, Number.isFinite(video.duration) ? video.duration : 0)
    }
    try {
      viewer = new View360(container, {
        projection: new EquirectProjection({
          src: video,
          video: {
            autoplay: getSettingBool("video_autoplay"),
          },
        }),
        plugins: [
          new ControlBar({
            pieView: { order: 0 },
            gyroButton: { position: "top-right", order: 1 },
            vrButton: { position: "top-right", order: 2 },
          }),
        ],
      })
    } catch (e: any) {
      setErr(`Video360 初始化失败：${e?.message || "unknown error"}`)
    }
  })

  onCleanup(() => viewer?.destroy())

  return (
    <VideoBox
      playlist={videos().map((v) => ({
        name: v.name,
      }))}
      current={currentObj().name}
      onSelect={selectVideo}
      onAutoNextChange={setAutoNext}
    >
      <Show when={err()}>
        <Alert status="warning" mb="$2">
          <AlertIcon />
          <AlertDescription>{err()}</AlertDescription>
        </Alert>
      </Show>
      <Box w="$full" h="60vh" id="video360-player" position="relative">
        {resumeVisible() && (
          <Box
            position="absolute"
            top="$3"
            left="$3"
            right="$3"
            zIndex={2}
            bg="rgba(15, 23, 42, 0.8)"
            border="1px solid rgba(148, 163, 184, 0.35)"
            color="white"
            rounded="$md"
            px="$3"
            py="$2"
            shadow="$md"
          >
            <HStack justifyContent="space-between" spacing="$3">
              <Text fontSize="$sm">Resume from {Math.floor(resumeAt())}s</Text>
              <HStack spacing="$2">
                <Button
                  size="xs"
                  colorScheme="info"
                  onClick={() => {
                    if (!videoEl) return
                    videoEl.currentTime = resumeAt()
                    setResumeApplied(true)
                    setResumeVisible(false)
                    void videoEl.play()
                  }}
                >
                  Continue
                </Button>
                <Button
                  size="xs"
                  variant="outline"
                  colorScheme="neutral"
                  onClick={() => {
                    if (!videoEl) return
                    videoEl.currentTime = 0
                    setResumeApplied(true)
                    setResumeVisible(false)
                  }}
                >
                  Start over
                </Button>
              </HStack>
            </HStack>
          </Box>
        )}
        <Box w="100%" h="100%" id="view360-container" className="view360-container">
          <canvas class="view360-canvas" />
          <video id="view360-video" playsinline style="display: none;" />
        </Box>
      </Box>
    </VideoBox>
  )
}

export default Preview
