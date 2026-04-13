import { Button, Heading, HStack, VStack } from "@hope-ui/solid"
import { createMemo, createSignal, For, onCleanup, Show } from "solid-js"
import { Paginator } from "~/components"
import { useFetch, useT } from "~/hooks"
import { PEmptyResp, PResp, TaskInfo } from "~/types"
import { handleResp, r } from "~/utils"
import { Task } from "./Task"

enum TaskStateEnum {
  Pending,
  Running,
  Succeeded,
  Canceling,
  Canceled,
  Errored,
  Failing,
  Failed,
  WaitingRetry,
  BeforeRetry,
}

const taskStateBucket = (state: number) => {
  if (
    state === TaskStateEnum.Running ||
    state === TaskStateEnum.Canceling ||
    state === TaskStateEnum.Failing
  ) {
    return 0
  }
  if (
    state === TaskStateEnum.Pending ||
    state === TaskStateEnum.Errored ||
    state === TaskStateEnum.WaitingRetry ||
    state === TaskStateEnum.BeforeRetry
  ) {
    return 1
  }
  return 2
}

export interface TasksProps {
  type: string
  done: string
  canRetry?: boolean
  nameMode?: "folder" | "path"
  expandedIds?: Set<string>
  onToggleExpand?: (id: string) => void
  getSpeedText?: (id: string) => string | undefined
}
export const Tasks = (props: TasksProps) => {
  const t = useT()
  const [loading, get] = useFetch(
    (): PResp<TaskInfo[]> => r.get(`/task/${props.type}/${props.done}`),
  )
  const [tasks, setTasks] = createSignal<TaskInfo[]>([])
  const [speedMap, setSpeedMap] = createSignal<Map<string, string>>(new Map())
  const lastSamples = new Map<string, { progress: number; ts: number }>()
  const speedFromStatus = (status?: string) => {
    if (!status) return ""
    const match = status.match(
      /(\d+(?:\.\d+)?)\s*(B\/s|KB\/s|MB\/s|GB\/s|KiB\/s|MiB\/s|GiB\/s)/i,
    )
    if (!match) return ""
    return `${match[1]} ${match[2]}`
  }
  const flattenTasks = (items: TaskInfo[], out: TaskInfo[]) => {
    for (const task of items) {
      out.push(task)
      if (task.sub_tasks?.length) {
        flattenTasks(task.sub_tasks, out)
      }
    }
  }
  const refresh = async () => {
    const resp = await get()
    handleResp(resp, (data) =>
      {
        const sorted =
          data?.sort((a, b) => {
            if (a.id > b.id) {
              return 1
            }
            return -1
          }) ?? []
        setTasks(sorted)
        const now = Date.now()
        const flat: TaskInfo[] = []
        flattenTasks(sorted, flat)
        const nextMap = new Map<string, string>()
        for (const task of flat) {
          if (!task?.id) continue
          const explicit = speedFromStatus(task.status)
          if (explicit) {
            nextMap.set(task.id, explicit)
            lastSamples.set(task.id, { progress: Number(task.progress) || 0, ts: now })
            continue
          }
          const prev = lastSamples.get(task.id)
          const curProgress = Number(task.progress) || 0
          if (!prev || now <= prev.ts) {
            lastSamples.set(task.id, { progress: curProgress, ts: now })
            continue
          }
          const delta = curProgress - prev.progress
          const dt = (now - prev.ts) / 1000
          lastSamples.set(task.id, { progress: curProgress, ts: now })
          if (dt <= 0) continue
          const rate = delta / dt
          if (!Number.isFinite(rate) || rate <= 0) continue
          const text = rate >= 1 ? `${rate.toFixed(1)}%/s` : `${(rate * 60).toFixed(1)}%/min`
          nextMap.set(task.id, text)
        }
        setSpeedMap(nextMap)
      },
    )
  }
  refresh()
  if (props.done === "undone") {
    const interval = setInterval(refresh, 2000)
    onCleanup(() => clearInterval(interval))
  }
  const [clearDoneLoading, clearDone] = useFetch(
    (): PEmptyResp => r.post(`/task/${props.type}/clear_done`),
  )
  const [clearSucceededLoading, clearSucceeded] = useFetch(
    (): PEmptyResp => r.post(`/task/${props.type}/clear_succeeded`),
  )
  const [retryFailedLoading, retryFailed] = useFetch(
    (): PEmptyResp => r.post(`/task/${props.type}/retry_failed`),
  )
  const [page, setPage] = createSignal(1)
  const pageSize = 20
  const summary = createMemo(() => {
    const leafTasks: TaskInfo[] = []
    const collectLeafTasks = (items: TaskInfo[]) => {
      for (const item of items) {
        if (!item) continue
        if (item.sub_tasks?.length) {
          collectLeafTasks(item.sub_tasks)
          continue
        }
        leafTasks.push(item)
      }
    }
    collectLeafTasks(tasks())
    let running = 0
    let unfinished = 0
    let finished = 0
    for (const task of leafTasks) {
      const bucket = taskStateBucket(task.state)
      if (bucket === 0) running++
      else if (bucket === 1) unfinished++
      else finished++
    }
    return {
      total: leafTasks.length,
      running,
      unfinished,
      finished,
    }
  })
  const curTasks = createMemo(() => {
    const start = (page() - 1) * pageSize
    const end = start + pageSize
    return tasks().slice(start, end)
  })
  return (
    <VStack w="$full" alignItems="start" spacing="$2">
      <Heading size="lg">{t(`tasks.${props.done}`)}</Heading>
      <Show when={props.done === "done"}>
        <HStack gap="$2" flexWrap="wrap">
          <Button colorScheme="accent" loading={loading()} onClick={refresh}>
            {t(`global.refresh`)}
          </Button>
          <Button
            loading={retryFailedLoading()}
            onClick={async () => {
              const resp = await retryFailed()
              handleResp(resp, () => refresh())
            }}
          >
            {t(`tasks.retry_failed`)}
          </Button>
          <Button
            colorScheme="danger"
            loading={clearDoneLoading()}
            onClick={async () => {
              const resp = await clearDone()
              handleResp(resp, () => refresh())
            }}
          >
            {t(`global.clear`)}
          </Button>
          <Button
            colorScheme="success"
            loading={clearSucceededLoading()}
            onClick={async () => {
              const resp = await clearSucceeded()
              handleResp(resp, () => refresh())
            }}
          >
            {t(`tasks.clear_succeeded`)}
          </Button>
        </HStack>
      </Show>
      <HStack gap="$2" flexWrap="wrap">
        <Heading size="sm">
          {`${t("tasks.group.running")}: ${summary().running}`}
        </Heading>
        <Heading size="sm">
          {`${t("tasks.group.unfinished")}: ${summary().unfinished}`}
        </Heading>
        <Heading size="sm">
          {`${t("tasks.group.finished")}: ${summary().finished}`}
        </Heading>
        <Heading size="sm">{`${t("tasks.summary.total")}: ${summary().total}`}</Heading>
      </HStack>
      <VStack w="$full" spacing="$2">
        <For each={curTasks()}>
          {(task) => (
            <Task
              {...task}
              {...props}
              nameMode={props.nameMode}
              getSpeedText={(id) => speedMap().get(id)}
            />
          )}
        </For>
      </VStack>
      <Paginator
        total={tasks().length}
        defaultPageSize={pageSize}
        onChange={(p) => {
          setPage(p)
        }}
      />
    </VStack>
  )
}

