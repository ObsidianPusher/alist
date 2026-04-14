import {
  Badge,
  Box,
  Button,
  Checkbox,
  createDisclosure,
  FormControl,
  FormLabel,
  HStack,
  IconButton,
  Input,
  Modal,
  ModalBody,
  ModalCloseButton,
  ModalContent,
  ModalFooter,
  ModalHeader,
  ModalOverlay,
  Progress,
  ProgressIndicator,
  Table,
  Tbody,
  Td,
  Text,
  Th,
  Thead,
  Tr,
  VStack,
  useColorModeValue,
} from "@hope-ui/solid"
import { createMemo, createSignal, For, onCleanup, onMount, Show } from "solid-js"
import {
  FiChevronRight,
  FiFile,
  FiFolder,
  FiRefreshCw,
  FiSearch,
} from "solid-icons/fi"
import { ModalFolderChoose, ModalInput, SelectWrapper } from "~/components"
import { useFetch, useRouter, useT } from "~/hooks"
import { mkdirFs } from "~/api/fs-ops"
import {
  localBrowserCopy,
  localBrowserCompress,
  localBrowserDirSizes,
  localBrowserExtract,
  localBrowserList,
  localBrowserMove,
  localBrowserRemove,
  localBrowserStorages,
  localBrowserUsage,
} from "~/api/local-browser"
import {
  LocalBrowserDirSizeItem,
  LocalBrowserEntry,
  LocalBrowserStorage,
  LocalBrowserUsage,
} from "~/types"
import {
  formatDate,
  getFileSize,
  handleResp,
  notify,
  pathDir,
  pathJoin,
} from "~/utils"

type LocalBrowserKind =
  | "all"
  | "dir"
  | "video"
  | "image"
  | "audio"
  | "text"
  | "archive"
  | "document"
  | "package"
  | "other"

const kindOptions: LocalBrowserKind[] = [
  "all",
  "dir",
  "video",
  "image",
  "audio",
  "text",
  "archive",
  "document",
  "package",
  "other",
]

const kindQuickOptions: LocalBrowserKind[] = [...kindOptions]

