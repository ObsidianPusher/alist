import { Center, VStack, Icon } from "@hope-ui/solid"
import { useContextMenu } from "solid-contextmenu"
import { batch, createMemo, createSignal, Show } from "solid-js"
import { CenterLoading, ImageWithError } from "~/components"
import { useLink, usePath, useUtil } from "~/hooks"
import { checkboxOpen, getMainColor, getSetting, objStore, selectAll, selectIndex } from "~/store"
import { ObjType, StoreObj } from "~/types"
import { api, bus } from "~/utils"
import { getIconByObj } from "~/utils/icon"
import {
  ItemCheckbox,
  useOpenItemWithCheckbox,
  useSelectWithMouse,
} from "./helper"

export const ImageItem = (props: { obj: StoreObj; index: number }) => {
  const { isHide } = useUtil()
  if (isHide(props.obj) || props.obj.type !== ObjType.IMAGE) {
    return null
  }
  const { setPathAs } = usePath()
  const objIcon = (
    <Icon color={getMainColor()} boxSize="$12" as={getIconByObj(props.obj)} />
  )
  const isLocalProvider = createMemo(() => /Local/i.test(objStore.provider || ""))
  const thumbSrc = createMemo(() => {
    if (!isLocalProvider()) return ""
    const raw = rawLink(props.obj, true)
    const sep = raw.includes("?") ? "&" : "?"
    if (props.obj.thumb) {
      const thumb = props.obj.thumb.startsWith("/d")
        ? `${api}${props.obj.thumb}`
        : props.obj.thumb
      return thumb.includes("type=thumb") ? thumb : `${thumb}${sep}type=thumb`
    }
    return `${raw}${sep}type=thumb`
  })
  const imageSize = createMemo(() => {
    const size = Number.parseInt(getSetting("image_view_item_size") || "", 10)
    return Number.isFinite(size) && size > 0 ? size : 150
  })
  const [hover, setHover] = createSignal(false)
  const showCheckbox = createMemo(
    () => checkboxOpen() && (hover() || props.obj.selected),
  )
  const { show } = useContextMenu({ id: 1 })
  const { rawLink } = useLink()
  const { isMouseSupported } = useSelectWithMouse()
  const isShouldOpenItem = useOpenItemWithCheckbox()
  return (
    <div
      style={{
        "flex-grow": 1,
        contain: "layout paint style",
        "content-visibility": "auto",
        "contain-intrinsic-size": "100px 180px",
      }}
    >
      <VStack
        w="$full"
        classList={{ selected: !!props.obj.selected }}
        class="image-item viselect-item"
        data-index={props.index}
        p="$1"
        spacing="$1"
        rounded="$lg"
        transition="all 0.3s"
        border="2px solid transparent"
        _hover={{
          border: `2px solid ${getMainColor()}`,
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
            selectAll(false)
            selectIndex(props.index, true, true)
          })
          show(e, { props: props.obj })
        }}
      >
        <Center
          w="$full"
          pos="relative"
          cursor={
            !isMouseSupported() && (!checkboxOpen() || isShouldOpenItem())
              ? "pointer"
              : "default"
          }
          on:dblclick={(e: MouseEvent) => {
            if (!isMouseSupported()) return
            if (e.ctrlKey || e.metaKey || e.shiftKey) return
            bus.emit("gallery", props.obj.name)
          }}
          on:click={(e: MouseEvent) => {
            const target = e.target as HTMLElement | null
            if (target?.closest("input[type='checkbox']")) return
            if (isMouseSupported()) return
            if (!checkboxOpen() || isShouldOpenItem()) {
              bus.emit("gallery", props.obj.name)
              return
            }
            selectIndex(props.index, !props.obj.selected)
          }}
        >
          <Show
            when={showCheckbox() || (isMouseSupported() && props.obj.selected)}
          >
            <ItemCheckbox
              pos="absolute"
              left="$1"
              top="$1"
              checked={props.obj.selected}
              onChange={(e: any) => {
                selectIndex(props.index, e.target.checked)
              }}
            />
          </Show>
          <Show when={isLocalProvider()} fallback={objIcon}>
            <ImageWithError
              h={`${imageSize()}px`}
              w="$full"
              objectFit="cover"
              rounded="$lg"
              shadow="$md"
              fallback={<CenterLoading size="lg" />}
              fallbackErr={objIcon}
              src={thumbSrc() || rawLink(props.obj)}
              alt={props.obj.name}
              loading="lazy"
              decoding="async"
            />
          </Show>
        </Center>
      </VStack>
    </div>
  )
}
