import { Error, FullLoading, ImageWithError } from "~/components"
import { useLink, useT } from "~/hooks"
import { objStore } from "~/store"

const Preview = () => {
  const t = useT()
  const { proxyLink, rawLink } = useLink()
  const isLocalProvider = () => /Local/i.test(objStore.provider || "")
  const src = () => {
    if (isLocalProvider()) {
      return objStore.raw_url || rawLink(objStore.obj, true)
    }
    return proxyLink(objStore.obj, true)
  }
  return (
    <ImageWithError
      maxH="75vh"
      rounded="$lg"
      src={src()}
      fallback={<FullLoading />}
      fallbackErr={<Error msg={t("home.preview.failed_load_img")} />}
    />
  )
}

export default Preview
