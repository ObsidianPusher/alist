import type { Resp } from "~/types"
import { handleRespWithNotifySuccess } from "~/utils"
import { usePath } from "./usePath"

type AfterSuccess = () => void

export const useFsAction = () => {
  const { refresh } = usePath()

  const submit = <T>(
    resp: Resp<T>,
    afterSuccess?: AfterSuccess,
    refreshAfterSuccess = true,
  ) => {
    handleRespWithNotifySuccess(resp, () => {
      if (refreshAfterSuccess) {
        refresh()
      }
      afterSuccess?.()
    })
  }

  return {
    submit,
  }
}
