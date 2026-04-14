import { Box } from "@hope-ui/solid"
import { MaybeLoading } from "~/components"
import { useFetch, useRouter } from "~/hooks"
import { password } from "~/store"
import { handleResp } from "~/utils"
import { getAliyunDocPreview } from "~/api/fs-other"

const AliDocPreview = () => {
  const { pathname } = useRouter()
  const [loading, post] = useFetch(() =>
    getAliyunDocPreview(pathname(), password()),
  )
  const init = async () => {
    const resp = await post()
    handleResp(resp, (data) => {
      const docOptions = aliyun.config({
        mount: document.querySelector("#office-preview")!,
        url: data.preview_url,
      })
      docOptions.setToken({ token: data.access_token })
    })
  }
  init()
  return (
    <MaybeLoading loading={loading()}>
      <Box w="$full" h="70vh" id="office-preview"></Box>
    </MaybeLoading>
  )
}

export default AliDocPreview
