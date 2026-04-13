import {
  Badge,
  Box,
  Button,
  Checkbox,
  createDisclosure,
  HStack,
  IconButton,
  Text,
  VStack,
} from "@hope-ui/solid"
import { createMemo, createSignal, For, onMount, Show } from "solid-js"
import { ModalFolderChoose, SelectWrapper } from "~/components"
import { useFetch, useRouter, useT } from "~/hooks"
import { VideoFavoriteItem, VideoFavoriteList } from "~/types"
import {
  batchCopyVideoFavorites,
  batchRemoveVideoFavorites,
  listVideoFavorites,
  toggleVideoFavorite,
} from "~/utils/api"
import {
  formatDate,
  getFileSize,
  handleRespWithNotifySuccess,
  handleRespWithoutNotify,
  notify,
} from "~/utils"
import { AiFillHeart, AiOutlineReload } from "solid-icons/ai"

const PAGE_SIZE = 200

const Favorites = () => {
  const t = useT()
  const { to } = useRouter()
  const [storageFilter, setStorageFilter] = createSignal<number>(0)
  const [listData, setListData] = createSignal<VideoFavoriteList>({
    content: [],
    total: 0,
    storage_filters: [],
    invalid_pruned: 0,
    permission_prune: 0,
  })
  const [selected, setSelected] = createSignal<Set<number>>(new Set())
  const [listLoading, listReq] = useFetch(listVideoFavorites)
  const [toggleLoading, toggleReq] = useFetch(toggleVideoFavorite)
  const [batchCopyLoading, batchCopyReq] = useFetch(batchCopyVideoFavorites)
  const [batchRemoveLoading, batchRemoveReq] = useFetch(batchRemoveVideoFavorites)
  const copyModal = createDisclosure()

  const selectedItems = createMemo(() => {
    const map = new Map<number, VideoFavoriteItem>()
    for (const item of listData().content) {
      map.set(item.id, item)
    }
    return Array.from(selected())
      .map((id) => map.get(id))
      .filter((v): v is VideoFavoriteItem => !!v)
  })

  const selectedStorageID = createMemo(() => {
    const first = selectedItems()[0]
    return first?.storage_id
  })

  const reload = async () => {
    const resp = await listReq({
      page: 1,
      per_page: PAGE_SIZE,
      storage_id: storageFilter() || undefined,
    })
    handleRespWithoutNotify(
      resp,
      (data) => {
        setListData(data)
        const next = new Set<number>()
        const exists = new Set(data.content.map((item) => item.id))
        for (const id of selected()) {
          if (exists.has(id)) next.add(id)
        }
        setSelected(next)
      },
      (msg) => notify.error(msg),
    )
  }

  const toggleSelected = (id: number, checked: boolean) => {
    const item = listData().content.find((v) => v.id === id)
    if (!item) return
    if (checked && selectedStorageID() && selectedStorageID() !== item.storage_id) {
      notify.warning(t("home.favorites.same_storage_only"))
      return
    }
    const next = new Set(selected())
    if (checked) next.add(id)
    else next.delete(id)
    setSelected(next)
  }

  const removeFavorite = async (item: VideoFavoriteItem) => {
    const resp = await toggleReq({ path: item.path, favorite: false })
    handleRespWithNotifySuccess(resp, () => {
      const next = new Set(selected())
      next.delete(item.id)
      setSelected(next)
      void reload()
    })
  }

  onMount(() => {
    void reload()
  })

  return (
    <VStack w="$full" spacing="$3" alignItems="stretch" p="$1">
      <HStack
        justifyContent="space-between"
        direction={{ "@initial": "column", "@md": "row" }}
        alignItems={{ "@initial": "stretch", "@md": "center" }}
        spacing="$2"
      >
        <HStack spacing="$2" wrap="wrap">
          <Badge colorScheme="danger" variant="subtle">
            {t("home.favorites.total")}: {listData().total}
          </Badge>
          <Show when={listData().invalid_pruned > 0}>
            <Badge colorScheme="warning" variant="subtle">
              {t("home.favorites.auto_cleaned")}: {listData().invalid_pruned}
            </Badge>
          </Show>
        </HStack>
        <HStack spacing="$2">
          <SelectWrapper
            size="sm"
            alwaysShowBorder
            w="220px"
            value={storageFilter()}
            onChange={(value) => {
              setStorageFilter(value)
              setSelected(new Set())
              void reload()
            }}
            options={[
              { value: 0, label: t("home.favorites.all_storages") },
              ...listData().storage_filters.map((item) => ({
                value: item.storage_id,
                label: item.storage_name,
              })),
            ]}
          />
          <IconButton
            aria-label={t("global.refresh")}
            icon={<AiOutlineReload />}
            loading={listLoading()}
            onClick={() => void reload()}
          />
        </HStack>
      </HStack>

      <HStack
        justifyContent="space-between"
        direction={{ "@initial": "column", "@md": "row" }}
        alignItems={{ "@initial": "stretch", "@md": "center" }}
        spacing="$2"
      >
        <Text fontSize="$sm" color="$neutral11">
          {t("home.favorites.pick_same_storage")}
        </Text>
        <HStack spacing="$2">
          <Button
            size="sm"
            colorScheme="success"
            disabled={selected().size === 0}
            onClick={copyModal.onOpen}
          >
            {t("home.favorites.batch_copy")} ({selected().size})
          </Button>
          <Button
            size="sm"
            colorScheme="warning"
            variant="outline"
            loading={batchRemoveLoading()}
            disabled={selected().size === 0}
            onClick={async () => {
              const ids = Array.from(selected())
              const resp = await batchRemoveReq(ids)
              handleRespWithNotifySuccess(resp, () => {
                setSelected(new Set())
                void reload()
              })
            }}
          >
            {t("home.favorites.batch_unfavorite")}
          </Button>
        </HStack>
      </HStack>

      <Show
        when={listData().content.length > 0}
        fallback={
          <Box py="$8" textAlign="center" color="$neutral11">
            {t("home.favorites.empty")}
          </Box>
        }
      >
        <VStack spacing="$2" alignItems="stretch">
          <For each={listData().content}>
            {(item) => {
              const disabledByStorage =
                !!selectedStorageID() && selectedStorageID() !== item.storage_id
              return (
                <HStack
                  px="$3"
                  py="$2"
                  borderWidth="1px"
                  borderColor="$neutral6"
                  rounded="$md"
                  justifyContent="space-between"
                  alignItems="center"
                  spacing="$2"
                  opacity={disabledByStorage ? 0.6 : 1}
                >
                  <HStack spacing="$3" alignItems="center" minW={0} flex={1}>
                    <Checkbox
                      checked={selected().has(item.id)}
                      disabled={disabledByStorage}
                      onChange={(e) => toggleSelected(item.id, e.currentTarget.checked)}
                    />
                    <VStack alignItems="flex-start" spacing="1" minW={0} flex={1}>
                      <HStack spacing="$2" wrap="wrap">
                        <Text
                          fontWeight="$semibold"
                          maxW="60vw"
                          overflow="hidden"
                          textOverflow="ellipsis"
                          whiteSpace="nowrap"
                          onClick={() => to(item.path)}
                          cursor="pointer"
                        >
                          {item.name}
                        </Text>
                        <Badge colorScheme={item.watched ? "success" : "info"}>
                          {item.watched ? "DONE" : `${Math.round(item.progress || 0)}%`}
                        </Badge>
                        <Badge variant="subtle">{item.storage_name}</Badge>
                      </HStack>
                      <Text fontSize="$xs" color="$neutral11">
                        {item.path}
                      </Text>
                      <Text fontSize="$xs" color="$neutral11">
                        {getFileSize(item.size)} · {formatDate(item.modified)} · {t("home.favorites.favorited_at")}: {formatDate(item.favorited_at)}
                      </Text>
                    </VStack>
                  </HStack>
                  <IconButton
                    aria-label="unfavorite"
                    icon={<AiFillHeart />}
                    colorScheme="danger"
                    variant="ghost"
                    loading={toggleLoading()}
                    onClick={() => void removeFavorite(item)}
                  />
                </HStack>
              )
            }}
          </For>
        </VStack>
      </Show>

      <ModalFolderChoose
        opened={copyModal.isOpen()}
        onClose={copyModal.onClose}
        loading={batchCopyLoading()}
        onSubmit={async (dstDir) => {
          const resp = await batchCopyReq({
            ids: Array.from(selected()),
            dst_dir: dstDir,
          })
          handleRespWithNotifySuccess(resp, () => {
            copyModal.onClose()
          })
        }}
      />
    </VStack>
  )
}

export default Favorites
