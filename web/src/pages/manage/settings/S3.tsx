import {
  Box,
  Button,
  FormControl,
  FormLabel,
  Heading,
  HStack,
  Input,
  Select,
  SelectContent,
  SelectIcon,
  SelectListbox,
  SelectOption,
  SelectOptionIndicator,
  SelectOptionText,
  SelectPlaceholder,
  SelectTrigger,
  SelectValue,
  SimpleGrid,
  Text,
  VStack,
} from "@hope-ui/solid"
import { createMemo, createSignal, For, onMount } from "solid-js"
import { MaybeLoading } from "~/components"
import { useFetch, useManageTitle, useT, useUtil } from "~/hooks"
import { Group, PEmptyResp, PResp, SettingItem, Type } from "~/types"
import { handleResp, notify } from "~/utils"
import { listSettingsByGroup, saveSettings as saveSettingsApi } from "~/api/admin/settings"
import { S3BucketItem, type S3Bucket } from "./S3BucketItem"

type S3SettingMap = Record<string, string>
type S3SettingKey =
  | "s3_endpoint"
  | "s3_region"
  | "s3_port"
  | "s3_access_key_id"
  | "s3_secret_access_key"
  | "s3_buckets"
type S3SettingsState = Partial<Record<S3SettingKey, string>> & S3SettingMap

const S3_PROTOCOLS = ["http", "https"] as const
const CREDENTIAL_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

const randomCredential = (length: number) => {
  const bytes = new Uint8Array(length)
  crypto.getRandomValues(bytes)
  let value = ""
  for (let i = 0; i < length; i += 1) {
    value += CREDENTIAL_ALPHABET[bytes[i] % CREDENTIAL_ALPHABET.length]
  }
  return value
}

const bucketItems = (value: string) => {
  try {
    const parsed = JSON.parse(value || "[]")
    if (!Array.isArray(parsed)) return []
    return parsed
      .filter((item) => item && typeof item === "object")
      .map((item) => ({
        name: String((item as any).name ?? ""),
        path: String((item as any).path ?? ""),
      }))
      .filter((item) => item.name || item.path)
  } catch {
    return []
  }
}

const normalizeBuckets = (items: S3Bucket[]) => {
  return JSON.stringify(
    items
      .map((item) => ({
        name: item.name.trim(),
        path: item.path.trim(),
      }))
      .filter((item) => item.name && item.path),
  )
}

const splitEndpoint = (endpoint: string) => {
  const value = endpoint.trim()
  if (!value) {
    return { protocol: "http", host: "" }
  }
  const matched = value.match(/^(https?):\/\/(.+)$/i)
  if (matched) {
    return {
      protocol: matched[1].toLowerCase(),
      host: matched[2],
    }
  }
  return {
    protocol: "http",
    host: value,
  }
}

const joinEndpoint = (protocol: string, host: string) => {
  const trimmedHost = host.trim().replace(/^\/+/, "")
  if (!trimmedHost) {
    return ""
  }
  return `${protocol}://${trimmedHost}`
}

