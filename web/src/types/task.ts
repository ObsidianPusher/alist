export interface TaskInfo {
  id: string
  name: string
  group_id?: string
  is_group?: boolean
  creator: string
  creator_role: number
  state: number
  status: string
  progress: number
  error: string
  display_path?: string
  sub_tasks?: TaskInfo[]
}
