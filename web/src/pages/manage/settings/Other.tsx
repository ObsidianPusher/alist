import { Button, Heading, HStack, Input } from "@hope-ui/solid"
import { createSignal, onMount } from "solid-js"
import { MaybeLoading } from "~/components"
import { useFetch, useManageTitle, useT, useUtil } from "~/hooks"
import { Group, SettingItem, PResp } from "~/types"
import { handleResp, notify } from "~/utils"
import { listSettingsByGroups, resetSettingToken } from "~/api/admin/settings"

const OtherSettings = () => {
  const t = useT()
  useManageTitle("manage.sidemenu.other")
  const [token, setToken] = createSignal("")
  const [settingsLoading, settingsData] = useFetch(
    (): PResp<SettingItem[]> => listSettingsByGroups(Group.SINGLE),
  )
  const refresh = async () => {
    const resp = await settingsData()
    handleResp(resp, (data) => {
      setToken(data.find((i) => i.key === "token")?.value || "")
    })
  }
  onMount(() => {
    refresh()
  })
  const [resetTokenLoading, resetToken] = useFetch(resetSettingToken)
  const { copy } = useUtil()

  return (
    <MaybeLoading loading={settingsLoading()}>
      <Heading my="$2">{t("settings.token")}</Heading>
      <Input value={token()} readOnly />
      <HStack my="$2" spacing="$2">
        <Button
          onClick={() => {
            copy(token())
          }}
        >
          {t("settings_other.copy_token")}
        </Button>
        <Button
          colorScheme="danger"
          loading={resetTokenLoading()}
          onClick={async () => {
            const resp = await resetToken()
            handleResp(resp, (data) => {
              notify.success(t("settings_other.reset_token_success"))
              setToken(data)
            })
          }}
        >
          {t("settings_other.reset_token")}
        </Button>
      </HStack>
    </MaybeLoading>
  )
}

export default OtherSettings
