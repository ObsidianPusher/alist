import { createDisclosure } from "@hope-ui/solid"
import { ModalInput } from "~/components"
import { useFetch, useFsAction, useRouter } from "~/hooks"
import { bus, pathJoin } from "~/utils"
import { onCleanup } from "solid-js"
import { mkdirFs } from "~/api/fs-ops"

export const Mkdir = () => {
  const { isOpen, onOpen, onClose } = createDisclosure()
  const [loading, ok] = useFetch(mkdirFs)
  const { pathname } = useRouter()
  const { submit } = useFsAction()
  const handler = (name: string) => {
    if (name === "mkdir") {
      onOpen()
    }
  }
  bus.on("tool", handler)
  onCleanup(() => {
    bus.off("tool", handler)
  })
  return (
    <ModalInput
      title="home.toolbar.input_dir_name"
      opened={isOpen()}
      onClose={onClose}
      loading={loading()}
      onSubmit={async (name) => {
        const resp = await ok(pathJoin(pathname(), name))
        submit(resp, () => {
          onClose()
        })
      }}
    />
  )
}