export const TypeTasks = (props: { type: string; canRetry?: boolean }) => {
  const t = useT()
  const storageKey = `task_name_mode_${props.type}`
  const expandedStorageKey = `task_expanded_${props.type}`
  const [nameMode, setNameMode] = createSignal<"folder" | "path">(
    (localStorage.getItem(storageKey) as "folder" | "path") || "folder",
  )
  const loadExpandedIds = (): Set<string> => {
    try {
      const raw = localStorage.getItem(expandedStorageKey)
      if (!raw) return new Set<string>()
      const parsed = JSON.parse(raw)
      if (!Array.isArray(parsed)) return new Set<string>()
      return new Set<string>(parsed.filter((v) => typeof v === "string"))
    } catch {
      return new Set<string>()
    }
  }
  const [expandedIds, setExpandedIds] = createSignal<Set<string>>(loadExpandedIds())
  const updateMode = (mode: "folder" | "path") => {
    setNameMode(mode)
    localStorage.setItem(storageKey, mode)
  }
  const toggleExpand = (id: string) => {
    if (!id) return
    setExpandedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      localStorage.setItem(expandedStorageKey, JSON.stringify([...next]))
      return next
    })
  }
  return (
    <VStack w="$full" alignItems="start" spacing="$4">
      <Heading size="xl">{t(`tasks.${props.type}`)}</Heading>
      <HStack gap="$2">
        <Button
          size="sm"
          variant={nameMode() === "folder" ? "solid" : "outline"}
          onClick={() => updateMode("folder")}
        >
          {t("tasks.name_mode_folder")}
        </Button>
        <Button
          size="sm"
          variant={nameMode() === "path" ? "solid" : "outline"}
          onClick={() => updateMode("path")}
        >
          {t("tasks.name_mode_path")}
        </Button>
      </HStack>
      <VStack w="$full" spacing="$2">
        <For each={["undone", "done"]}>
          {(done) => (
            <Tasks
              type={props.type}
              done={done}
              canRetry={props.canRetry}
              nameMode={nameMode()}
              expandedIds={expandedIds()}
              onToggleExpand={toggleExpand}
            />
          )}
        </For>
      </VStack>
    </VStack>
  )
}
