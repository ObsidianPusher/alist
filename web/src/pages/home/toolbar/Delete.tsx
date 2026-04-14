import {
  Modal,
  ModalOverlay,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Button,
  createDisclosure,
} from "@hope-ui/solid"
import { onCleanup } from "solid-js"
import { useFetch, useFsAction, useRouter, useT } from "~/hooks"
import { selectedObjs } from "~/store"
import { bus } from "~/utils"
import { removeFs } from "~/api/fs-ops"

export const Delete = () => {
  const t = useT()
  const { isOpen, onOpen, onClose } = createDisclosure()
  const [loading, ok] = useFetch(removeFs)
  const { submit } = useFsAction()
  const { pathname } = useRouter()
  const handler = (name: string) => {
    if (name === "delete") {
      onOpen()
    }
  }
  bus.on("tool", handler)
  onCleanup(() => {
    bus.off("tool", handler)
  })
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
        <ModalHeader>{t("home.toolbar.delete")}</ModalHeader>
        <ModalBody>
          <p>{t("home.toolbar.delete-tips")}</p>
        </ModalBody>
        <ModalFooter display="flex" gap="$2">
          <Button onClick={onClose} colorScheme="neutral">
            {t("global.cancel")}
          </Button>
          <Button
            colorScheme="danger"
            loading={loading()}
            onClick={async () => {
              const resp = await ok(
                pathname(),
                selectedObjs().map((obj) => obj.name),
              )
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
