import type { PEmptyResp, PResp, TaskInfo } from "~/types"
import { r } from "~/utils"

export const listTasks = (type: string, done: string): PResp<TaskInfo[]> => {
  return r.get(`/task/${type}/${done}`)
}

export const clearDoneTasks = (type: string): PEmptyResp => {
  return r.post(`/task/${type}/clear_done`)
}

export const clearSucceededTasks = (type: string): PEmptyResp => {
  return r.post(`/task/${type}/clear_succeeded`)
}

export const retryFailedTasks = (type: string): PEmptyResp => {
  return r.post(`/task/${type}/retry_failed`)
}

export const operateTask = (
  type: string,
  operation: "pause" | "delete" | "retry" | "resume",
  taskID: string,
): PEmptyResp => {
  if (operation === "retry" || operation === "resume") {
    return r.post(`/task/${type}/${operation}?tid=${taskID}`)
  }
  return r.post(`/task/${type}/${operation}?tid=${taskID}`)
}
