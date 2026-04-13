import { Box, Center, VStack, Icon, Text } from "@hope-ui/solid"
import { Motion } from "@motionone/solid"
import { useContextMenu } from "solid-contextmenu"
import { batch, createMemo, createSignal, Show } from "solid-js"
import {
  CenterLoading,
  LinkWithPush,
  ImageWithError,
  ThumbLoadError,
  VideoThumbnail,
} from "~/components"
import { useLink, usePath, useRouter, useUtil } from "~/hooks"
import {
  checkboxOpen,
  getSetting,
  getMainColor,
  local,
  objStore,
  selectAll,
  selectIndex,
} from "~/store"
import { ObjType, StoreObj } from "~/types"
import { api, bus, ext, hoverColor } from "~/utils"
import { getIconByObj } from "~/utils/icon"
import {
  ItemCheckbox,
  useOpenItemWithCheckbox,
  useSelectWithMouse,
} from "./helper"

export const GridItem = (props: { obj: StoreObj; index: number }) => {
  const { isHide } = useUtil()
  if (isHide(props.obj)) {
    return null
  }
  const { setPathAs } = usePath()
  const { rawLink } = useLink()
  const objIcon = (
    <Icon
      color={getMainColor()}
      boxSize={`${parseInt(local["grid_item_size"]) - 30}px`}
      as={getIconByObj(props.obj)}
    />
  )
  const [hover, setHover] = createSignal(false)
  const mediaExt = createMemo(() => ext(props.obj.name).toLowerCase())
  const imageTypes = createMemo(() =>
    (getSetting("image_types") || "")
      .split(",")
      .map((v) => v.trim().toLowerCase())
      .filter(Boolean),
  )
  const videoTypes = createMemo(() =>
    (getSetting("video_types") || "")
      .split(",")
      .map((v) => v.trim().toLowerCase())
      .filter(Boolean),
  )
  const isVideoMedia = createMemo(
    () =>
      props.obj.type === ObjType.VIDEO ||
      videoTypes().includes(mediaExt()) ||
      mediaExt() === "mp4",
  )
  const isBaiduProvider = createMemo(() =>
    /BaiduNetdisk/i.test(objStore.provider || ""),
  )
  const isLocalProvider = createMemo(() => /Local/i.test(objStore.provider || ""))
  const disableAllThumb = createMemo(() => !isLocalProvider())
  const disableVideoThumb = createMemo(() => isVideoMedia() && disableAllThumb())
  const isImageMedia = createMemo(
    () =>
      !isVideoMedia() &&
      (props.obj.type === ObjType.IMAGE || imageTypes().includes(mediaExt())),
  )
  const showCheckbox = createMemo(
    () => checkboxOpen() && (hover() || props.obj.selected),
  )
  const progress = createMemo(() =>
    Math.min(100, Math.max(0, props.obj.progress || 0)),
  )
  const showProgress = createMemo(
    () =>
      props.obj.type === ObjType.VIDEO &&
      (props.obj.watched || progress() > 0),
  )
  const imageThumbSrc = createMemo(() => {
    if (disableAllThumb()) return ""
    const raw = rawLink(props.obj, true)
    const sep = raw.includes("?") ? "&" : "?"
    const rawThumb = `${raw}${sep}type=thumb`
    if (props.obj.thumb) {
      const t = props.obj.thumb
      if (t.includes("type=thumb")) {
        return t.startsWith("/d") ? `${api}${t}` : t
      }
      if (props.obj.thumb.startsWith("/d")) {
        return `${api}${props.obj.thumb}`
      }
      return props.obj.thumb
    }
    if (props.obj.type !== ObjType.IMAGE && props.obj.type !== ObjType.VIDEO) return ""
    return rawThumb
  })
  const directThumbSrc = createMemo(() => {
    if (disableAllThumb()) return ""
    const raw = rawLink(props.obj, true)
    const sep = raw.includes("?") ? "&" : "?"
    return `${raw}${sep}type=thumb`
  })
  const videoThumbImageSrc = createMemo(() => {
    if (!isVideoMedia()) return ""
    if (disableVideoThumb()) return ""
    if (disableAllThumb()) return ""
    // For non-Baidu providers, prefer explicit image thumbnail from backend if present.
    if (!props.obj.thumb) return ""
    if (props.obj.thumb.includes("type=thumb")) return ""
    if (props.obj.thumb.startsWith("/d")) return `${api}${props.obj.thumb}`
    return props.obj.thumb
  })
  const videoSrc = createMemo(() => {
    if (!isVideoMedia()) return ""
    return rawLink(props.obj, true)
  })
  const thumbSrc = createMemo(() => {
    if (isVideoMedia()) {
      return videoThumbImageSrc()
    }
    if (!isImageMedia()) {
      return ""
    }
    if (disableAllThumb()) return ""
    if (imageThumbSrc()) return imageThumbSrc()
    const raw = rawLink(props.obj, true)
    const sep = raw.includes("?") ? "&" : "?"
    return `${raw}${sep}type=thumb`
  })
  const { show } = useContextMenu({ id: 1 })
  const { pushHref, to } = useRouter()
  const { isMouseSupported } = useSelectWithMouse()
  const isShouldOpenItem = useOpenItemWithCheckbox()
  return (
    <Motion.div
      initial={{ opacity: 0, scale: 0.9 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.2 }}
      style={{
        width: "100%",
      }}
    >
      <VStack
        classList={{ selected: !!props.obj.selected }}
        class="grid-item viselect-item"
        data-index={props.index}
        w="$full"
        p="$1"
        spacing="$1"
        rounded="$lg"
        transition="all 0.3s"
        _hover={{
          transform: "scale(1.06)",
          bgColor: hoverColor(),
        }}
        as={LinkWithPush}
        href={props.obj.name}
        cursor={
          !isMouseSupported() && (!checkboxOpen() || isShouldOpenItem())
            ? "pointer"
            : "default"
        }
        bgColor={props.obj.selected ? hoverColor() : undefined}
        on:dblclick={(e: MouseEvent) => {
          if (!isMouseSupported()) return
          if (e.ctrlKey || e.metaKey || e.shiftKey) return
          to(pushHref(props.obj.name))
        }}
        on:click={(e: MouseEvent) => {
          if (isMouseSupported()) return e.preventDefault()
          if (!checkboxOpen()) return
          e.preventDefault()
          if (isShouldOpenItem()) {
            to(pushHref(props.obj.name))
            return
          }
          selectIndex(props.index, !props.obj.selected)
        }}
        onMouseEnter={() => {
          setHover(true)
          setPathAs(props.obj.name, props.obj.is_dir, true)
        }}
        onMouseLeave={() => {
          setHover(false)
        }}
        onContextMenu={(e: MouseEvent) => {
          batch(() => {
            // if (!checkboxOpen()) {
            //   toggleCheckbox();
            // }
            selectAll(false)
            selectIndex(props.index, true, true)
          })
          show(e, { props: props.obj })
        }}
      >
        <Center
          class="item-thumbnail"
          h={`${parseInt(local["grid_item_size"])}px`}
          w="$full"
          on:dblclick={(e: MouseEvent) => {
            if (!isMouseSupported()) return
            if (isImageMedia()) {
              e.stopPropagation()
              e.preventDefault()
              bus.emit("gallery", props.obj.name)
            }
          }}
          on:click={(e: MouseEvent) => {
            if (isMouseSupported()) return
            if (checkboxOpen() && !isShouldOpenItem()) return
            if (isImageMedia()) {
              e.stopPropagation()
              e.preventDefault()
              bus.emit("gallery", props.obj.name)
            }
          }}
          pos="relative"
        >
          <Show when={showProgress()}>
            <Box
              position="absolute"
              top="$1"
              right="$1"
              bg={props.obj.watched ? "$success9" : "$info9"}
              color="white"
              px="$2"
              py="$0_5"
              fontSize="$xs"
              rounded="full"
              shadow="$sm"
            >
              {props.obj.watched ? "DONE" : `${Math.round(progress())}%`}
            </Box>
            <Box
              position="absolute"
              left="$1"
              right="$1"
              bottom="$1"
              h="3px"
              bg="rgba(148, 163, 184, 0.35)"
              rounded="full"
            >
              <Box
                h="$full"
                rounded="full"
                bg={props.obj.watched ? "$success9" : "$info9"}
                w={`${props.obj.watched ? 100 : progress()}%`}
              />
            </Box>
          </Show>
          <Show when={showCheckbox()}>
            <ItemCheckbox
              pos="absolute"
              left="$1"
              top="$1"
              // colorScheme="neutral"
              on:click={(e: MouseEvent) => {
                e.stopPropagation()
              }}
              checked={props.obj.selected}
              onChange={(e: any) => {
                selectIndex(props.index, e.target.checked)
              }}
            />
          </Show>
          <Show when={!disableAllThumb()} fallback={objIcon}>
            <Show when={isVideoMedia() && !disableVideoThumb()} fallback={<Show when={isImageMedia()} fallback={objIcon}>
              <Show when={thumbSrc()} fallback={<ThumbLoadError src={directThumbSrc()} name={props.obj.name} />}>
                <ImageWithError
                  maxH="$full"
                  maxW="$full"
                  rounded="$lg"
                  shadow="$md"
                  fallback={<CenterLoading size="lg" />}
                  fallbackErr={<ThumbLoadError src={thumbSrc()} name={props.obj.name} />}
                  src={thumbSrc()}
                  alt={props.obj.name}
                  loading="lazy"
                />
              </Show>
            </Show>}>
              <Show when={videoThumbImageSrc()} fallback={<VideoThumbnail src={videoSrc()} alt={props.obj.name} fallbackErr={<ThumbLoadError src={videoSrc()} name={props.obj.name} report={false} />} />}>
                <ImageWithError
                  maxH="$full"
                  maxW="$full"
                  rounded="$lg"
                  shadow="$md"
                  fallback={<CenterLoading size="lg" />}
                  fallbackErr={<ThumbLoadError src={videoThumbImageSrc()} name={props.obj.name} report={false} />}
                  src={videoThumbImageSrc()}
                  alt={props.obj.name}
                  loading="lazy"
                />
              </Show>
            </Show>
          </Show>
        </Center>
        <Text
          css={{
            whiteSpace: "normal",
            textOverflow: "unset",
            wordBreak: "break-all",
            overflowWrap: "anywhere",
            lineHeight: "1.3",
          }}
          w="$full"
          overflow="visible"
          textAlign="center"
          fontSize="$sm"
          title={props.obj.name}
        >
          {props.obj.name}
        </Text>
      </VStack>
    </Motion.div>
  )
}
