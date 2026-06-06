import { Box, Button, HStack, IconButton, Text } from "@hope-ui/solid"
import { useNavigate } from "@solidjs/router"
import {
  createEffect,
  createMemo,
  createSignal,
  on,
  onCleanup,
  onMount,
} from "solid-js"
import { useFetch, useLink, useRouter, useVideoProgress } from "~/hooks"
import {
  getSettingBool,
  local,
  password,
  useCurrentObj,
  useCurrentRawUrl,
  useCurrentRelated,
} from "~/store"
import { ObjType } from "~/types"
import { ext, pathDir, pathJoin } from "~/utils"
import { getVideoFavoriteStatus, toggleVideoFavorite } from "~/api/media"
import { createMediaSourceController } from "./player_common"
import { VideoBox } from "./video_box"
import {
  AiFillHeart,
  AiOutlineFullscreen,
  AiOutlineFullscreenExit,
  AiOutlineHeart,
} from "solid-icons/ai"
import {
  FiPause,
  FiPlay,
  FiSkipBack,
  FiSkipForward,
  FiVolume2,
  FiVolumeX,
} from "solid-icons/fi"
import SubtitlesOctopus from "libass-wasm"
import workerUrl from "libass-wasm/dist/js/subtitles-octopus-worker.js?url"
import wasmUrl from "libass-wasm/dist/js/subtitles-octopus-worker.wasm?url"
import TimesNewRomanFont from "~/components/artplayer-plugin-ass/fonts/TimesNewRoman.ttf?url"
import fallbackFont from "~/components/artplayer-plugin-ass/fonts/SourceHanSansCN-Bold.woff2?url"
import { listFsObjects } from "~/api/fs"

let subtitleInstance: SubtitlesOctopus | null = null
let subtitleWorkerPromise: Promise<string> | null = null
let subtitleResizeObserver: ResizeObserver | null = null
let subtitleResizeRaf = 0
let controlsHideTimer = 0

const isAbsoluteUrl = (url: string) => /^https?:\/\//.test(url)

const toAbsoluteUrl = (url: string) => {
  if (isAbsoluteUrl(url)) return url
  return new URL(url, document.baseURI).toString()
}

const loadWorker = ({
  workerUrl,
  wasmUrl,
}: {
  workerUrl: string
  wasmUrl: string
}) => {
  return fetch(workerUrl)
    .then((res) => res.text())
    .then((text) => {
      let workerScriptContent = text

      workerScriptContent = workerScriptContent.replace(
        /wasmBinaryFile\s*=\s*"(subtitles-octopus-worker\.wasm)"/g,
        (_match, wasm) => {
          const resolvedWasmUrl = wasmUrl
            ? toAbsoluteUrl(wasmUrl)
            : new URL(wasm, toAbsoluteUrl(workerUrl)).toString()
          return `wasmBinaryFile = "${resolvedWasmUrl}"`
        },
      )

      const workerBlob = new Blob([workerScriptContent], {
        type: "text/javascript",
      })
      return URL.createObjectURL(workerBlob)
    })
}

const getSubtitleWorkerUrl = () => {
  subtitleWorkerPromise ||= loadWorker({ workerUrl, wasmUrl })
  return subtitleWorkerPromise
}

const srtToVtt = (content: string) => {
  const blocks = content
    .replace(/\r\n/g, "\n")
    .trim()
    .split(/\n{2,}/)
  const cues = blocks
    .map((block) => {
      const lines = block
        .split("\n")
        .map((line) => line.trimEnd())
        .filter((line) => line.trim() !== "")
      if (lines.length === 0) return ""
      if (/^\d+$/.test(lines[0])) {
        lines.shift()
      }
      if (lines.length === 0) return ""
      lines[0] = lines[0].replace(/,/g, ".")
      return lines.join("\n")
    })
    .filter(Boolean)
  return `WEBVTT\n\n${cues.join("\n\n")}\n`
}

const formatTime = (value: number) => {
  if (!Number.isFinite(value) || value < 0) return "0:00"
  const total = Math.floor(value)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const mm = h > 0 ? String(m).padStart(2, "0") : String(m)
  const ss = String(s).padStart(2, "0")
  return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
}

