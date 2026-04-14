import { Box, Button, HStack, IconButton, Text } from "@hope-ui/solid"
import { useNavigate } from "@solidjs/router"
import { createEffect, createMemo, createSignal, on, onCleanup, onMount } from "solid-js"
import { useLink, useRouter, useVideoProgress } from "~/hooks"
import {
  getSettingBool,
  local,
  useCurrentObj,
  useCurrentObjs,
  useCurrentRawUrl,
} from "~/store"
import { ObjType } from "~/types"
import { ext, pathDir, pathJoin, trimBase } from "~/utils"
import { getVideoFavoriteStatus, toggleVideoFavorite } from "~/api/media"
import { createMediaSourceController } from "./player_common"
import { VideoBox } from "./video_box"
import { AiFillHeart, AiOutlineHeart } from "solid-icons/ai"

const Preview = () => {
  const { proxyLink, rawLink } = useLink()
  const { pathname } = useRouter()
  const navigate = useNavigate()
  const mediaController = createMediaSourceController()
  const currentObj = useCurrentObj()
  const objs = useCurrentObjs()
  const rawUrl = useCurrentRawUrl()
  const [autoNext, setAutoNext] = createSignal(true)
  const [favorite, setFavorite] = createSignal(false)
  const [favoriteLoading, setFavoriteLoading] = createSignal(false)
  let videoEl: HTMLVideoElement | undefined

  const videos = createMemo(() => {
    const list = objs().filter((obj) => obj.type === ObjType.VIDEO)
    if (list.length === 0) return [currentObj()]
    return [...list].sort((a, b) =>
      a.name.localeCompare(b.name, undefined, { numeric: true }),
    )
  })
  const currentIndex = createMemo(() =>
    videos().findIndex((f) => f.name === currentObj().name),
  )
  const hasPrev = createMemo(() => currentIndex() > 0)
  const hasNext = createMemo(
    () => currentIndex() >= 0 && currentIndex() < videos().length - 1,
  )
  const gotoIndex = (idx: number) => {
    if (idx < 0 || idx >= videos().length) return
    const target = videos()[idx]
    navigate(pathJoin(pathDir(location.pathname), target.name))
  }
  const nextVideo = () => gotoIndex(currentIndex() + 1)
  const previousVideo = () => gotoIndex(currentIndex() - 1)
  const selectVideo = (name: string) => {
    const idx = videos().findIndex((f) => f.name === name)
    gotoIndex(idx)
  }

  const playbackCandidates = createMemo(() => {
    const items = [
      rawUrl(),
      rawLink(currentObj(), true),
      proxyLink(currentObj(), true),
    ]
    return Array.from(new Set(items.filter(Boolean)))
  })
  let candidateIndex = 0
  let switching = false
  let pendingSeek = -1
  let monitorTimer = 0
  let stallCounter = 0
  let lastTime = 0
  let manualSeeking = false
  const currentPlaybackURL = () =>
    playbackCandidates()[Math.max(0, candidateIndex)] || rawUrl()
  const currentPath = () => trimBase(pathname())
  const autoResumeEnabled = () => local["video_auto_resume"] === "true"
  const {
    resumeVisible,
    resumeAt,
    resumeDuration,
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

  const bindSource = (url: string) => {
    if (!videoEl) return
    const kind = ext(currentObj().name).toLowerCase()
    if (kind === "m3u8") {
      mediaController.bindM3u8(videoEl, url)
      return
    }
    if (kind === "flv") {
      mediaController.bindFlv(videoEl, url)
      return
    }
    mediaController.cleanup()
    videoEl.src = url
    videoEl.load()
  }

  const safePlay = async () => {
    if (!videoEl) return
    try {
      const ret = videoEl.play() as Promise<void> | void
      if (ret && typeof (ret as Promise<void>).catch === "function") {
        await (ret as Promise<void>).catch(() => undefined)
      }
    } catch {
      // Ignore autoplay/touch-gate errors; playback retry remains silent.
    }
  }

  const tryNextCandidate = async () => {
    if (!videoEl || switching) return false
    if (candidateIndex >= playbackCandidates().length - 1) return false
    const seekPos =
      Number.isFinite(videoEl.currentTime) && videoEl.currentTime > 0
        ? videoEl.currentTime
        : -1
    switching = true
    candidateIndex += 1
    try {
      pendingSeek = seekPos
      bindSource(currentPlaybackURL())
      await safePlay()
      stallCounter = 0
      lastTime = 0
      return true
    } finally {
      switching = false
    }
  }

  onMount(() => {
    if (!videoEl) return
    videoEl.setAttribute("playsinline", "true")
    videoEl.setAttribute("webkit-playsinline", "true")
    videoEl.setAttribute("x5-playsinline", "true")
    videoEl.setAttribute("x5-video-player-type", "h5")
    videoEl.setAttribute("x5-video-player-fullscreen", "false")
    bindSource(currentPlaybackURL())
    if (getSettingBool("video_autoplay")) {
      void safePlay()
    }

    const onCanPlay = () => {
      stallCounter = 0
    }
    const onLoadedMeta = async () => {
      if (!videoEl) return
      if (pendingSeek > 0) {
        const d = Number.isFinite(videoEl.duration) ? videoEl.duration : 0
        if (!d || pendingSeek < d-1) {
          videoEl.currentTime = pendingSeek
        }
        pendingSeek = -1
      }
      if (resumeApplied()) return
      const position = resumeAt()
      const duration = resumeDuration() || videoEl.duration || 0
      if (!Number.isFinite(position) || position <= 5) return
      if (Number.isFinite(duration) && duration > 0 && position >= duration - 3) {
        return
      }
      if (autoResumeEnabled()) {
        videoEl.currentTime = position
        setResumeApplied(true)
        setResumeVisible(false)
      } else {
        setResumeVisible(true)
      }
    }
    const onError = async () => {
      if (!videoEl || manualSeeking) return
      if (videoEl.error == null) return
      await tryNextCandidate()
    }
    const onEnded = () => {
      void persistProgress(
        Number.isFinite(videoEl?.duration || 0)
          ? (videoEl?.duration as number)
          : (videoEl?.currentTime || 0),
        Number.isFinite(videoEl?.duration || 0)
          ? (videoEl?.duration as number)
          : 0,
      )
      if (autoNext()) nextVideo()
    }
    const onTimeUpdate = () => {
      if (!videoEl) return
      if (videoEl.paused || !Number.isFinite(videoEl.currentTime)) return
      const now = videoEl.currentTime
      if (now <= 1 || Number.isNaN(now)) return
      const ts = Date.now()
      if (ts - lastReport < 10000 && Math.abs(now - lastReportedPos) < 5) return
      lastReport = ts
      lastReportedPos = now
      void persistProgress(
        now,
        Number.isFinite(videoEl.duration) ? videoEl.duration : 0,
      )
    }
    const onSeeking = () => {
      manualSeeking = true
      stallCounter = 0
    }
    const onSeeked = () => {
      manualSeeking = false
      stallCounter = 0
      lastTime = videoEl?.currentTime || 0
    }
    videoEl.addEventListener("canplay", onCanPlay)
    videoEl.addEventListener("loadeddata", onCanPlay)
    videoEl.addEventListener("loadedmetadata", onLoadedMeta)
    videoEl.addEventListener("error", onError)
    videoEl.addEventListener("seeking", onSeeking)
    videoEl.addEventListener("seeked", onSeeked)
    videoEl.addEventListener("ended", onEnded)
    videoEl.addEventListener("timeupdate", onTimeUpdate)

    const onKeyDown = (event: KeyboardEvent) => {
      if (!videoEl) return
      const target = event.target as HTMLElement | null
      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      ) {
        return
      }
      switch (event.key.toLowerCase()) {
        case " ":
        case "k":
          event.preventDefault()
          if (videoEl.paused) void safePlay()
          else videoEl.pause()
          break
        case "arrowleft":
          event.preventDefault()
          videoEl.currentTime = Math.max(0, videoEl.currentTime - 5)
          break
        case "arrowright":
          event.preventDefault()
          videoEl.currentTime = Math.min(
            Number.isFinite(videoEl.duration) ? videoEl.duration : Infinity,
            videoEl.currentTime + 5,
          )
          break
        case "j":
          event.preventDefault()
          videoEl.currentTime = Math.max(0, videoEl.currentTime - 10)
          break
        case "l":
          event.preventDefault()
          videoEl.currentTime = Math.min(
            Number.isFinite(videoEl.duration) ? videoEl.duration : Infinity,
            videoEl.currentTime + 10,
          )
          break
        case "arrowup":
          event.preventDefault()
          videoEl.volume = Math.min(1, Number((videoEl.volume + 0.05).toFixed(2)))
          break
        case "arrowdown":
          event.preventDefault()
          videoEl.volume = Math.max(0, Number((videoEl.volume - 0.05).toFixed(2)))
          break
        case "m":
          event.preventDefault()
          videoEl.muted = !videoEl.muted
          break
        case "f":
          event.preventDefault()
          if (document.fullscreenElement) {
            void document.exitFullscreen()
          } else {
            void videoEl.requestFullscreen?.()
          }
          break
        case "n":
          event.preventDefault()
          nextVideo()
          break
        case "p":
          event.preventDefault()
          previousVideo()
          break
        case "[":
          event.preventDefault()
          videoEl.playbackRate = Math.max(0.5, videoEl.playbackRate - 0.25)
          break
        case "]":
          event.preventDefault()
          videoEl.playbackRate = Math.min(4, videoEl.playbackRate + 0.25)
          break
        default:
          break
      }
    }
    window.addEventListener("keydown", onKeyDown)

    monitorTimer = window.setInterval(async () => {
      if (!videoEl || switching || videoEl.paused) return
      if (manualSeeking || videoEl.seeking) {
        stallCounter = 0
        return
      }
      if (videoEl.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) {
        stallCounter = 0
        return
      }
      const now = videoEl.currentTime || 0
      const progressing = now > lastTime + 0.01
      lastTime = now
      if (progressing) {
        stallCounter = 0
        return
      }
      stallCounter += 1
      if (stallCounter < 2) return
      const ok = await tryNextCandidate()
      if (!ok) return
    }, 3000)

    onCleanup(() => {
      window.removeEventListener("keydown", onKeyDown)
      videoEl?.removeEventListener("canplay", onCanPlay)
      videoEl?.removeEventListener("loadeddata", onCanPlay)
      videoEl?.removeEventListener("loadedmetadata", onLoadedMeta)
      videoEl?.removeEventListener("error", onError)
      videoEl?.removeEventListener("seeking", onSeeking)
      videoEl?.removeEventListener("seeked", onSeeked)
      videoEl?.removeEventListener("ended", onEnded)
      videoEl?.removeEventListener("timeupdate", onTimeUpdate)
    })
  })

  onCleanup(() => {
    if (monitorTimer) window.clearInterval(monitorTimer)
    mediaController.cleanup()
  })

  let lastReport = 0
  let lastReportedPos = 0

  createEffect(
    on(pathname, () => {
      void loadResume()
      void (async () => {
        const resp = await getVideoFavoriteStatus(currentPath())
        if (!resp || resp.code !== 200 || !resp.data) return
        setFavorite(!!resp.data.favorite)
      })()
    }),
  )

  return (
      <VideoBox
        playlist={videos().map((v) => ({ name: v.name }))}
        current={currentObj().name}
      onSelect={selectVideo}
      hasPrev={hasPrev()}
      hasNext={hasNext()}
      onPrev={previousVideo}
      onNext={nextVideo}
      onAutoNextChange={setAutoNext}
      actions={
        <IconButton
          size="sm"
          colorScheme={favorite() ? "danger" : "neutral"}
          variant={favorite() ? "solid" : "outline"}
          aria-label={favorite() ? "Unfavorite" : "Favorite"}
          icon={favorite() ? <AiFillHeart /> : <AiOutlineHeart />}
          loading={favoriteLoading()}
          onClick={async () => {
            if (favoriteLoading()) return
            setFavoriteLoading(true)
            try {
              const next = !favorite()
              const resp = await toggleVideoFavorite({
                path: currentPath(),
                favorite: next,
              })
              if (resp.code === 200) {
                setFavorite(next)
              }
            } finally {
              setFavoriteLoading(false)
            }
          }}
        />
      }
    >
      <Box
        w="$full"
        h={{ "@initial": "44dvh", "@sm": "52dvh", "@md": "60vh" }}
        minH="220px"
        maxH="78dvh"
        position="relative"
        bgColor="black"
        rounded="$md"
        overflow="hidden"
      >
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
              <Text fontSize="$sm">
                Resume from {Math.floor(resumeAt())}s
              </Text>
              <HStack spacing="$2">
                <Button
                  size="xs"
                  colorScheme="info"
                  onClick={() => {
                    if (!videoEl) return
                    videoEl.currentTime = resumeAt()
                    setResumeApplied(true)
                    setResumeVisible(false)
                    void safePlay()
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
        <video
          ref={videoEl}
          controls
          playsinline
          muted={false}
          preload="auto"
          style={{
            width: "100%",
            height: "100%",
            "object-fit": "contain",
            background: "black",
          }}
        />
      </Box>
    </VideoBox>
  )
}

export default Preview
