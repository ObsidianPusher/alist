import { Flex, Grid, Heading, VStack } from "@hope-ui/solid"
import {
  For,
  Show,
  createEffect,
  createMemo,
  createSignal,
  onCleanup,
} from "solid-js"
import { ImageItem } from "./ImageItem"
import { local, objStore } from "~/store"
import { GridItem } from "./GridItem"
import { ObjType, StoreObj } from "~/types"
import { useT } from "~/hooks"
import { useSelectWithMouse } from "./helper"

const INITIAL_BATCH_SIZE = 36
const BATCH_STEP_SIZE = 36

const ImageLayout = (props: { images: StoreObj[] }) => {
  const t = useT()
  const folders = createMemo(() =>
    objStore.objs
      .map((obj, index) => ({ obj, index }))
      .filter(({ obj }) => obj.is_dir),
  )
  const imageItems = createMemo(() =>
    objStore.objs.reduce<{ obj: StoreObj; index: number }[]>((items, obj, index) => {
      if (obj.type === ObjType.IMAGE) {
        items.push({ obj, index })
      }
      return items
    }, []),
  )
  const [visibleCount, setVisibleCount] = createSignal(INITIAL_BATCH_SIZE)
  const [loadMoreAnchor, setLoadMoreAnchor] =
    createSignal<HTMLDivElement | undefined>()
  let loadMoreObserver: IntersectionObserver | undefined

  createEffect(() => {
    const total = imageItems().length
    if (visibleCount() === 0 || visibleCount() > total) {
      setVisibleCount(Math.min(INITIAL_BATCH_SIZE, total))
    }
  })
  const { isMouseSupported, registerSelectContainer, captureContentMenu } =
    useSelectWithMouse()
  registerSelectContainer()

  const loadMore = () => {
    setVisibleCount((count) =>
      Math.min(count + BATCH_STEP_SIZE, imageItems().length),
    )
  }

  createEffect(() => {
    const anchor = loadMoreAnchor()
    if (!anchor) return
    loadMoreObserver?.disconnect()
    loadMoreObserver = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) {
          loadMore()
        }
      },
      {
        rootMargin: "1200px 0px 1200px 0px",
        threshold: 0,
      },
    )
    loadMoreObserver.observe(anchor)
  })

  onCleanup(() => {
    loadMoreObserver?.disconnect()
  })

  const visibleImages = createMemo(() =>
    imageItems().slice(0, visibleCount()),
  )
  return (
    <VStack
      oncapture:contextmenu={captureContentMenu}
      classList={{ "viselect-container": isMouseSupported() }}
      spacing="$2"
      w="$full"
    >
      <Show when={local["show_folder_in_image_view"] === "top"}>
        <Grid
          w="$full"
          gap="$1"
          templateColumns="repeat(auto-fill, minmax(100px,1fr))"
          class="image-folders"
        >
          <For each={folders()}>
            {(item) => {
              return <GridItem obj={item.obj} index={item.index} />
            }}
          </For>
        </Grid>
      </Show>
      <Show
        when={visibleImages().length > 0}
        fallback={<Heading m="$2">{t("home.no_images")}</Heading>}
      >
        <Flex w="$full" gap="$1" flexWrap="wrap" class="image-images">
          <For each={visibleImages()}>
            {(item) => {
              return <ImageItem obj={item.obj} index={item.index} />
            }}
          </For>
        </Flex>
      </Show>
      <Show when={visibleCount() < imageItems().length}>
        <div
          ref={setLoadMoreAnchor}
          aria-hidden="true"
          style={{
            height: "1px",
            width: "100%",
          }}
        />
      </Show>
      <Show when={local["show_folder_in_image_view"] === "bottom"}>
        <Grid
          w="$full"
          gap="$1"
          templateColumns="repeat(auto-fill, minmax(100px,1fr))"
          class="image-folders"
        >
          <For each={folders()}>
            {(item) => {
              return <GridItem obj={item.obj} index={item.index} />
            }}
          </For>
        </Grid>
      </Show>
    </VStack>
  )
}

export default ImageLayout
