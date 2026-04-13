import { Button, HStack } from "@hope-ui/solid"
import { useCopyLink, useT } from "~/hooks"
import { objStore } from "~/store"
import { FileInfo } from "./info"

export const Download = () => {
  const t = useT()
  const { copyCurrentRawLink } = useCopyLink()
  return (
    <FileInfo>
      <HStack spacing="$2">
        <Button colorScheme="accent" onClick={() => copyCurrentRawLink(true)}>
          {t("home.toolbar.copy_link")}
        </Button>
        <Button as="a" href={objStore.raw_url} target="_blank">
          {t("home.preview.download")}
        </Button>
      </HStack>
    </FileInfo>
  )
}

export default Download