const resizeSubtitleCanvas = (
  wrapEl?: HTMLDivElement,
  videoEl?: HTMLVideoElement,
  canvasEl?: HTMLCanvasElement,
) => {
  if (!wrapEl || !canvasEl) return null
  const rect = wrapEl.getBoundingClientRect()
  if (rect.width <= 0 || rect.height <= 0) return null
  const dpr = Math.max(1, window.devicePixelRatio || 1)
  let width = rect.width
  let height = rect.height
  let left = 0
  let top = 0

  const naturalWidth = videoEl?.videoWidth || 0
  const naturalHeight = videoEl?.videoHeight || 0
  if (naturalWidth > 0 && naturalHeight > 0) {
    const videoAspect = naturalWidth / naturalHeight
    const containerAspect = rect.width / rect.height
    if (containerAspect > videoAspect) {
      height = rect.height
      width = height * videoAspect
      left = (rect.width - width) / 2
    } else {
      width = rect.width
      height = width / videoAspect
      top = (rect.height - height) / 2
    }
  }

  const pixelWidth = Math.max(1, Math.round(width * dpr))
  const pixelHeight = Math.max(1, Math.round(height * dpr))
  if (canvasEl.width !== pixelWidth) canvasEl.width = pixelWidth
  if (canvasEl.height !== pixelHeight) canvasEl.height = pixelHeight
  canvasEl.style.position = "absolute"
  canvasEl.style.left = `${Math.max(0, left)}px`
  canvasEl.style.top = `${Math.max(0, top)}px`
  canvasEl.style.width = `${Math.max(1, Math.round(width))}px`
  canvasEl.style.height = `${Math.max(1, Math.round(height))}px`
  canvasEl.style.pointerEvents = "none"
  return { width: pixelWidth, height: pixelHeight }
}

