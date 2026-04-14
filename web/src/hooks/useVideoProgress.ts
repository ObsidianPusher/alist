import { Accessor, createSignal } from "solid-js"
import { getVideoProgress, saveVideoProgress } from "~/api/media"

type VideoProgressMeta = {
  path: string
  size?: number
  modTime?: number
}

export const useVideoProgress = (meta: Accessor<VideoProgressMeta>) => {
  const [resumeVisible, setResumeVisible] = createSignal(false)
  const [resumeAt, setResumeAt] = createSignal(0)
  const [resumeDuration, setResumeDuration] = createSignal(0)
  const [resumeApplied, setResumeApplied] = createSignal(false)

  const resetResume = () => {
    setResumeApplied(false)
    setResumeVisible(false)
    setResumeAt(0)
    setResumeDuration(0)
  }

  const loadResume = async () => {
    resetResume()
    const resp = await getVideoProgress(meta().path)
    if (!resp || resp.code !== 200 || !resp.data) return
    if (resp.data.finished) return
    if (!Number.isFinite(resp.data.position) || resp.data.position <= 5) return
    setResumeAt(resp.data.position)
    setResumeDuration(resp.data.duration || 0)
    setResumeVisible(true)
  }

  const persistProgress = async (position: number, duration: number) => {
    if (!Number.isFinite(position) || position <= 0) return
    const current = meta()
    await saveVideoProgress({
      path: current.path,
      size: current.size,
      mod_time: current.modTime,
      position,
      duration: Number.isFinite(duration) ? duration : 0,
    })
  }

  return {
    resumeVisible,
    resumeAt,
    resumeDuration,
    resumeApplied,
    setResumeVisible,
    setResumeApplied,
    loadResume,
    persistProgress,
    resetResume,
  }
}