const S3Settings = () => {
  const t = useT()
  useManageTitle("manage.sidemenu.s3")
  const { copy } = useUtil()
  const [settingsLoading, getSettings] = useFetch(
    (): PResp<SettingItem[]> => listSettingsByGroup(Group.S3),
  )
  const [settings, setSettings] = createSignal<S3SettingsState>({})
  const [protocol, setProtocol] = createSignal<"http" | "https">("http")
  const [endpointHost, setEndpointHost] = createSignal("")
  const [buckets, setBuckets] = createSignal<S3Bucket[]>([])

  const refresh = async () => {
    const resp = await getSettings()
    handleResp(resp, (data) => {
      const map: S3SettingsState = {}
      for (const item of data) {
        map[item.key] = item.value
      }
      const endpoint = splitEndpoint(map.s3_endpoint || "")
      setProtocol(endpoint.protocol as "http" | "https")
      setEndpointHost(endpoint.host)
      setSettings(map)
      setBuckets(bucketItems(map.s3_buckets || "[]"))
    })
  }

  onMount(refresh)

  const updateField = (key: string, value: string) => {
    setSettings((prev) => ({ ...prev, [key]: value }))
  }

  const currentBuckets = createMemo(() => buckets())
  const buildSavePayload = (overrides?: Partial<S3SettingsState>) => {
    const next = {
      ...settings(),
      ...overrides,
      s3_endpoint: joinEndpoint(protocol(), endpointHost()),
      s3_region: settings().s3_region || "",
      s3_port: settings().s3_port || "9000",
      s3_buckets: normalizeBuckets(buckets()),
    }

    return [
      {
        key: "s3_endpoint",
        value: next.s3_endpoint || "",
        type: Type.String,
        help: "",
        group: Group.S3,
        flag: 1,
      },
      {
        key: "s3_region",
        value: next.s3_region || "",
        type: Type.String,
        help: "",
        group: Group.S3,
        flag: 1,
      },
      {
        key: "s3_port",
        value: next.s3_port || "9000",
        type: Type.Number,
        help: "",
        group: Group.S3,
        flag: 1,
      },
      {
        key: "s3_access_key_id",
        value: next.s3_access_key_id || "",
        type: Type.String,
        help: "",
        group: Group.S3,
        flag: 1,
      },
      {
        key: "s3_secret_access_key",
        value: next.s3_secret_access_key || "",
        type: Type.String,
        help: "",
        group: Group.S3,
        flag: 1,
      },
      {
        key: "s3_buckets",
        value: next.s3_buckets || "[]",
        type: Type.Text,
        help: "",
        group: Group.S3,
        flag: 1,
      },
    ]
  }

  const [saveLoading, saveSettings] = useFetch(
    (): PEmptyResp => saveSettingsApi(buildSavePayload()),
  )

  return (
    <MaybeLoading loading={settingsLoading()}>
      <VStack w="$full" alignItems="start" spacing="$3">
        <Box
          w="$full"
          p="$4"
          borderRadius="$xl"
          border="1px solid $neutral5"
          bg="linear-gradient(145deg, rgba(15, 23, 42, 0.04), rgba(15, 23, 42, 0.0))"
          boxShadow="0 16px 44px rgba(15, 23, 42, 0.08)"
        >
          <HStack w="$full" justifyContent="space-between" alignItems="start" wrap="wrap" mb="$3">
            <VStack alignItems="start" spacing="$1">
              <Heading>{t("settings.s3_panel")}</Heading>
              <Text fontSize="$sm" color="$neutral11">
                {t("settings.s3_panel_tip")}
              </Text>
            </VStack>
            <HStack spacing="$2">
              <Button variant="outline" onClick={refresh}>
                {t("global.refresh")}
              </Button>
              <Button
                variant="outline"
                colorScheme="warning"
                onClick={async () => {
                  if (
                    !window.confirm(
                      "确认重新生成 S3 凭据？生成后当前 access key 和 secret access key 会立即失效。",
                    )
                  ) {
                    return
                  }
                  const nextSettings = {
                    ...settings(),
                    s3_access_key_id: randomCredential(20),
                    s3_secret_access_key: randomCredential(40),
                  }
                  setSettings(nextSettings)
                  const resp = await saveSettingsApi(buildSavePayload(nextSettings))
                  handleResp(resp, async () => {
                    await refresh()
                    notify.success("S3 凭据已重新生成")
                  })
                }}
              >
                重新生成凭据
              </Button>
              <Button
                colorScheme="accent"
                loading={saveLoading()}
                onClick={async () => {
                  const resp = await saveSettings()
                  handleResp(resp, async () => {
                    await refresh()
                    notify.success(t("global.save_success"))
                  })
                }}
              >
                {t("global.save")}
              </Button>
            </HStack>
          </HStack>

          <SimpleGrid columns={{ "@initial": 1, "@md": 2 }} gap="$3">
            <FormControl>
              <FormLabel>{t("settings.s3_endpoint")}</FormLabel>
              <HStack spacing="$2" alignItems="stretch">
                <Select
                  value={protocol()}
                  onChange={(value) => setProtocol(value as "http" | "https")}
                >
                  <SelectTrigger w="$28">
                    <SelectPlaceholder>{t("global.choose")}</SelectPlaceholder>
                    <SelectValue />
                    <SelectIcon />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectListbox>
                      <For each={S3_PROTOCOLS}>
                        {(item) => (
                          <SelectOption value={item}>
                            <SelectOptionText>{item}</SelectOptionText>
                            <SelectOptionIndicator />
                          </SelectOption>
                        )}
                      </For>
                    </SelectListbox>
                  </SelectContent>
                </Select>
                <Input
                  value={endpointHost()}
                  placeholder="s3.example.com or s3.example.com:9000"
                  onInput={(e) => setEndpointHost(e.currentTarget.value)}
                />
              </HStack>
            </FormControl>
            <FormControl>
              <FormLabel>{t("settings.s3_region")}</FormLabel>
              <Input
                value={settings().s3_region || ""}
                placeholder="us-east-1"
                onInput={(e) => updateField("s3_region", e.currentTarget.value)}
              />
            </FormControl>
            <FormControl>
              <FormLabel>{t("settings.s3_port")}</FormLabel>
              <Input
                type="number"
                value={settings().s3_port || "9000"}
                onInput={(e) => updateField("s3_port", e.currentTarget.value)}
              />
            </FormControl>
            <FormControl>
              <FormLabel>{t("settings.s3_access_key_id")}</FormLabel>
              <HStack spacing="$2" alignItems="stretch">
                <Input
                  value={settings().s3_access_key_id || ""}
                  readOnly
                />
                <Button
                  variant="outline"
                  onClick={() => copy(settings().s3_access_key_id || "")}
                >
                  {t("global.copy")}
                </Button>
              </HStack>
            </FormControl>
            <FormControl>
              <FormLabel>{t("settings.s3_secret_access_key")}</FormLabel>
              <HStack spacing="$2" alignItems="stretch">
                <Input
                  type="password"
                  value={settings().s3_secret_access_key || ""}
                  readOnly
                />
                <Button
                  variant="outline"
                  onClick={() => copy(settings().s3_secret_access_key || "")}
                >
                  {t("global.copy")}
                </Button>
              </HStack>
            </FormControl>
          </SimpleGrid>
        </Box>

        <Box
          w="$full"
          p="$4"
          borderRadius="$xl"
          border="1px solid $neutral5"
          bg="white"
          boxShadow="0 10px 30px rgba(15, 23, 42, 0.06)"
        >
          <HStack w="$full" justifyContent="space-between" alignItems="start" mb="$3">
            <VStack alignItems="start" spacing="$1">
              <Heading fontSize="$lg">{t("settings.s3_bucket_panel")}</Heading>
              <Text fontSize="$sm" color="$neutral11">
                {t("settings.s3_bucket_panel_tip")}
              </Text>
            </VStack>
            <Button
              onClick={() => {
                setBuckets((prev) => [...prev, { name: "", path: "" }])
              }}
            >
              {t("global.add")}
            </Button>
          </HStack>

          <VStack w="$full" spacing="$3" alignItems="start">
            <For each={currentBuckets()}>
              {(bucket, index) => (
                <S3BucketItem
                  name={bucket.name}
                  path={bucket.path}
                  onChange={(val) => {
                    setBuckets((prev) =>
                      prev.map((item, i) => (i === index() ? val : item)),
                    )
                  }}
                  onDelete={() => {
                    const currentIndex = index()
                    setBuckets((prev) =>
                      prev.filter((_, i) => i !== currentIndex),
                    )
                  }}
                />
              )}
            </For>
          </VStack>
        </Box>
      </VStack>
    </MaybeLoading>
  )
}

export default S3Settings