const Preview = () => {
  const { proxyLink, rawLink } = useLink()
  const { pathname } = useRouter()
  const navigate = useNavigate()
  const mediaController = createMediaSourceController()
  const currentObj = useCurrentObj()
  const rawUrl = useCurrentRawUrl()
  const related = useCurrentRelated()
  const [, fetchFolder] = useFetch((path: string) =>
    listFsObjects({
      path,
      password: password(),
      perPage: 0,
    }),
  )
  const [autoNext, setAutoNext] = createSignal(true)
  const [favorite, setFavorite] = createSignal(false)
  const [favoriteLoading, setFavoriteLoading] = createSignal(false)
  let videoEl: HTMLVideoElement | undefined
  let playerWrapEl: HTMLDivElement | undefined
  let subtitleCanvasEl: HTMLCanvasElement | undefined
  let subtitleTrackEl: HTMLTrackElement | null = null
  let subtitleTrackUrl: string | null = null
  let subtitleBindToken = 0
  const [playing, setPlaying] = createSignal(false)
  const [muted, setMuted] = createSignal(false)
  const [volume, setVolume] = createSignal(0.5)
  const [currentTime, setCurrentTime] = createSignal(0)
  const [duration, setDuration] = createSignal(0)
  const [controlsVisible, setControlsVisible] = createSignal(true)
  const [fullscreen, setFullscreen] = createSignal(false)
  const currentPath = () => pathname()
  const [videoList, setVideoList] = createSignal<{ name: string }[]>([
    {
      name: currentObj().name,
    },
  ])

  const videos = createMemo(() => {
    return videoList().length > 0
      ? videoList()
      : [
          {
            name: currentObj().name,
          },
        ]
  })
  const currentIndex = createMemo(() =>
    videos().findIndex((f) => f.name === currentObj().name),
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
  const refreshVideos = async () => {
    const dir = pathDir(currentPath()) || "/"
    const resp = await fetchFolder(dir)
    if (!resp || resp.code !== 200) {
      setVideoList([{ name: currentObj().name }])
      return
    }
    const list = (resp.data?.content ?? [])
      .filter((obj) => obj.type === ObjType.VIDEO)
      .map((obj) => ({ name: obj.name }))
    const uniqueWithName: { name: string }[] = []
    const seen = new Set<string>()
    for (const item of list) {
      if (seen.has(item.name)) continue
      seen.add(item.name)
      uniqueWithName.push(item)
    }
    setVideoList(
      uniqueWithName.length > 0
        ? uniqueWithName
        : [{ name: currentObj().name }],
    )
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
  const subtitleCandidates = createMemo(() => {
    const base = currentObj()
      .name.replace(/\.[^.]+$/, "")
      .toLowerCase()
    return related()
      .filter((obj) => {
        const name = obj.name.toLowerCase()
        if (![".srt", ".ass", ".vtt"].some((suffix) => name.endsWith(suffix))) {
          return false
        }
        return (
          name === `${base}.srt` ||
          name === `${base}.ass` ||
          name === `${base}.vtt` ||
          name.startsWith(`${base}.`)
        )
      })
      .sort((a, b) => {
        const na = a.name.toLowerCase()
        const nb = b.name.toLowerCase()
        const pa =
          na === `${base}.ass` || na === `${base}.srt` || na === `${base}.vtt`
            ? 0
            : na.startsWith(`${base}.`)
              ? 1
              : 2
        const pb =
          nb === `${base}.ass` || nb === `${base}.srt` || nb === `${base}.vtt`
            ? 0
            : nb.startsWith(`${base}.`)
              ? 1
              : 2
        return (
          pa - pb || a.name.localeCompare(b.name, undefined, { numeric: true })
        )
      })
  })
  const activeSubtitle = createMemo(() => subtitleCandidates()[0] ?? null)
  const activeSubtitleKey = createMemo(() => {
    const subtitle = activeSubtitle()
    if (!subtitle) return ""
    return `${currentPath()}::${subtitle.name.toLowerCase()}`
  })
  const isAssSubtitle = createMemo(() => {
    const subtitle = activeSubtitle()
    return ext(subtitle?.name ?? "").toLowerCase() === "ass"
  })
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

  const syncMediaState = () => {
    if (!videoEl) return
    setPlaying(!videoEl.paused)
    setMuted(videoEl.muted)
    setVolume(videoEl.volume)
    setCurrentTime(videoEl.currentTime || 0)
    setDuration(Number.isFinite(videoEl.duration) ? videoEl.duration : 0)
  }

  const syncSubtitleCanvasSize = () => {
    if (!playerWrapEl || !subtitleCanvasEl) return
    if (subtitleResizeRaf) return
    subtitleResizeRaf = window.requestAnimationFrame(() => {
      subtitleResizeRaf = 0
      const size = resizeSubtitleCanvas(playerWrapEl, videoEl, subtitleCanvasEl)
      if (size && subtitleInstance) {
        subtitleInstance.resize(size.width, size.height, 0, 0)
      }
    })
  }

  const clearControlsHideTimer = () => {
    if (!controlsHideTimer) return
    window.clearTimeout(controlsHideTimer)
    controlsHideTimer = 0
  }

  const showControls = () => {
    setControlsVisible(true)
    clearControlsHideTimer()
    if (videoEl && !videoEl.paused) {
      controlsHideTimer = window.setTimeout(() => {
        setControlsVisible(false)
      }, 4200)
    }
  }

  const seekTo = (nextTime: number) => {
    if (!videoEl || !Number.isFinite(nextTime)) return
    const max = Number.isFinite(videoEl.duration) ? videoEl.duration : nextTime
    const target = Math.min(Math.max(0, nextTime), Math.max(0, max))
    videoEl.currentTime = target
    setCurrentTime(target)
  }

  const seekBy = (delta: number) => {
    if (!videoEl) return
    seekTo((videoEl.currentTime || 0) + delta)
    showControls()
  }

  const togglePlay = () => {
    if (!videoEl) return
    if (videoEl.paused) {
      void safePlay()
    } else {
      videoEl.pause()
    }
    showControls()
  }

  const toggleMute = () => {
    if (!videoEl) return
    videoEl.muted = !videoEl.muted
    setMuted(videoEl.muted)
    showControls()
  }

  const setVolumeValue = (nextVolume: number) => {
    if (!videoEl || !Number.isFinite(nextVolume)) return
    const target = Math.min(1, Math.max(0, nextVolume))
    videoEl.volume = target
    if (target > 0 && videoEl.muted) {
      videoEl.muted = false
    }
    syncMediaState()
    showControls()
  }

  const toggleFullscreen = async () => {
    if (!playerWrapEl) return
    try {
      if (document.fullscreenElement === playerWrapEl) {
        await document.exitFullscreen()
      } else {
        await playerWrapEl.requestFullscreen?.()
      }
    } catch {
      // Ignore fullscreen failures; player remains usable inline.
    }
  }

  const clearSubtitle = () => {
    subtitleBindToken += 1
    if (subtitleTrackEl) {
      subtitleTrackEl.remove()
      subtitleTrackEl = null
    }
    if (subtitleTrackUrl) {
      URL.revokeObjectURL(subtitleTrackUrl)
      subtitleTrackUrl = null
    }
    if (subtitleInstance) {
      subtitleInstance.dispose()
      subtitleInstance = null
    }
  }

  const bindSubtitle = async (
    subtitle: Exclude<ReturnType<typeof activeSubtitle>, null>,
  ) => {
    if (!videoEl) return
    clearSubtitle()
    const bindToken = subtitleBindToken

    try {
      const subtitleUrl = proxyLink(subtitle, true)
      const subtitleExt = ext(subtitle.name).toLowerCase()

      if (subtitleExt === "ass") {
        if (bindToken !== subtitleBindToken) return
        syncSubtitleCanvasSize()
        subtitleInstance = new SubtitlesOctopus({
          availableFonts: {
            "times new roman": toAbsoluteUrl(TimesNewRomanFont),
          },
          workerUrl: await getSubtitleWorkerUrl(),
          fallbackFont: toAbsoluteUrl(fallbackFont),
          video: videoEl,
          canvas: subtitleCanvasEl,
          subUrl: subtitleUrl,
        })
        syncSubtitleCanvasSize()
        return
      }

      if (subtitleExt === "srt") {
        const resp = await fetch(subtitleUrl)
        const text = await resp.text()
        if (bindToken !== subtitleBindToken) return
        subtitleTrackUrl = URL.createObjectURL(
          new Blob([srtToVtt(text)], { type: "text/vtt" }),
        )
      } else {
        subtitleTrackUrl = subtitleUrl
      }

      if (bindToken !== subtitleBindToken || !videoEl || !subtitleTrackUrl)
        return
      subtitleTrackEl = document.createElement("track")
      subtitleTrackEl.kind = "subtitles"
      subtitleTrackEl.label = subtitle.name
      subtitleTrackEl.srclang = "en"
      subtitleTrackEl.src = subtitleTrackUrl
      subtitleTrackEl.default = true
      videoEl.appendChild(subtitleTrackEl)

      window.setTimeout(() => {
        if (!videoEl) return
        const tracks = videoEl.textTracks
        if (tracks.length > 0) {
          tracks[tracks.length - 1].mode = "showing"
        }
      }, 0)
    } catch {
      clearSubtitle()
    }
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
    syncSubtitleCanvasSize()
    if (getSettingBool("video_autoplay")) {
      void safePlay()
    }

    const onCanPlay = () => {
      stallCounter = 0
    }
    const onPlay = () => {
      syncMediaState()
      showControls()
    }
    const onPause = () => {
      syncMediaState()
      clearControlsHideTimer()
      setControlsVisible(true)
    }
    const onLoadedMeta = async () => {
      if (!videoEl) return
      syncMediaState()
      if (pendingSeek > 0) {
        const d = Number.isFinite(videoEl.duration) ? videoEl.duration : 0
        if (!d || pendingSeek < d - 1) {
          videoEl.currentTime = pendingSeek
        }
        pendingSeek = -1
      }
      if (resumeApplied()) return
      const position = resumeAt()
      const duration = resumeDuration() || videoEl.duration || 0
      if (!Number.isFinite(position) || position <= 5) return
      if (
        Number.isFinite(duration) &&
        duration > 0 &&
        position >= duration - 3
      ) {
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
    const onDurationChange = () => {
      syncMediaState()
    }
    const onVolumeChange = () => {
      syncMediaState()
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
          : videoEl?.currentTime || 0,
        Number.isFinite(videoEl?.duration || 0)
          ? (videoEl?.duration as number)
          : 0,
      )
      if (autoNext()) nextVideo()
    }
    const onTimeUpdate = () => {
      if (!videoEl) return
      syncMediaState()
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
    videoEl.addEventListener("play", onPlay)
    videoEl.addEventListener("pause", onPause)
    videoEl.addEventListener("loadeddata", onCanPlay)
    videoEl.addEventListener("loadedmetadata", onLoadedMeta)
    videoEl.addEventListener("durationchange", onDurationChange)
    videoEl.addEventListener("volumechange", onVolumeChange)
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
          videoEl.volume = Math.min(
            1,
            Number((videoEl.volume + 0.05).toFixed(2)),
          )
          break
        case "arrowdown":
          event.preventDefault()
          videoEl.volume = Math.max(
            0,
            Number((videoEl.volume - 0.05).toFixed(2)),
          )
          break
        case "m":
          event.preventDefault()
          videoEl.muted = !videoEl.muted
          break
        case "f":
          event.preventDefault()
          void toggleFullscreen()
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
      showControls()
    }
    window.addEventListener("keydown", onKeyDown)

    const onFullscreenChange = () => {
      setFullscreen(document.fullscreenElement === playerWrapEl)
      syncSubtitleCanvasSize()
      window.setTimeout(syncSubtitleCanvasSize, 0)
      window.setTimeout(syncSubtitleCanvasSize, 120)
      showControls()
    }
    document.addEventListener("fullscreenchange", onFullscreenChange)

    const onWindowResize = () => {
      syncSubtitleCanvasSize()
      showControls()
    }
    window.addEventListener("resize", onWindowResize, { passive: true })

    if (playerWrapEl && "ResizeObserver" in window) {
      subtitleResizeObserver = new ResizeObserver(() => {
        syncSubtitleCanvasSize()
      })
      subtitleResizeObserver.observe(playerWrapEl)
    }

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
      window.removeEventListener("resize", onWindowResize)
      document.removeEventListener("fullscreenchange", onFullscreenChange)
      subtitleResizeObserver?.disconnect()
      subtitleResizeObserver = null
      clearControlsHideTimer()
      videoEl?.removeEventListener("canplay", onCanPlay)
      videoEl?.removeEventListener("play", onPlay)
      videoEl?.removeEventListener("pause", onPause)
      videoEl?.removeEventListener("loadeddata", onCanPlay)
      videoEl?.removeEventListener("loadedmetadata", onLoadedMeta)
      videoEl?.removeEventListener("durationchange", onDurationChange)
      videoEl?.removeEventListener("volumechange", onVolumeChange)
      videoEl?.removeEventListener("error", onError)
      videoEl?.removeEventListener("seeking", onSeeking)
      videoEl?.removeEventListener("seeked", onSeeked)
      videoEl?.removeEventListener("ended", onEnded)
      videoEl?.removeEventListener("timeupdate", onTimeUpdate)
    })
  })

  createEffect(
    on(activeSubtitleKey, () => {
      const subtitle = activeSubtitle()
      if (!subtitle) {
        clearSubtitle()
        return
      }
      void bindSubtitle(subtitle)
    }),
  )

  onCleanup(() => {
    if (monitorTimer) window.clearInterval(monitorTimer)
    if (subtitleResizeRaf) window.cancelAnimationFrame(subtitleResizeRaf)
    subtitleResizeRaf = 0
    clearSubtitle()
    mediaController.cleanup()
  })

  let lastReport = 0
  let lastReportedPos = 0

  createEffect(() => {
    pathname()
    void refreshVideos()
    void loadResume()
    void (async () => {
      const resp = await getVideoFavoriteStatus(currentPath())
      if (!resp || resp.code !== 200 || !resp.data) return
      setFavorite(!!resp.data.favorite)
    })()
  })

  return (
    <VideoBox
      playlist={videos().map((v) => ({ name: v.name }))}
      current={currentObj().name}
      onSelect={selectVideo}
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
        ref={playerWrapEl}
        w="$full"
        h={{ "@initial": "44dvh", "@sm": "52dvh", "@md": "60vh" }}
        minH="220px"
        maxH="78dvh"
        position="relative"
        bgColor="black"
        rounded="$md"
        overflow="hidden"
        onPointerMove={() => {
          showControls()
        }}
        onPointerDown={showControls}
        onTouchStart={showControls}
        onMouseMove={showControls}
      >
        <canvas
          ref={subtitleCanvasEl}
          style={{
            position: "absolute",
            inset: "0",
            width: "100%",
            height: "100%",
            "pointer-events": "none",
            "z-index": 20,
            opacity: isAssSubtitle() ? 1 : 0,
          }}
        />
        <video
          ref={videoEl}
          playsinline
          muted={false}
          preload="auto"
          style={{
            width: "100%",
            height: "100%",
            "object-fit": "contain",
            background: "black",
            display: "block",
          }}
        />
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
        <Box
          position="absolute"
          left="$3"
          right="$3"
          bottom="$3"
          zIndex={30}
          opacity={controlsVisible() || !playing() ? 1 : 0}
          pointerEvents={controlsVisible() || !playing() ? "auto" : "none"}
          transition="opacity 220ms ease, transform 220ms ease"
          transform={
            controlsVisible() || !playing()
              ? "translateY(0)"
              : "translateY(8px)"
          }
        >
          <Box
            bg="rgba(15, 23, 42, 0.72)"
            border="1px solid rgba(148, 163, 184, 0.18)"
            rounded="$xl"
            px="$3"
            py="$2"
            shadow="$2xl"
            color="white"
            style={{
              backdropFilter: "blur(18px)",
            }}
          >
            <Box mb="$2">
              <HStack spacing="$3" alignItems="center">
                <Text fontSize="$xs" color="rgba(226, 232, 240, 0.85)">
                  {formatTime(currentTime())}
                </Text>
                <input
                  type="range"
                  min="0"
                  max={Math.max(0, duration())}
                  step="0.01"
                  value={Math.min(currentTime(), duration() || currentTime())}
                  onInput={(e) => seekTo(Number(e.currentTarget.value))}
                  onPointerDown={showControls}
                  onPointerUp={showControls}
                  onPointerCancel={showControls}
                  style={{
                    width: "100%",
                    flex: "1",
                    accentColor: "var(--hope-colors-info10)",
                  }}
                />
                <Text fontSize="$xs" color="rgba(226, 232, 240, 0.85)">
                  {formatTime(duration())}
                </Text>
              </HStack>
            </Box>
            <HStack
              spacing="$2"
              alignItems="center"
              justifyContent="space-between"
              flexWrap="wrap"
            >
              <Box flex="1" />
              <HStack
                spacing="$1"
                alignItems="center"
                justifyContent="center"
                flex="0 0 auto"
                flexWrap="nowrap"
              >
                <IconButton
                  size="sm"
                  variant="outline"
                  colorScheme="neutral"
                  aria-label="Back 10 seconds"
                  title="Back 10s"
                  icon={<FiSkipBack />}
                  onClick={() => seekBy(-10)}
                />
                <IconButton
                  size="sm"
                  variant="solid"
                  colorScheme="info"
                  aria-label={playing() ? "Pause" : "Play"}
                  title={playing() ? "Pause" : "Play"}
                  icon={playing() ? <FiPause /> : <FiPlay />}
                  onClick={togglePlay}
                />
                <IconButton
                  size="sm"
                  variant="outline"
                  colorScheme="neutral"
                  aria-label="Forward 10 seconds"
                  title="Forward 10s"
                  icon={<FiSkipForward />}
                  onClick={() => seekBy(10)}
                />
              </HStack>
              <HStack spacing="$2" alignItems="center" flexWrap="wrap" flex="1" justifyContent="flex-end">
                <IconButton
                  size="sm"
                  variant="outline"
                  colorScheme="neutral"
                  aria-label={muted() ? "Unmute" : "Mute"}
                  title={muted() ? "Unmute" : "Mute"}
                  icon={muted() ? <FiVolumeX /> : <FiVolume2 />}
                  onClick={toggleMute}
                />
                <Box
                  display={{ "@initial": "none", "@md": "block" }}
                  minW="120px"
                  w="140px"
                >
                  <input
                    type="range"
                    min="0"
                    max="1"
                    step="0.01"
                    value={volume()}
                    onInput={(e) =>
                      setVolumeValue(Number(e.currentTarget.value))
                    }
                    onPointerDown={showControls}
                    onPointerUp={showControls}
                    onPointerCancel={showControls}
                    style={{
                      width: "100%",
                      accentColor: "var(--hope-colors-info10)",
                    }}
                  />
                </Box>
                <IconButton
                  size="sm"
                  variant="outline"
                  colorScheme="neutral"
                  aria-label={fullscreen() ? "Exit fullscreen" : "Fullscreen"}
                  title={fullscreen() ? "Exit fullscreen" : "Fullscreen"}
                  icon={
                    fullscreen() ? (
                      <AiOutlineFullscreenExit />
                    ) : (
                      <AiOutlineFullscreen />
                    )
                  }
                  onClick={() => void toggleFullscreen()}
                />
              </HStack>
            </HStack>
          </Box>
        </Box>
      </Box>
    </VideoBox>
  )
}

export default Preview