const Browser = () => {
  const t = useT()
  const { to, setSearchParams } = useRouter()
  const cardBg = useColorModeValue("white", "$neutral2")
  const pageBg = useColorModeValue(
    "linear(to-br, $neutral1, #f7fbff)",
    "linear(to-br, $neutral2, $neutral3)",
  )
  const panelBg = useColorModeValue("$neutral1", "$neutral3")
  const subtleBg = useColorModeValue("$neutral2", "$neutral4")
  const lineColor = useColorModeValue("$neutral6", "$neutral7")
  const searchBg = useColorModeValue("$neutral1", "$neutral3")
  const [storages, setStorages] = createSignal<LocalBrowserStorage[]>([])
  const [entries, setEntries] = createSignal<LocalBrowserEntry[]>([])
  const [total, setTotal] = createSignal(0)
  const [path, setPath] = createSignal("/")
  const [storageMount, setStorageMount] = createSignal("")
  const [pendingStorageMount, setPendingStorageMount] = createSignal("")
  const [keyword, setKeyword] = createSignal("")
  const [kind, setKind] = createSignal<LocalBrowserKind>("all")
  const [recursive, setRecursive] = createSignal(false)
  const [page, setPage] = createSignal(1)
  const [perPage] = createSignal(100)
  const [selected, setSelected] = createSignal<Record<string, boolean>>({})
  const [archiveName, setArchiveName] = createSignal("archive.zip")
  const [password, setPassword] = createSignal("")
  const [extractDstDir, setExtractDstDir] = createSignal("")
  const [usage, setUsage] = createSignal<LocalBrowserUsage | null>(null)
  const [dirSizeMap, setDirSizeMap] = createSignal<Record<string, LocalBrowserDirSizeItem>>({})
  let dirSizeTimer: number | undefined

  const compressModal = createDisclosure()
  const extractModal = createDisclosure()
  const copyModal = createDisclosure()
  const moveModal = createDisclosure()
  const deleteModal = createDisclosure()
  const mkdirModal = createDisclosure()

  const [, loadStorages] = useFetch(localBrowserStorages, true)
  const [loadListLoading, loadList] = useFetch(localBrowserList)
  const [compressLoading, compressReq] = useFetch(localBrowserCompress)
  const [extractLoading, extractReq] = useFetch(localBrowserExtract)
  const [copyLoading, copyReq] = useFetch(localBrowserCopy)
  const [moveLoading, moveReq] = useFetch(localBrowserMove)
  const [deleteLoading, deleteReq] = useFetch(localBrowserRemove)
  const [mkdirLoading, mkdirReq] = useFetch(mkdirFs)
  const [, usageReq] = useFetch(localBrowserUsage)
  const [, dirSizesReq] = useFetch(localBrowserDirSizes)

  const selectedEntries = createMemo(() =>
    entries().filter((entry) => selected()[entry.path]),
  )

  const selectedNames = createMemo(() =>
    selectedEntries().map((entry) => entry.name),
  )
  const selectedPaths = createMemo(() =>
    selectedEntries().map((entry) => entry.path),
  )
  const selectedCount = createMemo(() => selectedEntries().length)
  const allCurrentChecked = createMemo(
    () =>
      entries().length > 0 &&
      entries().every((entry) => Boolean(selected()[entry.path])),
  )
  const hasFilters = createMemo(
    () => keyword().trim() !== "" || recursive() || kind() !== "all",
  )

  const selectedExtractTargets = createMemo<LocalBrowserEntry[]>(() => {
    const picked = selectedEntries()
    if (picked.length === 0) return []
    const archives = picked.filter(
      (entry) => !entry.is_dir && entry.extractable,
    )
    if (archives.length !== picked.length) return []
    const grouped = new Map<string, LocalBrowserEntry>()
    for (const entry of archives) {
      const groupKey = entry.archive_group
        ? `${pathDir(entry.path)}::${entry.archive_group}`
        : entry.path
      if (!grouped.has(groupKey)) {
        grouped.set(groupKey, entry)
      }
    }
    return Array.from(grouped.values())
  })

  const selectedExtractPreview = createMemo(() => {
    const targets = selectedExtractTargets()
    if (targets.length === 0) return "-"
    if (targets.length === 1) return targets[0].name
    return `${targets[0].name} +${targets.length - 1}`
  })

  const selectedExtractFormat = createMemo(() => {
    const targets = selectedExtractTargets()
    if (targets.length !== 1) return ""
    return targets[0].archive_format || ""
  })

  const breadcrumbs = createMemo(() => {
    const current = path().trim()
    if (!current || current === "/") return [{ label: "/", path: "/" }]
    const fixed = current.startsWith("/") ? current : `/${current}`
    const parts = fixed.split("/").filter(Boolean)
    const res: { label: string; path: string }[] = [{ label: "/", path: "/" }]
    let acc = ""
    for (const part of parts) {
      acc += `/${part}`
      res.push({ label: part, path: acc })
    }
    return res
  })

  const totalPage = createMemo(() => {
    const totalValue = total()
    const size = perPage()
    if (totalValue <= 0) return 1
    return Math.max(1, Math.ceil(totalValue / size))
  })

  const setSelectedPath = (pathValue: string, checked: boolean) => {
    setSelected((prev) => ({ ...prev, [pathValue]: checked }))
  }

  const clearSelection = () => setSelected({})

  const resetFilters = () => {
    setKeyword("")
    setKind("all")
    setRecursive(false)
    setPage(1)
  }

  const clearDirSizeTimer = () => {
    if (dirSizeTimer !== undefined) {
      window.clearTimeout(dirSizeTimer)
      dirSizeTimer = undefined
    }
  }

  const refreshUsage = async (targetPath: string) => {
    const resp = await usageReq({ path: targetPath })
    handleResp(resp, (data) => {
      setUsage(data)
    })
  }

  const mergeDirSizes = (items: LocalBrowserDirSizeItem[]) => {
    if (!items || items.length === 0) return
    setDirSizeMap((prev) => {
      const next = { ...prev }
      for (const item of items) {
        next[item.path] = item
      }
      return next
    })
  }

  const refreshDirSizes = async (targetPaths: string[], retry = true) => {
    clearDirSizeTimer()
    const paths = targetPaths.filter(Boolean)
    if (paths.length === 0) {
      return
    }
    const resp = await dirSizesReq({ paths })
    handleResp(resp, (data) => {
      mergeDirSizes(data.items || [])
      const pending = (data.items || []).some(
        (item: LocalBrowserDirSizeItem) => item.status === "pending",
      )
      if (pending && retry) {
        dirSizeTimer = window.setTimeout(() => {
          void refreshDirSizes(paths, true)
        }, 1200)
      }
    })
  }

  const renderEntrySize = (entry: LocalBrowserEntry) => {
    if (!entry.is_dir) {
      return getFileSize(entry.size)
    }
    const state = dirSizeMap()[entry.path]
    if (!state || state.status === "pending") {
      return t("global.loading")
    }
    if (state.status === "ready") {
      return getFileSize(state.size)
    }
    return "-"
  }

  const refreshList = async (nextPath?: string, resetPage = false) => {
    const targetPath = nextPath ?? path()
    const targetPage = resetPage ? 1 : page()
    if (resetPage) setPage(1)
    const resp = await loadList({
      path: targetPath,
      keyword: keyword().trim(),
      kind: kind(),
      recursive: recursive(),
      page: targetPage,
      per_page: perPage(),
    })
    handleResp(resp, (data) => {
      clearDirSizeTimer()
      setDirSizeMap({})
      setEntries(data.content)
      setTotal(data.total)
      setPath(data.current_path)
      setStorageMount(data.storage_mount)
      setPendingStorageMount(data.storage_mount)
      clearSelection()
      void refreshUsage(data.current_path)
      const dirPaths = data.content
        .filter((entry: LocalBrowserEntry) => entry.is_dir)
        .map((entry: LocalBrowserEntry) => entry.path)
      void refreshDirSizes(dirPaths, true)
    })
  }

  const init = async () => {
    const resp = await loadStorages()
    handleResp(resp, (data) => {
      setStorages(data)
      if (data.length === 0) {
        notify.warning(t("home.local_browser.no_local_storage"))
        return
      }
      const initPath = data[0].mount_path || "/"
      setPath(initPath)
      setStorageMount(data[0].mount_path)
      setPendingStorageMount(data[0].mount_path)
      void refreshList(initPath, true)
    })
  }

  onMount(() => {
    void init()
  })

  onCleanup(() => {
    clearDirSizeTimer()
  })

  const enterFolder = (entry: LocalBrowserEntry) => {
    if (!entry.is_dir) return
    void refreshList(entry.path, true)
  }

  const goParent = () => {
    if (!path() || path() === "/") return
    void refreshList(pathDir(path()), true)
  }

  const switchStorage = () => {
    const v = pendingStorageMount()
    if (!v || v === storageMount()) return
    setStorageMount(v)
    setPath(v)
    resetFilters()
    void refreshList(v, true)
  }

  const storageOptionLabel = (st: LocalBrowserStorage) => {
    const name = (st.name || "").trim()
    const mount = (st.mount_path || "").trim() || "/"
    if (!name || name === mount) return mount
    return `${name} (${mount})`
  }

  const openInHome = (targetPath: string) => {
    if (!targetPath) return
    setSearchParams({ view: undefined })
    to(targetPath)
  }

  const openCurrentDirInHome = () => openInHome(path())

  const openCopyModal = () => {
    if (selectedCount() === 0) {
      notify.warning(t("home.local_browser.select_files"))
      return
    }
    copyModal.onOpen()
  }

  const openMoveModal = () => {
    if (selectedCount() === 0) {
      notify.warning(t("home.local_browser.select_files"))
      return
    }
    moveModal.onOpen()
  }

  const openDeleteModal = () => {
    if (selectedCount() === 0) {
      notify.warning(t("home.local_browser.select_files"))
      return
    }
    deleteModal.onOpen()
  }

  const doCompress = async () => {
    const names = selectedNames()
    if (names.length === 0) {
      notify.warning(t("home.local_browser.select_files"))
      return
    }
    let targetArchiveName = archiveName().trim()
    if (!targetArchiveName) {
      targetArchiveName = "archive.zip"
    }
    if (!targetArchiveName.toLowerCase().endsWith(".zip")) {
      targetArchiveName = `${targetArchiveName}.zip`
    }
    const resp = await compressReq({
      dir: path(),
      names,
      archive_name: targetArchiveName,
      password: password(),
    })
    handleResp(resp, () => {
      notify.success(t("home.local_browser.compress_success"))
      compressModal.onClose()
      setPassword("")
      void refreshList()
    })
  }

  const doExtract = async () => {
    const targets = selectedExtractTargets()
    if (targets.length === 0) {
      notify.warning(t("home.local_browser.select_extractable_archive_group"))
      return
    }
    const dstDir = extractDstDir().trim() || path()
    const resp = await extractReq({
      archive_path: targets[0].path,
      archive_paths: targets.map((entry) => entry.path),
      dst_dir: dstDir,
      password: password(),
    })
    handleResp(resp, () => {
      notify.success(t("home.local_browser.extract_task_created"))
      extractModal.onClose()
      setPassword("")
      setExtractDstDir("")
      void refreshList()
    })
  }

  const doDelete = async () => {
    const paths = selectedPaths()
    if (paths.length === 0) {
      notify.warning(t("home.local_browser.select_files"))
      return
    }
    const resp = await deleteReq({ paths })
    handleResp(resp, () => {
      notify.success(t("home.local_browser.delete_success"))
      deleteModal.onClose()
      void refreshList()
    })
  }

  const renderRowActions = (entry: LocalBrowserEntry) => {
    return (
      <HStack spacing="$1" wrap="wrap" justifyContent="flex-end">
        <Button
          size="xs"
          variant="outline"
          onClick={() => openInHome(entry.path)}
        >
          {t("home.local_browser.open")}
        </Button>
      </HStack>
    )
  }

  const renderEntryTitle = (entry: LocalBrowserEntry) => {
    return (
      <HStack spacing="$2" minW={0}>
        <Show when={entry.is_dir} fallback={<FiFile size={14} />}>
          <FiFolder size={14} />
        </Show>
        <Text
          cursor="pointer"
          onClick={() =>
            entry.is_dir ? enterFolder(entry) : openInHome(entry.path)
          }
          maxW={{ "@initial": "50vw", "@md": "28vw" }}
          title={entry.name}
          style={{
            overflow: "hidden",
            "text-overflow": "ellipsis",
            "white-space": "nowrap",
          }}
        >
          {entry.name}
        </Text>
      </HStack>
    )
  }

  const renderKindInfo = (entry: LocalBrowserEntry) => {
    return (
      <VStack spacing="1" alignItems="flex-start">
        <Text>{t(`home.local_browser.kind.${entry.kind}`)}</Text>
        <Show when={entry.extractable}>
          <HStack spacing="$1" wrap="wrap">
            <Badge colorScheme="info" variant="subtle">
              {(entry.archive_format || "").toUpperCase()}
            </Badge>
            <Show
              when={entry.archive_group && entry.archive_group !== entry.name}
            >
              <Text
                fontSize="$xs"
                color="$neutral11"
                title={entry.archive_group}
              >
                {t("home.local_browser.grouped_part")}
              </Text>
            </Show>
          </HStack>
        </Show>
      </VStack>
    )
  }

  return (
    <VStack
      w="$full"
      spacing="$3"
      alignItems="stretch"
      p="$2"
      rounded="$xl"
      bg={pageBg()}
    >
      <HStack
        justifyContent="space-between"
        alignItems={{ "@initial": "flex-start", "@md": "center" }}
        direction={{ "@initial": "column", "@md": "row" }}
        spacing="$2"
        px="$2"
      >
        <VStack alignItems="flex-start" spacing="2">
          <Text fontSize="$xl" fontWeight="$semibold">
            {t("home.local_browser.tab")}
          </Text>
          <Text color="$neutral11" fontSize="$sm">
            {t("home.local_browser.supported_extract_formats")}
          </Text>
        </VStack>
        <Badge variant="subtle" colorScheme="info">
          {t("home.local_browser.path")}: {path()}
        </Badge>
      </HStack>
      <VStack
        alignItems="stretch"
        spacing="$3"
        p="$4"
        borderWidth="1px"
        borderColor={lineColor()}
        rounded="$xl"
        bg={cardBg()}
        shadow="$sm"
      >
        <HStack
          justifyContent="space-between"
          alignItems="center"
          wrap="wrap"
          spacing="$2"
        >
          <HStack spacing="$1" wrap="wrap" overflowX="auto">
            <For each={breadcrumbs()}>
              {(item, idx) => (
                <HStack spacing="$1">
                  <Button
                    size="xs"
                    variant="ghost"
                    colorScheme={
                      idx() === breadcrumbs().length - 1 ? "accent" : "neutral"
                    }
                    onClick={() => void refreshList(item.path, true)}
                  >
                    {item.label}
                  </Button>
                  <Show when={idx() < breadcrumbs().length - 1}>
                    <FiChevronRight size={12} />
                  </Show>
                </HStack>
              )}
            </For>
          </HStack>
          <HStack spacing="$2" wrap="wrap">
            <Badge variant="subtle">
              {t("home.local_browser.total")}: {total()}
            </Badge>
            <Badge variant="subtle">
              {t("home.selected")}: {selectedCount()}
            </Badge>
          </HStack>
        </HStack>
        <Show when={usage()}>
          <VStack
            spacing="$1"
            alignItems="stretch"
            p="$2"
            rounded="$lg"
            borderWidth="1px"
            borderColor={lineColor()}
            bg={subtleBg()}
          >
            <HStack justifyContent="space-between" fontSize="$sm" color="$neutral11">
              <Text>
                磁盘已用 {getFileSize(usage()!.used)} / {getFileSize(usage()!.total)}
              </Text>
              <Text>剩余 {getFileSize(usage()!.free)}</Text>
            </HStack>
            <Progress
              value={Math.min(100, Math.max(0, usage()!.used_percent || 0))}
              trackColor="$neutral4"
              rounded="$full"
              size="sm"
              aria-label="local-storage-usage"
            >
              <ProgressIndicator color="$info8" rounded="$full" />
            </Progress>
          </VStack>
        </Show>
        <HStack
          spacing="$2"
          direction={{ "@initial": "column", "@md": "row" }}
          alignItems={{ "@initial": "stretch", "@md": "center" }}
          p="$2"
          rounded="$lg"
          bg={panelBg()}
          borderWidth="1px"
          borderColor={lineColor()}
        >
          <SelectWrapper
            size="sm"
            alwaysShowBorder
            value={pendingStorageMount()}
            onChange={(v) => setPendingStorageMount(v)}
            w={{ "@initial": "$full", "@md": "$72" }}
            options={storages().map((st) => ({
              value: st.mount_path,
              label: storageOptionLabel(st),
            }))}
          />
          <Button
            size="sm"
            colorScheme="accent"
            onClick={switchStorage}
            disabled={
              !pendingStorageMount() || pendingStorageMount() === storageMount()
            }
          >
            {t("home.local_browser.switch_storage")}
          </Button>
          <Input
            aria-label={t("home.local_browser.path")}
            value={path()}
            onInput={(e) => setPath(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void refreshList(path(), true)
            }}
          />
          <Button size="sm" variant="outline" onClick={goParent}>
            {t("home.local_browser.up")}
          </Button>
          <Button size="sm" variant="outline" onClick={openCurrentDirInHome}>
            {t("home.local_browser.open_home")}
          </Button>
          <Button size="sm" variant="outline" onClick={mkdirModal.onOpen}>
            {t("home.toolbar.mkdir")}
          </Button>
          <IconButton
            aria-label={t("global.refresh")}
            icon={<FiRefreshCw />}
            loading={loadListLoading()}
            onClick={() => void refreshList()}
          />
        </HStack>

        <HStack
          spacing="$2"
          direction={{ "@initial": "column", "@md": "row" }}
          alignItems={{ "@initial": "stretch", "@md": "center" }}
          p="$2"
          rounded="$lg"
          bg={panelBg()}
          borderWidth="1px"
          borderColor={lineColor()}
        >
          <HStack
            flex={1}
            spacing="$2"
            px="$3"
            py="$2"
            borderWidth="1px"
            borderColor={lineColor()}
            rounded="$lg"
            bg={searchBg()}
          >
            <FiSearch size={16} />
            <Input
              variant="unstyled"
              placeholder={t("home.local_browser.search_placeholder")}
              value={keyword()}
              onInput={(e) => setKeyword(e.currentTarget.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void refreshList(path(), true)
              }}
            />
          </HStack>
          <Checkbox
            checked={recursive()}
            onChange={(e: Event & { currentTarget: HTMLInputElement }) =>
              setRecursive(e.currentTarget.checked)
            }
          >
            {t("home.local_browser.recursive")}
          </Checkbox>
          <Button
            colorScheme="accent"
            loading={loadListLoading()}
            onClick={() => void refreshList(path(), true)}
          >
            {t("home.local_browser.search")}
          </Button>
          <Show when={hasFilters()}>
            <Button
              variant="ghost"
              onClick={() => {
                resetFilters()
                void refreshList(path(), true)
              }}
            >
              {t("global.clear")}
            </Button>
          </Show>
        </HStack>

        <HStack spacing="$2" wrap="wrap">
          <For each={kindQuickOptions}>
            {(item) => (
              <Button
                size="xs"
                variant={kind() === item ? "solid" : "outline"}
                colorScheme={kind() === item ? "accent" : "neutral"}
                onClick={() => {
                  setKind(item)
                  void refreshList(path(), true)
                }}
              >
                {t(`home.local_browser.kind.${item}`)}
              </Button>
            )}
          </For>
        </HStack>
      </VStack>

      <HStack
        justifyContent="space-between"
        direction={{ "@initial": "column", "@md": "row" }}
        alignItems={{ "@initial": "stretch", "@md": "center" }}
        spacing="$2"
        px="$3"
        py="$2"
        rounded="$lg"
        borderWidth="1px"
        borderColor={lineColor()}
        bg={subtleBg()}
      >
        <HStack spacing="$2" wrap="wrap" color="$neutral11">
          <Text fontSize="$sm">
            {t("home.local_browser.total")}: {total()}
          </Text>
          <Text fontSize="$sm">
            {t("home.selected")}: {selectedCount()}
          </Text>
        </HStack>
        <HStack spacing="$2" wrap="wrap">
          <Button
            size="sm"
            variant="outline"
            disabled={selectedCount() === 0}
            onClick={openCopyModal}
          >
            {t("home.local_browser.copy_to")}
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={selectedCount() === 0}
            onClick={openMoveModal}
          >
            {t("home.local_browser.move_to")}
          </Button>
          <Button
            size="sm"
            colorScheme="danger"
            variant="outline"
            disabled={selectedCount() === 0}
            onClick={openDeleteModal}
          >
            {t("global.delete")}
          </Button>
          <Button
            colorScheme="info"
            size="sm"
            disabled={selectedCount() === 0}
            onClick={() => {
              setArchiveName("archive.zip")
              setPassword("")
              compressModal.onOpen()
            }}
          >
            {t("home.local_browser.compress")} ({selectedCount()})
          </Button>
          <Button
            colorScheme="warning"
            variant="outline"
            size="sm"
            disabled={selectedExtractTargets().length === 0}
            onClick={() => {
              setPassword("")
              setExtractDstDir(path())
              extractModal.onOpen()
            }}
          >
            {t("home.local_browser.extract")}
          </Button>
        </HStack>
        <HStack spacing="$2">
          <Button
            size="sm"
            variant="ghost"
            disabled={page() <= 1 || loadListLoading()}
            onClick={() => {
              const next = Math.max(1, page() - 1)
              setPage(next)
              void refreshList(path(), false)
            }}
          >
            {t("home.local_browser.prev")}
          </Button>
          <Text fontSize="$sm" color="$neutral11">
            {page()} / {totalPage()}
          </Text>
          <Button
            size="sm"
            variant="ghost"
            disabled={page() >= totalPage() || loadListLoading()}
            onClick={() => {
              const next = Math.min(totalPage(), page() + 1)
              setPage(next)
              void refreshList(path(), false)
            }}
          >
            {t("home.local_browser.next")}
          </Button>
        </HStack>
      </HStack>

      <Show
        when={entries().length > 0}
        fallback={
          <Box
            py="$10"
            textAlign="center"
            color="$neutral11"
            borderWidth="1px"
            borderColor={lineColor()}
            rounded="$lg"
            bg={cardBg()}
          >
            <Show
              when={loadListLoading()}
              fallback={t("home.local_browser.empty")}
            >
              {t("home.local_browser.loading")}
            </Show>
          </Box>
        }
      >
        <Box
          display={{ "@initial": "none", "@md": "block" }}
          overflowX="auto"
          borderWidth="1px"
          borderColor={lineColor()}
          rounded="$lg"
          bg={cardBg()}
          px="$2"
          py="$1"
        >
          <Table>
            <Thead>
              <Tr>
                <Th w="$10">
                  <Checkbox
                    aria-label={t("home.local_browser.select_all")}
                    checked={allCurrentChecked()}
                    onChange={(e: Event & { currentTarget: HTMLInputElement }) => {
                      const checked = e.currentTarget.checked
                      const next: Record<string, boolean> = {}
                      for (const entry of entries()) {
                        next[entry.path] = checked
                      }
                      setSelected(next)
                    }}
                  />
                </Th>
                <Th>{t("home.obj.name")}</Th>
                <Th>{t("home.local_browser.kind_label")}</Th>
                <Th>{t("home.obj.size")}</Th>
                <Th>{t("home.obj.modified")}</Th>
                <Th>{t("global.operations")}</Th>
              </Tr>
            </Thead>
            <Tbody>
              <For each={entries()}>
                {(entry) => (
                  <Tr _hover={{ bg: "$neutral3" }}>
                    <Td>
                      <Checkbox
                        aria-label={entry.name}
                        checked={Boolean(selected()[entry.path])}
                        onChange={(e: Event & { currentTarget: HTMLInputElement }) =>
                          setSelectedPath(entry.path, e.currentTarget.checked)
                        }
                      />
                    </Td>
                    <Td>{renderEntryTitle(entry)}</Td>
                    <Td>{renderKindInfo(entry)}</Td>
                    <Td>{renderEntrySize(entry)}</Td>
                    <Td>{formatDate(entry.modified)}</Td>
                    <Td>{renderRowActions(entry)}</Td>
                  </Tr>
                )}
              </For>
            </Tbody>
          </Table>
        </Box>

        <VStack
          display={{ "@initial": "flex", "@md": "none" }}
          spacing="$2"
          alignItems="stretch"
        >
          <For each={entries()}>
            {(entry) => (
              <HStack
                px="$3"
                py="$2"
                borderWidth="1px"
                borderColor={lineColor()}
                rounded="$lg"
                justifyContent="space-between"
                alignItems="center"
                bg={cardBg()}
              >
                <HStack spacing="$2" alignItems="center" minW={0} flex={1}>
                  <Checkbox
                    aria-label={entry.name}
                    checked={Boolean(selected()[entry.path])}
                    onChange={(e: Event & { currentTarget: HTMLInputElement }) =>
                      setSelectedPath(entry.path, e.currentTarget.checked)
                    }
                  />
                  <VStack spacing="1" alignItems="flex-start" minW={0} flex={1}>
                    {renderEntryTitle(entry)}
                    <HStack spacing="$2" wrap="wrap">
                      {renderKindInfo(entry)}
                      <Text fontSize="$xs" color="$neutral11">
                        {renderEntrySize(entry)}
                      </Text>
                    </HStack>
                    <Text fontSize="$xs" color="$neutral11">
                      {formatDate(entry.modified)}
                    </Text>
                    {renderRowActions(entry)}
                  </VStack>
                </HStack>
              </HStack>
            )}
          </For>
        </VStack>
      </Show>

      <ModalFolderChoose
        opened={copyModal.isOpen()}
        onClose={copyModal.onClose}
        defaultValue={path()}
        loading={copyLoading()}
        onSubmit={async (dst) => {
          const resp = await copyReq({ paths: selectedPaths(), dst_dir: dst })
          handleResp(resp, () => {
            notify.success(t("home.local_browser.copy_success"))
            copyModal.onClose()
            void refreshList()
          })
        }}
      />

      <ModalFolderChoose
        opened={moveModal.isOpen()}
        onClose={moveModal.onClose}
        defaultValue={path()}
        loading={moveLoading()}
        onSubmit={async (dst) => {
          const resp = await moveReq({ paths: selectedPaths(), dst_dir: dst })
          handleResp(resp, () => {
            notify.success(t("home.local_browser.move_success"))
            moveModal.onClose()
            void refreshList()
          })
        }}
      />

      <Modal
        blockScrollOnMount={false}
        opened={deleteModal.isOpen()}
        onClose={deleteModal.onClose}
        size={{ "@initial": "xs", "@md": "md" }}
      >
        <ModalOverlay />
        <ModalContent>
          <ModalHeader>{t("global.delete")}</ModalHeader>
          <ModalBody>
            <Text color="$neutral11">
              {t("home.local_browser.delete_confirm")}
            </Text>
          </ModalBody>
          <ModalFooter display="flex" gap="$2">
            <Button onClick={deleteModal.onClose} colorScheme="neutral">
              {t("global.cancel")}
            </Button>
            <Button
              colorScheme="danger"
              loading={deleteLoading()}
              onClick={() => void doDelete()}
            >
              {t("global.confirm")}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      <ModalInput
        title="home.toolbar.input_dir_name"
        opened={mkdirModal.isOpen()}
        onClose={mkdirModal.onClose}
        loading={mkdirLoading()}
        onSubmit={async (name) => {
          const next = name.trim()
          if (!next) return
          const resp = await mkdirReq(pathJoin(path(), next))
          handleResp(resp, () => {
            mkdirModal.onClose()
            void refreshList()
          })
        }}
      />

      <Modal
        opened={compressModal.isOpen()}
        onClose={compressModal.onClose}
        closeOnOverlayClick={!compressLoading()}
        closeOnEsc={!compressLoading()}
      >
        <ModalOverlay />
        <ModalContent>
          <ModalCloseButton />
          <ModalHeader>{t("home.local_browser.compress")}</ModalHeader>
          <ModalBody>
            <VStack spacing="$3" alignItems="stretch">
              <FormControl required>
                <FormLabel>{t("home.local_browser.archive_name")}</FormLabel>
                <Input
                  value={archiveName()}
                  onInput={(e) => setArchiveName(e.currentTarget.value)}
                />
              </FormControl>
              <FormControl>
                <FormLabel>
                  {t("home.local_browser.password_optional")}
                </FormLabel>
                <Input
                  type="password"
                  value={password()}
                  onInput={(e) => setPassword(e.currentTarget.value)}
                />
              </FormControl>
              <Text fontSize="$sm" color="$neutral11">
                {t("home.local_browser.selected_count")}: {selectedCount()}
              </Text>
            </VStack>
          </ModalBody>
          <ModalFooter display="flex" gap="$2">
            <Button variant="ghost" onClick={compressModal.onClose}>
              {t("global.cancel")}
            </Button>
            <Button
              colorScheme="info"
              loading={compressLoading()}
              onClick={() => void doCompress()}
            >
              {t("home.local_browser.compress")}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      <Modal
        opened={extractModal.isOpen()}
        onClose={extractModal.onClose}
        closeOnOverlayClick={!extractLoading()}
        closeOnEsc={!extractLoading()}
      >
        <ModalOverlay />
        <ModalContent>
          <ModalCloseButton />
          <ModalHeader>{t("home.local_browser.extract")}</ModalHeader>
          <ModalBody>
            <VStack spacing="$3" alignItems="stretch">
              <FormControl>
                <FormLabel>{t("home.local_browser.archive")}</FormLabel>
                <VStack alignItems="flex-start" spacing="1">
                  <Text>{selectedExtractPreview()}</Text>
                  <Show when={selectedExtractFormat()}>
                    <Badge colorScheme="info" variant="subtle">
                      {selectedExtractFormat().toUpperCase()}
                    </Badge>
                  </Show>
                  <Show when={selectedCount() > 0}>
                    <Text fontSize="$xs" color="$neutral11">
                      {t("home.local_browser.selected_count")}:{" "}
                      {selectedCount()} · {t("home.local_browser.archive")}:{" "}
                      {selectedExtractTargets().length}
                    </Text>
                  </Show>
                </VStack>
              </FormControl>
              <FormControl>
                <FormLabel>
                  {t("home.local_browser.extract_target_dir")}
                </FormLabel>
                <Input
                  value={extractDstDir()}
                  onInput={(e) => setExtractDstDir(e.currentTarget.value)}
                />
              </FormControl>
              <FormControl>
                <FormLabel>
                  {t("home.local_browser.password_optional")}
                </FormLabel>
                <Input
                  type="password"
                  value={password()}
                  onInput={(e) => setPassword(e.currentTarget.value)}
                />
              </FormControl>
              <Text fontSize="$xs" color="$neutral11">
                {t("home.local_browser.supported_extract_formats")}
              </Text>
            </VStack>
          </ModalBody>
          <ModalFooter display="flex" gap="$2">
            <Button variant="ghost" onClick={extractModal.onClose}>
              {t("global.cancel")}
            </Button>
            <Button
              colorScheme="warning"
              loading={extractLoading()}
              onClick={() => void doExtract()}
            >
              {t("home.local_browser.extract")}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </VStack>
  )
}

export default Browser
