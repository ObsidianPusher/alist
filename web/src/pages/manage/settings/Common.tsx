import { useFetch, useT, useRouter, useManageTitle } from "~/hooks"
import { Group, SettingItem, PResp, PEmptyResp, EmptyResp } from "~/types"
import { r, notify, getTarget, handleResp } from "~/utils"
import { createStore } from "solid-js/store"
import { Box, Button, HStack, VStack } from "@hope-ui/solid"
import { createSignal, Index, Show } from "solid-js"
import { Item } from "./SettingItem"
import { ResponsiveGrid } from "../common/ResponsiveGrid"
import MemoryUsage from "./MemoryUsage"
import BaiduMetrics from "./BaiduMetrics"

export interface CommonSettingsProps {
  group: Group
}
const CommonSettings = (props: CommonSettingsProps) => {
  const t = useT()
  const { pathname } = useRouter()
  useManageTitle(`manage.sidemenu.${pathname().split("/").pop()}`)
  const [settingsLoading, getSettings] = useFetch(
    (): PResp<SettingItem[]> =>
      r.get(`/admin/setting/list?group=${props.group}`),
  )
  const [settings, setSettings] = createStore<SettingItem[]>([])
  const refresh = async () => {
    const resp = await getSettings()
    handleResp(resp, setSettings)
  }
  refresh()
  const [saveLoading, saveSettings] = useFetch(
    (): PEmptyResp => r.post("/admin/setting/save", getTarget(settings)),
  )
  const [loading, setLoading] = createSignal(false)
  const [activeTab, setActiveTab] = createSignal<"memory" | "baidu">("memory")
  return (
    <VStack w="$full" alignItems="start" spacing="$2">
      <Show when={props.group === Group.GLOBAL}>
        <Box
          w="$full"
          p="$2"
          borderRadius="$2xl"
          bg="linear-gradient(135deg, rgba(15, 23, 42, 0.06), rgba(15, 23, 42, 0.02))"
          border="1px solid $neutral5"
          boxShadow="0 18px 48px rgba(15, 23, 42, 0.08)"
        >
          <HStack spacing="$2" wrap="wrap">
            <Button
              variant={activeTab() === "memory" ? "solid" : "ghost"}
              colorScheme={activeTab() === "memory" ? "accent" : "neutral"}
              borderRadius="$full"
              onClick={() => setActiveTab("memory")}
            >
              {t("settings.global_tab_memory")}
            </Button>
            <Button
              variant={activeTab() === "baidu" ? "solid" : "ghost"}
              colorScheme={activeTab() === "baidu" ? "accent" : "neutral"}
              borderRadius="$full"
              onClick={() => setActiveTab("baidu")}
            >
              {t("settings.global_tab_baidu")}
            </Button>
          </HStack>
        </Box>
        <Show when={activeTab() === "memory"} fallback={<BaiduMetrics />}>
          <MemoryUsage />
        </Show>
      </Show>
      <ResponsiveGrid>
        <Index each={settings}>
          {(item, _) => (
            <Item
              {...item()}
              onChange={(val) => {
                setSettings((i) => item().key === i.key, "value", val)
              }}
              onDelete={async () => {
                setLoading(true)
                const resp: EmptyResp = await r.post(
                  `/admin/setting/delete?key=${item().key}`,
                )
                setLoading(false)
                handleResp(resp, () => {
                  notify.success(t("global.delete_success"))
                  refresh()
                })
              }}
            />
          )}
        </Index>
      </ResponsiveGrid>
      <HStack spacing="$2">
        <Button
          colorScheme="accent"
          onClick={refresh}
          loading={settingsLoading() || loading()}
        >
          {t("global.refresh")}
        </Button>
        <Button
          loading={saveLoading()}
          onClick={async () => {
            const resp = await saveSettings()
            handleResp(resp, () => notify.success(t("global.save_success")))
          }}
        >
          {t("global.save")}
        </Button>
      </HStack>
    </VStack>
  )
}

export default CommonSettings
