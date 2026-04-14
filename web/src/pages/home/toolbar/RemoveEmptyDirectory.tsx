import {
  Button,
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
  ModalOverlay,
  createDisclosure,
} from "@hope-ui/solid"
import { useFetch, useFsAction, useRouter, useT } from "~/hooks"
import { bus } from "~/utils"
import { onCleanup } from "solid-js"
import { removeEmptyDirectoryFs } from "~/api/fs-ops"

export const RemoveEmptyDirectory = () => {
  const { isOpen, onOpen, onClose } = createDisclosure()
  const { pathname } = useRouter()
  const [loading, ok] = useFetch(removeEmptyDirectoryFs)
  const { submit } = useFsAction()
  const handler = (name: string) => {
    if (name === "removeEmptyDirectory") {
      onOpen()
    }
  }
  bus.on("tool", handler)
  onCleanup(() => {
    bus.off("tool", handler)
  })
  const t = useT()
  return (
    <Modal
      blockScrollOnMount={false}
      opened={isOpen()}
      onClose={onClose}
      size={{
        "@initial": "xs",
        "@md": "md",
      }}
    >
      <ModalOverlay />
      <ModalContent>
        <ModalHeader>{t("home.toolbar.remove_empty_directory")}</ModalHeader>
        <ModalBody>
          <p>{t("home.toolbar.remove_empty_directory-tips")}</p>
        </ModalBody>
        <ModalFooter display="flex" gap="$2">
          <Button onClick={onClose} colorScheme="neutral">
            {t("global.cancel")}
          </Button>
          <Button
            colorScheme="danger"
            loading={loading()}
            onClick={async () => {
              const resp = await ok(pathname())
              submit(resp, () => {
                onClose()
              })
            }}
          >
            {t("global.confirm")}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  )
}
