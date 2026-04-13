import flvjs from "flv.js"
import Hls from "hls.js"

export const parseAutoFullscreen = (value?: string): boolean => {
  return value === "true"
}

export const createMediaSourceController = () => {
  let hls: Hls | null = null
  let flv: ReturnType<typeof flvjs.createPlayer> | null = null

  const cleanup = () => {
    if (hls) {
      hls.destroy()
      hls = null
    }
    if (flv) {
      try {
        flv.pause()
      } catch {
        // no-op
      }
      flv.destroy()
      flv = null
    }
  }

  const bindFlv = (video: HTMLMediaElement, url: string) => {
    cleanup()
    if (!flvjs.isSupported()) {
      video.src = url
      return
    }
    flv = flvjs.createPlayer(
      {
        type: "flv",
        url,
      },
      {
        referrerPolicy: "same-origin",
        enableWorker: true,
        enableStashBuffer: true,
        stashInitialSize: 768 * 1024,
        lazyLoad: true,
        lazyLoadMaxDuration: 90,
        lazyLoadRecoverDuration: 30,
        autoCleanupSourceBuffer: true,
        autoCleanupMaxBackwardDuration: 120,
        autoCleanupMinBackwardDuration: 30,
        fixAudioTimestampGap: true,
      },
    )
    flv.attachMediaElement(video)
    flv.load()
  }

  const bindM3u8 = (video: HTMLMediaElement, url: string) => {
    cleanup()
    if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url
      return
    }
    if (!Hls.isSupported()) {
      video.src = url
      return
    }
    hls = new Hls({
      enableWorker: true,
      lowLatencyMode: false,
      backBufferLength: 90,
      maxBufferLength: 45,
      maxMaxBufferLength: 90,
      maxBufferSize: 120 * 1000 * 1000,
      capLevelToPlayerSize: true,
      abrEwmaFastLive: 3.0,
      abrEwmaSlowLive: 9.0,
      fragLoadingTimeOut: 25000,
      fragLoadingMaxRetry: 4,
      levelLoadingMaxRetry: 4,
      manifestLoadingMaxRetry: 3,
    })
    hls.loadSource(url)
    hls.attachMedia(video)
  }

  return {
    bindFlv,
    bindM3u8,
    cleanup,
  }
}
