import { Box, HStack, Icon, Text } from "@hope-ui/solid"
import { Motion } from "@motionone/solid"
import { useContextMenu } from "solid-contextmenu"
import { batch, Show } from "solid-js"
import { LinkWithPush } from "~/components"
import { usePath, useRouter, useUtil } from "~/hooks"
import {
  checkboxOpen,
  getMainColor,
  OrderBy,
  selectAll,
  selectIndex,
} from "~/store"
import { ObjType, StoreObj } from "~/types"
import { bus, formatDate, getFileSize, hoverColor } from "~/utils"
import { getIconByObj } from "~/utils/icon"
import {
  ItemCheckbox,
  useOpenItemWithCheckbox,
  useSelectWithMouse,
} from "./helper"

export interface Col {
  name: OrderBy
  textAlign: "left" | "right"
  w: any
}

export const cols: Col[] = [
  { name: "name", textAlign: "left", w: { "@initial": "76%", "@md": "50%" } },
  { name: "size", textAlign: "right", w: { "@initial": "24%", "@md": "17%" } },
  { name: "modified", textAlign: "right", w: { "@initial": 0, "@md": "33%" } },
]

export const ListItem = (props: { obj: StoreObj; index: number }) => {
  const { isHide } = useUtil()
  if (isHide(props.obj)) {
    return null
  }
  const { setPathAs } = usePath()
  const { show } = useContextMenu({ id: 1 })
  const { pushHref, to } = useRouter()
  const { isMouseSupported } = useSelectWithMouse()
  const isShouldOpenItem = useOpenItemWithCheckbox()
  const progress = () => Math.min(100, Math.max(0, props.obj.progress || 0))
  const showProgress = () =>
    props.obj.type === ObjType.VIDEO && (props.obj.watched || progress() > 0)
  return (
    <Motion.div
      initial={{ opacity: 0, scale: 0.95 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.2 }}
      style={{
        width: "100%",
      }}
    >
      <HStack
        classList={{ selected: !!props.obj.selected }}
        class="list-item viselect-item"
        data-index={props.index}
        w="$full"
        p="$2"
        rounded="$lg"
        position="relative"
        transition="all 0.3s"
        _hover={{
          transform: "scale(1.01)",
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
          setPathAs(props.obj.name, props.obj.is_dir, true)
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
        <Show when={showProgress()}>
          <Box
            position="absolute"
            left="$3"
            right="$3"
            bottom="$1"
            h="2px"
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
        <HStack class="name-box" spacing="$1" w={cols[0].w}>
          <Show when={checkboxOpen()}>
            <ItemCheckbox
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
          <Icon
            class="icon"
            boxSize="$6"
            color={getMainColor()}
            as={getIconByObj(props.obj)}
            mr="$1"
            on:click={(e: MouseEvent) => {
              if (props.obj.type === ObjType.IMAGE) {
                e.stopPropagation()
                e.preventDefault()
                bus.emit("gallery", props.obj.name)
              }
            }}
          />
          <Text
            class="name"
            css={{
              wordBreak: "break-all",
              whiteSpace: "normal",
              overflowWrap: "anywhere",
              lineHeight: "1.3",
            }}
            title={props.obj.name}
          >
            {props.obj.name}
          </Text>
        </HStack>
        <Text class="size" w={cols[1].w} textAlign={cols[1].textAlign as any}>
          {getFileSize(props.obj.size)}
        </Text>
        <Text
          class="modified"
          display={{ "@initial": "none", "@md": "inline" }}
          w={cols[2].w}
          textAlign={cols[2].textAlign as any}
        >
          {formatDate(props.obj.modified)}
        </Text>
      </HStack>
    </Motion.div>
  )
}
