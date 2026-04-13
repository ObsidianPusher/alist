import {
  Badge,
  Button,
  Heading,
  HStack,
  Progress,
  ProgressIndicator,
  Stack,
  Text,
  useColorModeValue,
  VStack,
} from "@hope-ui/solid"
import { createMemo, createSignal, For, Show } from "solid-js"
import { useT, useFetch } from "~/hooks"
import { PEmptyResp, TaskInfo } from "~/types"
import { handleResp, notify, r } from "~/utils"
import { TasksProps } from "./Tasks"

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

const StateMap: Record<
  string,
  | "primary"
  | "accent"
  | "neutral"
  | "success"
  | "info"
  | "warning"
  | "danger"
  | undefined
> = {
  [TaskStateEnum.Failed]: "danger",
  [TaskStateEnum.Succeeded]: "success",
  [TaskStateEnum.Canceled]: "neutral",
}

const stateBucket = (state: number) => {
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

const Creator = (props: { name: string; role: number }) => {
  if (props.role < 0) return null
  const roleColors = ["info", "neutral", "accent"]
  return <Badge colorScheme={roleColors[props.role] as any}>{props.name}</Badge>
}

export const TaskState = (props: { state: number }) => {
  const t = useT()
  return (
    <Badge colorScheme={StateMap[props.state] ?? "info"}>
      {t(`tasks.state.${props.state}`)}
    </Badge>
  )
}

const displayPathByMode = (path: string, mode: "folder" | "path") => {
  if (mode === "path") return path
  const normalized = path.replace(/\/+$/, "")
  const parts = normalized.split("/").filter(Boolean)
  return parts[parts.length - 1] || "/"
}

export const Task = (
  props: TaskInfo &
    TasksProps & {
      readonly?: boolean
      depth?: number
    },
) => {
  const t = useT()
  const depth = () => props.depth ?? 0
  const operateName = props.done === "undone" ? "pause" : "delete"
  const canRetry = props.done === "done" && props.state === TaskStateEnum.Failed
  const canResume =
    props.done === "done" && props.state === TaskStateEnum.Canceled
  const operateText = () =>
    props.done === "undone" ? t("tasks.pause") : t("global.delete")
  const [localExpanded, setLocalExpanded] = createSignal(false)
  const expanded = createMemo(() => {
    const ids = props.expandedIds
    if (ids && typeof (ids as any).has === "function") {
      return (ids as Set<string>).has(props.id)
    }
    return localExpanded()
  })
  const toggleExpand = () => {
    if (props.onToggleExpand) {
      props.onToggleExpand(props.id)
      return
    }
    setLocalExpanded(!localExpanded())
  }
  const childTasks = createMemo(() => {
    const tasks = props.sub_tasks ?? []
    if (!tasks.length) return []
    const seen = new Set<string>([props.id])
    const deduped: TaskInfo[] = []
    for (const task of tasks) {
      if (!task || !task.id || seen.has(task.id)) continue
      seen.add(task.id)
      deduped.push(task)
    }
    deduped.sort((a, b) => {
      const bucketDiff = stateBucket(a.state) - stateBucket(b.state)
      if (bucketDiff !== 0) return bucketDiff
      if (a.state !== b.state) return a.state - b.state
      return a.id > b.id ? 1 : -1
    })
    return deduped
  })
  const hasSubTasks = createMemo(() => childTasks().length > 0)
  const displayName = createMemo(() => {
    if (!props.display_path) return props.name
    const mode = props.nameMode || "folder"
    const base = displayPathByMode(props.display_path, mode)
    if (hasSubTasks()) {
      return `${base} (${childTasks().length})`
    }
    return base
  })
  const progress = () => {
    const raw = Number(props.progress)
    if (!Number.isFinite(raw)) {
      return props.state === TaskStateEnum.Succeeded ? 100 : 0
    }
    const clamped = Math.max(0, Math.min(100, raw))
    if (clamped >= 100 && props.state !== TaskStateEnum.Succeeded) {
      return 99.9
    }
    return clamped
  }
  const progressText = createMemo(() => `${progress().toFixed(1)}%`)
  const speedText = createMemo(() => props.getSpeedText?.(props.id) || "")
  const displayStatus = createMemo(() => {
    if (!hasSubTasks()) return props.status
    const tasks = childTasks()
    let running = 0
    let unfinished = 0
    let finished = 0
    for (const task of tasks) {
      const bucket = stateBucket(task.state)
      if (bucket === 0) running++
      else if (bucket === 1) unfinished++
      else finished++
    }
    return `${t("tasks.group.running")}: ${running} · ${t("tasks.group.unfinished")}: ${unfinished} · ${t("tasks.group.finished")}: ${finished}`
  })
  const [operateLoading, operate] = useFetch(
    (): PEmptyResp =>
      r.post(`/task/${props.type}/${operateName}?tid=${props.id}`),
  )
  const [retryLoading, retry] = useFetch(
    (): PEmptyResp => r.post(`/task/${props.type}/retry?tid=${props.id}`),
  )
  const [resumeLoading, resume] = useFetch(
    (): PEmptyResp => r.post(`/task/${props.type}/resume?tid=${props.id}`),
  )
  const [deleted, setDeleted] = createSignal(false)
  return (
    <Show when={!deleted()}>
      <Stack
        bgColor={useColorModeValue("$background", "$neutral3")()}
        w="$full"
        shadow="$md"
        rounded="$lg"
        p={{ "@initial": "$2", "@md": "$3" }}
        direction={{ "@initial": "column", "@lg": "row" }}
        alignItems={{ "@initial": "stretch", "@lg": "center" }}
        gap="$2"
      >
        <VStack w="$full" minW={0} alignItems="start" spacing="$2">
          <Heading
            size="sm"
            css={{
              wordBreak: "break-all",
            }}
          >
            {displayName()}
          </Heading>
          <Creator name={props.creator} role={props.creator_role} />
          <HStack gap="$2" flexWrap="wrap">
            <TaskState state={props.state} />
            <Show when={hasSubTasks() && depth() < 4}>
              <Button
                size="xs"
                variant="outline"
                onClick={toggleExpand}
                aria-expanded={expanded()}
                aria-label={expanded() ? t("tasks.fold") : t("tasks.expand")}
              >
                {expanded() ? t("tasks.fold") : t("tasks.expand")}
              </Button>
            </Show>
          </HStack>
          <Text
            css={{
              wordBreak: "break-all",
              lineHeight: 1.5,
            }}
          >
            {displayStatus()}
          </Text>
          <HStack gap="$3" flexWrap="wrap">
            <Text color="$neutral11" fontSize="$sm">
              {progressText()}
            </Text>
            <Show when={speedText()}>
              <Text color="$neutral10" fontSize="$sm">
                {speedText()}
              </Text>
            </Show>
          </HStack>
          <Show when={props.error}>
            <Text color="$danger9" css={{ wordBreak: "break-all" }}>
              {props.error}
            </Text>
          </Show>
          <Progress
            w="$full"
            trackColor="$info3"
            rounded="$full"
            size="sm"
            value={progress()}
            aria-label={`${displayName()} progress`}
          >
            <ProgressIndicator color="$info8" rounded="$md" />
            {/* <ProgressLabel /> */}
          </Progress>
          <Show when={expanded() && hasSubTasks()}>
            <VStack
              w="$full"
              alignItems="start"
              spacing="$2"
              pl={{ "@initial": "$2", "@md": "$3" }}
              borderLeft="1px solid $neutral5"
            >
              <For each={childTasks()}>
                {(subTask) => (
                  <Task
                    {...subTask}
                    type={props.type}
                    done={props.done}
                    canRetry={props.canRetry}
                    readonly
                    nameMode={props.nameMode}
                    expandedIds={props.expandedIds}
                    onToggleExpand={props.onToggleExpand}
                    getSpeedText={props.getSpeedText}
                    depth={depth() + 1}
                  />
                )}
              </For>
            </VStack>
          </Show>
        </VStack>

        <Show when={!props.readonly}>
          <Stack
            direction={{ "@initial": "row", "@lg": "column" }}
            justifyContent={{ "@initial": "flex-end", "@lg": "center" }}
            alignSelf={{ "@initial": "flex-end", "@lg": "center" }}
            w={{ "@initial": "$full", "@lg": "auto" }}
            spacing="$1"
          >
            <Show when={props.canRetry}>
              <Button
                disabled={!canResume}
                display={canResume ? "block" : "none"}
                loading={resumeLoading()}
                aria-label={t("tasks.resume")}
                onClick={async () => {
                  const resp = await resume()
                  handleResp(resp, () => {
                    notify.info(t("tasks.resume"))
                    setDeleted(true)
                  })
                }}
              >
                {t(`tasks.resume`)}
              </Button>
              <Button
                disabled={!canRetry}
                display={canRetry ? "block" : "none"}
                loading={retryLoading()}
                aria-label={t("tasks.retry")}
                onClick={async () => {
                  const resp = await retry()
                  handleResp(resp, () => {
                    notify.info(t("tasks.retry"))
                    setDeleted(true)
                  })
                }}
              >
                {t(`tasks.retry`)}
              </Button>
            </Show>
            <Button
              colorScheme="danger"
              loading={operateLoading()}
              aria-label={operateText()}
              onClick={async () => {
                const resp = await operate()
                handleResp(resp, () => {
                  notify.success(
                    props.done === "undone"
                      ? t("tasks.pause")
                      : t("global.delete_success"),
                  )
                  setDeleted(true)
                })
              }}
            >
              {operateText()}
            </Button>
          </Stack>
        </Show>
      </Stack>
    </Show>
  )
}
