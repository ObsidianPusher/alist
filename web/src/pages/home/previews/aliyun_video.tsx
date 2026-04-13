import { Box, Button, Center, HStack, Text } from "@hope-ui/solid"
import { Show, createSignal, onCleanup, onMount } from "solid-js"
import { useRouter, useLink, useFetch } from "~/hooks"
import { getSettingBool, local, objStore, password } from "~/store"
import { ObjType, PResp } from "~/types"
import { ext, handleResp, notify, r, pathDir, pathJoin, trimBase } from "~/utils"
import { getVideoProgress, saveVideoProgress } from "~/utils/api"
import Artplayer from "artplayer"
import { type Option } from "artplayer/types/option"
import { type Setting } from "artplayer/types/setting"
import { type Events } from "artplayer/types/events"
import artplayerPluginDanmuku from "artplayer-plugin-danmuku"
import artplayerPluginAss from "~/components/artplayer-plugin-ass"
import { currentLang } from "~/app/i18n"
import { AutoHeightPlugin, VideoBox } from "./video_box"
import { ArtPlayerIconsSubtitle } from "~/components/icons"
import { useNavigate } from "@solidjs/router"
import { TiWarning } from "solid-icons/ti"
import { createMediaSourceController, parseAutoFullscreen } from "./player_common"

export interface Data {
  drive_id: string
  file_id: string
  video_preview_play_info: VideoPreviewPlayInfo
}

export interface VideoPreviewPlayInfo {
  category: string
  live_transcoding_task_list: LiveTranscodingTaskList[]
  meta: Meta
}

export interface LiveTranscodingTaskList {
  stage: string
  status: string
  template_height: number
  template_id: string
  template_name: string
  template_width: number
  url: string
}

export interface Meta {
  duration: number
  height: number
  width: number
}

