import {
  Alert,
  AlertIcon,
  Box,
  Button,
  Heading,
  HStack,
  Text,
  VStack,
} from "@hope-ui/solid"
import { createMemo, createSignal, For, Show } from "solid-js"
import { MaybeLoading } from "~/components"
import { useFetch, useRouter, useT, useUtil } from "~/hooks"
import { handleResp, joinBase, notify } from "~/utils"
import {
  Addition,
  DriverConfig,
  DriverItem,
  PResp,
  Storage,
  Type,
} from "~/types"
import { getDriverInfo, getStorage, listDrivers, saveStorage, type DriverInfo, type Drivers } from "~/api/admin/storages"
import { createStore, produce } from "solid-js/store"
import { Item } from "./Item"
import { ResponsiveGrid } from "../common/ResponsiveGrid"

function GetDefaultValue(type: Type, value?: string) {
  switch (type) {
    case Type.Bool:
      if (value) {
        return value === "true"
      }
      return false
    case Type.Number:
      if (value) {
        return parseInt(value)
      }
      return 0
    default:
      if (value) {
        return value
      }
      return ""
  }
}
const AddOrEdit = () => {
  const t = useT()
  const { params, back, to } = useRouter()
  const { id } = params
  const [driversLoading, loadDrivers] = useFetch(
    (): PResp<Drivers> => listDrivers(),
    true,
  )
  const [drivers, setDrivers] = createSignal<Drivers>({})
  const initAdd = async () => {
    const resp = await loadDrivers()
    handleResp(resp, setDrivers)
  }

  const [storageLoading, loadStorage] = useFetch(
    (): PResp<Storage> => getStorage(id!),
    true,
  )
  const [driverLoading, loadDriver] = useFetch(
    (): PResp<DriverInfo> => getDriverInfo(storage.driver),
    true,
  )
  const initEdit = async () => {
    const storageResp = await loadStorage()
    handleResp(storageResp, async (storageData) => {
      setStorage(storageData)
      setAddition(JSON.parse(storageData.addition))
      const driverResp = await loadDriver()
      handleResp(driverResp, (driverData) =>
        setDrivers({ [storage.driver]: driverData }),
      )
    })
  }
  if (id) {
    initEdit()
  } else {
    initAdd()
  }
  const [storage, setStorage] = createStore<Storage>({} as Storage)
  const [addition, setAddition] = createStore<Addition>({})
  const { copy } = useUtil()
  const [okLoading, ok] = useFetch((): PResp<{ id: number }> => {
    setStorage("addition", JSON.stringify(addition))
    return saveStorage(id, storage)
  })
  const alert = createMemo(() => {
    const i = drivers()[storage.driver]?.config.alert
    if (i) {
      return i.split("|")[0]
    }
  })
  const baiduAuthorizeUrl = createMemo(() => {
    if (storage.driver !== "BaiduNetdisk") {
      return ""
    }
    const clientId = (addition as any).client_id?.toString().trim()
    if (!clientId) {
      return ""
    }
    const redirectUri =
      (addition as any).redirect_uri?.toString().trim() || "oob"
    const params = new URLSearchParams({
      response_type: "code",
      client_id: clientId,
      redirect_uri: redirectUri,
      scope: "basic,netdisk",
    })
    return `https://openapi.baidu.com/oauth/2.0/authorize?${params.toString()}`
  })
  return (
    <MaybeLoading
      loading={id ? storageLoading() || driverLoading() : driversLoading()}
    >
      <Heading mb="$2">{t(`global.${id ? "edit" : "add"}`)}</Heading>
      <VStack mb="$2" spacing="$2">
        <Item
          name="driver"
          default=""
          readonly={id !== undefined}
          required
          searchable
          type={Type.Select}
          options={id ? storage.driver : Object.keys(drivers()).join(",")}
          value={storage.driver}
          full_name_path="storages.common.driver"
          options_prefix="drivers.drivers"
          driver="drivers"
          onChange={(value) => {
            for (const item of drivers()[value].common) {
              setStorage(
                item.name as keyof Storage,
                GetDefaultValue(item.type, item.default) as any,
              )
            }
            // clear addition first
            setAddition(
              produce((addition) => {
                for (const key in addition) {
                  delete addition[key]
                }
              }),
            )
            for (const item of drivers()[value].additional) {
              setAddition(
                item.name,
                GetDefaultValue(item.type, item.default) as any,
              )
            }
            setStorage("driver", value)
          }}
        />
        <Show when={alert()}>
          <Alert status={alert() as any} w="$full">
            <AlertIcon />
            {t(`drivers.config.${storage.driver}.alert`)}
          </Alert>
        </Show>
        <Show when={storage.driver === "BaiduNetdisk"}>
          <Box
            w="$full"
            p="$3"
            borderRadius="$lg"
            border="1px solid $neutral7"
            backgroundColor="$neutral2"
          >
            <Text fontWeight="$medium" mb="$1">
              {t("drivers.BaiduNetdisk.auth_url")}
            </Text>
            <Text fontSize="$sm" color="$neutral11" mb="$2">
              {t("drivers.BaiduNetdisk.auth_url-tips")}
            </Text>
            <HStack spacing="$2" alignItems="start" wrap="wrap">
              <Text
                css={{
                  fontFamily: "monospace",
                  wordBreak: "break-all",
                  flex: 1,
                }}
                fontSize="$sm"
              >
                {baiduAuthorizeUrl() || "-"}
              </Text>
              <Button
                size="sm"
                colorScheme="accent"
                onClick={() => {
                  if (baiduAuthorizeUrl()) {
                    copy(baiduAuthorizeUrl())
                    notify.success(t("global.copied"))
                  }
                }}
                disabled={!baiduAuthorizeUrl()}
              >
                {t("global.copy")}
              </Button>
            </HStack>
          </Box>
        </Show>
      </VStack>
      <ResponsiveGrid>
        <Show when={drivers()[storage.driver]}>
          <For each={drivers()[storage.driver].common}>
            {(item) => (
              <Item
                {...item}
                driver="common"
                value={(storage as any)[item.name]}
                onChange={(val: any) => {
                  setStorage(item.name as keyof Storage, val)
                }}
              />
            )}
          </For>
          <For
            each={drivers()[storage.driver].additional}
          >
            {(item) => (
              <Item
                {...item}
                driver={storage.driver}
                value={addition[item.name] as any}
                onChange={(val: any) => {
                  setAddition(item.name, val)
                }}
              />
            )}
          </For>
        </Show>
      </ResponsiveGrid>
      <Button
        mt="$2"
        loading={okLoading()}
        onClick={async () => {
          if (drivers()[storage.driver].config.need_ms) {
            notify.info(t("manage.add_storage-tips"))
            window.open(joinBase("/@manage/messenger"), "_blank")
          }
          const resp = await ok()
          // TODO maybe can use handleRrespWithNotifySuccess
          handleResp(
            resp,
            () => {
              notify.success(t("global.save_success"))
              back()
            },
            (msg, code) => {
              if (resp.data.id) {
                to(`/@manage/storages/edit/${resp.data.id}`)
              }
            },
          )
        }}
      >
        {t(`global.${id ? "save" : "add"}`)}
      </Button>
    </MaybeLoading>
  )
}

export default AddOrEdit