const Preview = () => {
  const { pathname, searchParams } = useRouter()
  const { proxyLink } = useLink()
  const navigate = useNavigate()
  const mediaController = createMediaSourceController()
  let videos = objStore.objs.filter((obj) => obj.type === ObjType.VIDEO)
  if (videos.length === 0) {
    videos = [objStore.obj]
  }
  const next_video = () => {
    const index = videos.findIndex((f) => f.name === objStore.obj.name)
    if (index < videos.length - 1) {
      navigate(
        pathJoin(pathDir(location.pathname), videos[index + 1].name) +
          "?auto_fullscreen=" +
          player.fullscreen,
      )
    }
  }
  const previous_video = () => {
    const index = videos.findIndex((f) => f.name === objStore.obj.name)
    if (index > 0) {
      navigate(
        pathJoin(pathDir(location.pathname), videos[index - 1].name) +
          "?auto_fullscreen=" +
          player.fullscreen,
      )
    }
  }
  let player: Artplayer
  const [resumeVisible, setResumeVisible] = createSignal(false)
  const [resumeAt, setResumeAt] = createSignal(0)
  const [resumeApplied, setResumeApplied] = createSignal(false)
  const autoResumeEnabled = () => local["video_auto_resume"] === "true"
  const currentPath = () => trimBase(location.pathname)
  let lastReport = 0
  let lastReportedPos = 0
  let option: Option = {
    id: pathname(),
    container: "#video-player",
    title: objStore.obj.name,
    volume: 0.5,
    autoplay: getSettingBool("video_autoplay"),
    autoSize: false,
    autoMini: true,
    controls: [
      {
        name: "previous-button",
        index: 10,
        position: "left",
        html: '<svg fill="none" stroke-width="2" xmlns="http://www.w3.org/2000/svg" height="22" width="22" class="icon icon-tabler icon-tabler-player-track-prev-filled" width="1em" height="1em" viewBox="0 0 24 24" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" style="overflow: visible; color: currentcolor;"><path stroke="none" d="M0 0h24v24H0z" fill="none"></path><path d="M20.341 4.247l-8 7a1 1 0 0 0 0 1.506l8 7c.647 .565 1.659 .106 1.659 -.753v-14c0 -.86 -1.012 -1.318 -1.659 -.753z" stroke-width="0" fill="currentColor"></path><path d="M9.341 4.247l-8 7a1 1 0 0 0 0 1.506l8 7c.647 .565 1.659 .106 1.659 -.753v-14c0 -.86 -1.012 -1.318 -1.659 -.753z" stroke-width="0" fill="currentColor"></path></svg>',
        tooltip: "Previous",
        click: function () {
          previous_video()
        },
      },
      {
        name: "next-button",
        index: 11,
        position: "left",
        html: '<svg fill="none" stroke-width="2" xmlns="http://www.w3.org/2000/svg" height="22" width="22" class="icon icon-tabler icon-tabler-player-track-next-filled" width="1em" height="1em" viewBox="0 0 24 24" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" style="overflow: visible; color: currentcolor;"><path stroke="none" d="M0 0h24v24H0z" fill="none"></path><path d="M2 5v14c0 .86 1.012 1.318 1.659 .753l8 -7a1 1 0 0 0 0 -1.506l-8 -7c-.647 -.565 -1.659 -.106 -1.659 .753z" stroke-width="0" fill="currentColor"></path><path d="M13 5v14c0 .86 1.012 1.318 1.659 .753l8 -7a1 1 0 0 0 0 -1.506l-8 -7c-.647 -.565 -1.659 -.106 -1.659 .753z" stroke-width="0" fill="currentColor"></path></svg>',
        tooltip: "Next",
        click: function () {
          next_video()
        },
      },
    ],
    loop: false,
    flip: true,
    playbackRate: true,
    aspectRatio: true,
    setting: true,
    hotkey: true,
    pip: true,
    mutex: true,
    fullscreen: true,
    fullscreenWeb: true,
    subtitleOffset: true,
    miniProgressBar: false,
    playsInline: true,
    quality: [],
    plugins: [AutoHeightPlugin],
    whitelist: [],
    settings: [],
    moreVideoAttr: {
      // @ts-ignore
      "webkit-playsinline": true,
      playsInline: true,
    },
    type: "m3u8",
    customType: {
      m3u8: function (video: HTMLMediaElement, url: string) {
        mediaController.bindM3u8(video, url)
      },
    },
    lang: ["en", "zh-cn", "zh-tw"].includes(currentLang().toLowerCase())
      ? (currentLang().toLowerCase() as any)
      : "en",
    lock: true,
    fastForward: true,
    autoPlayback: true,
    autoOrientation: true,
    airplay: true,
  }

  const subtitle = objStore.related.filter((obj) => {
    for (const ext of [".srt", ".ass", ".vtt"]) {
      if (obj.name.endsWith(ext)) {
        return true
      }
    }
    return false
  })
  const danmu = objStore.related.find((obj) => {
    for (const ext of [".xml"]) {
      if (obj.name.endsWith(ext)) {
        return true
      }
    }
    return false
  })

  // TODO: add a switch in manage panel to choose whether to enable `libass-wasm`
  const enableEnhanceAss = true

  if (subtitle.length != 0) {
    let isEnhanceAssMode = false

    // set default subtitle
    const defaultSubtitle = subtitle[0]
    if (enableEnhanceAss && ext(defaultSubtitle.name).toLowerCase() === "ass") {
      isEnhanceAssMode = true
      option.plugins?.push(
        artplayerPluginAss({
          // debug: true,
          subUrl: proxyLink(defaultSubtitle, true),
        }),
      )
    } else {
      option.subtitle = {
        url: proxyLink(defaultSubtitle, true),
        type: ext(defaultSubtitle.name),
      }
    }

    // render subtitle toggle menu
    const innerMenu: Setting[] = [
      {
        id: "setting_subtitle_display",
        html: "Display",
        tooltip: "Show",
        switch: true,
        onSwitch: function (item: Setting) {
          item.tooltip = item.switch ? "Hide" : "Show"
          setSubtitleVisible(!item.switch)

          // sync menu subtitle tooltip
          const menu_sub = option.settings?.find(
            (_) => _.id === "setting_subtitle",
          )
          menu_sub && (menu_sub.tooltip = item.tooltip)

          return !item.switch
        },
      },
    ]
    subtitle.forEach((item, i) => {
      innerMenu.push({
        default: i === 0,
        html: (
          <span
            title={item.name}
            style={{
              "max-width": "200px",
              overflow: "hidden",
              "text-overflow": "ellipsis",
              "word-break": "break-all",
              "white-space": "normal",
              display: "-webkit-box",
              "-webkit-line-clamp": "2",
              "-webkit-box-orient": "vertical",
              "font-size": "12px",
            }}
          >
            {item.name}
          </span>
        ) as HTMLElement,
        name: item.name,
        url: proxyLink(item, true),
      })
    })

    option.settings?.push({
      id: "setting_subtitle",
      html: "Subtitle",
      tooltip: "Show",
      icon: ArtPlayerIconsSubtitle({ size: 24 }) as HTMLElement,
      selector: innerMenu,
      onSelect: function (item: Setting) {
        if (enableEnhanceAss && ext(item.name).toLowerCase() === "ass") {
          isEnhanceAssMode = true
          this.emit("artplayer-plugin-ass:switch" as keyof Events, item.url)
          setSubtitleVisible(true)
        } else {
          isEnhanceAssMode = false
          this.subtitle.switch(item.url, { name: item.name })
          this.once("subtitleLoad", setSubtitleVisible.bind(this, true))
        }

        const switcher = innerMenu.find(
          (_) => _.id === "setting_subtitle_display",
        )

        if (switcher && !switcher.switch) switcher.$html?.click?.()

        // sync from display switcher
        return switcher?.tooltip
      },
    })

    function setSubtitleVisible(visible: boolean) {
      const type = isEnhanceAssMode ? "ass" : "webvtt"

      switch (type) {
        case "ass":
          player.subtitle.show = false
          player.emit("artplayer-plugin-ass:visible" as keyof Events, visible)
          break

        case "webvtt":
        default:
          player.subtitle.show = visible
          player.emit("artplayer-plugin-ass:visible" as keyof Events, false)
          break
      }
    }
  }

  if (danmu) {
    option.plugins?.push(
      artplayerPluginDanmuku({
        danmuku: proxyLink(danmu, true),
        speed: 5,
        opacity: 1,
        fontSize: 25,
        color: "#FFFFFF",
        mode: 0,
        margin: [0, "0%"],
        antiOverlap: false,
        useWorker: true,
        synchronousPlayback: false,
        lockTime: 5,
        maxLength: 100,
        minWidth: 200,
        maxWidth: 400,
        theme: "dark",
      }),
    )
  }
  const [loading, post] = useFetch(
    (): PResp<Data> =>
      r.post("/fs/other", {
        path: pathname(),
        password: password(),
        method: "video_preview",
      }),
  )
  onMount(async () => {
    const resp = await post()
    setWarnVisible(resp.code !== 200)
    handleResp(resp, (data) => {
      const list =
        data.video_preview_play_info.live_transcoding_task_list.filter(
          (l) => l.url,
        )
      if (list.length === 0) {
        notify.error("No transcoding video found")
        return
      }
      option.url = list[list.length - 1].url
      option.quality = list.map((item, i) => {
        return {
          html: item.template_id,
          url: item.url,
          default: i === list.length - 1,
        }
      })
      player = new Artplayer(option)
      const auto_fullscreen = parseAutoFullscreen(searchParams["auto_fullscreen"])
      player.on("ready", () => {
        player.fullscreen = auto_fullscreen
      })
      player.on("video:loadedmetadata", () => {
        if (resumeApplied()) return
        const position = resumeAt()
        if (!Number.isFinite(position) || position <= 5) return
        const duration = player.duration || 0
        if (Number.isFinite(duration) && duration > 0 && position >= duration - 3) {
          return
        }
        if (autoResumeEnabled()) {
          player.seek = position
          setResumeApplied(true)
          setResumeVisible(false)
        } else {
          setResumeVisible(true)
        }
      })
      player.on("video:ended", () => {
        void saveVideoProgress({
          path: currentPath(),
          size: objStore.obj.size,
          mod_time: Date.parse(objStore.obj.modified) || 0,
          position: Number.isFinite(player.duration) ? player.duration : player.currentTime,
          duration: Number.isFinite(player.duration) ? player.duration : 0,
        })
        if (!autoNext()) return
        next_video()
      })
      player.on("video:timeupdate", () => {
        if (player.paused) return
        const now = player.currentTime || 0
        if (!Number.isFinite(now) || now <= 1) return
        const ts = Date.now()
        if (ts - lastReport < 10000 && Math.abs(now - lastReportedPos) < 5) return
        lastReport = ts
        lastReportedPos = now
        void saveVideoProgress({
          path: currentPath(),
          size: objStore.obj.size,
          mod_time: Date.parse(objStore.obj.modified) || 0,
          position: now,
          duration: Number.isFinite(player.duration) ? player.duration : 0,
        })
      })
      interval = window.setInterval(resetPlayUrl, 1000 * 60 * 14)
    })
  })
  let interval: number
  let curSeek: number
  async function resetPlayUrl() {
    const resp = await post()
    handleResp(resp, async (data) => {
      const list =
        data.video_preview_play_info.live_transcoding_task_list.filter(
          (l) => l.url,
        )
      if (list.length === 0) {
        notify.error("No transcoding video found")
        return
      }
      const quality = list.map((item, i) => {
        return {
          html: item.template_id,
          url: item.url,
          default: i === list.length - 1,
        }
      })
      option.quality = quality
      player.quality = quality
      curSeek = player.currentTime
      let curPlaying = player.playing
      await player.switchUrl(quality[quality.length - 1].url)
      if (!curPlaying) player.pause()
      setTimeout(() => {
        player.seek = curSeek
      }, 1000)
    })
  }
  onCleanup(() => {
    mediaController.cleanup()
    player?.destroy()
    window.clearInterval(interval)
  })
  const [autoNext, setAutoNext] = createSignal()
  const [warnVisible, setWarnVisible] = createSignal(false)
  onMount(() => {
    void (async () => {
      const resp = await getVideoProgress(currentPath())
      if (!resp || resp.code !== 200 || !resp.data) return
      if (resp.data.finished) return
      if (!Number.isFinite(resp.data.position) || resp.data.position <= 5) return
      setResumeAt(resp.data.position)
      setResumeVisible(true)
    })()
  })
  return (
    <VideoBox onAutoNextChange={setAutoNext}>
      <Box w="$full" h="60vh" position="relative">
        <Box w="$full" h="$full" id="video-player" />
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
                    if (!player) return
                    player.seek = resumeAt()
                    setResumeApplied(true)
                    setResumeVisible(false)
                    player.play()
                  }}
                >
                  Continue
                </Button>
                <Button
                  size="xs"
                  variant="outline"
                  colorScheme="neutral"
                  onClick={() => {
                    if (!player) return
                    player.seek = 0
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
      </Box>
      <Show when={warnVisible()}>
        <Center w="100%" h="60vh" bgColor="black">
          <TiWarning size="4rem" />
        </Center>
      </Show>
    </VideoBox>
  )
}

export default Preview
