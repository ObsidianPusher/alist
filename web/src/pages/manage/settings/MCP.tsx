import { Box, Button, FormControl, FormLabel, Heading, HStack, Input, Switch as HopeSwitch, Text, Textarea, VStack } from "@hope-ui/solid"
import { createSignal, onMount, Show } from "solid-js"
import { FolderChooseInput, MaybeLoading } from "~/components"
import { getMCPConfig, resetMCPToken, saveMCPConfig, type MCPConfig } from "~/api/admin/mcp"
import { useFetch, useManageTitle, useUtil } from "~/hooks"
import { handleResp, notify } from "~/utils"

const endpoint = () => `${window.location.origin}${window.location.pathname.split("/@manage")[0]}/mcp`
const snippets = (token: string) => ({
  codex: `export ALIST_MCP_TOKEN='${token}'\n\n# ~/.codex/config.toml\n[mcp_servers.alist_docs]\nurl = "${endpoint()}"\nbearer_token_env_var = "ALIST_MCP_TOKEN"`,
  claude: `claude mcp add --transport http alist-docs '${endpoint()}' --header 'Authorization: Bearer ${token}' --scope user`,
  json: JSON.stringify({ mcpServers: { alist_docs: { type: "http", url: endpoint(), headers: { Authorization: `Bearer ${token}` } } } }, null, 2),
})

const MCPSettings = () => {
  useManageTitle("manage.sidemenu.mcp")
  const { copy } = useUtil()
  const [config, setConfig] = createSignal<MCPConfig>({ enabled: false, docs_root: "/", max_read_bytes: "1048576", max_write_bytes: "1048576", history_limit: "50", token_configured: false })
  const [token, setToken] = createSignal("")
  const [loading, get] = useFetch(getMCPConfig)
  const refresh = async () => { const resp = await get(); handleResp(resp, setConfig) }
  onMount(refresh)
  const change = (key: keyof MCPConfig, value: string | boolean) => setConfig((old) => ({ ...old, [key]: value }))
  const save = async () => {
    const c = config()
    const resp = await saveMCPConfig({ enabled: c.enabled, docs_root: c.docs_root, max_read_bytes: Number(c.max_read_bytes), max_write_bytes: Number(c.max_write_bytes), history_limit: Number(c.history_limit) })
    handleResp(resp, (next) => { setConfig(next); notify.success("MCP 配置已保存并立即生效") })
  }
  const rotate = async () => {
    if (!window.confirm("重新生成 Token 后，旧 Token 会立即失效。确认继续？")) return
    const resp = await resetMCPToken()
    handleResp(resp, async ({ token }) => { setToken(token); await refresh(); notify.success("Token 已生成，仅在此处显示一次") })
  }
  return <MaybeLoading loading={loading()}><VStack w="$full" alignItems="start" spacing="$4">
    <Box w="$full" p="$4" borderRadius="$xl" border="1px solid $neutral5">
      <HStack justifyContent="space-between" w="$full" mb="$4"><VStack alignItems="start"><Heading>MCP 文档服务</Heading><Text color="$neutral11">保存后立即热加载；地址自动使用当前访问域名。</Text></VStack><HStack><Button variant="outline" onClick={refresh}>刷新</Button><Button variant="outline" colorScheme="warning" onClick={rotate}>生成 / 重置 Token</Button><Button colorScheme="accent" onClick={save}>保存</Button></HStack></HStack>
      <VStack alignItems="stretch" spacing="$3"><FormControl><FormLabel>启用 MCP</FormLabel><HopeSwitch checked={config().enabled} onChange={(e: any) => change("enabled", e.currentTarget.checked)} /></FormControl><FormControl><FormLabel>文档目录（从挂载目录选择）</FormLabel><FolderChooseInput onlyFolder value={config().docs_root} onChange={(value) => change("docs_root", value)} /></FormControl><FormControl><FormLabel>MCP 地址</FormLabel><Input readOnly value={endpoint()} /></FormControl><FormControl><FormLabel>单次读取上限（字节）</FormLabel><Input type="number" value={config().max_read_bytes} onInput={(e) => change("max_read_bytes", e.currentTarget.value)} /></FormControl><FormControl><FormLabel>单次写入上限（字节）</FormLabel><Input type="number" value={config().max_write_bytes} onInput={(e) => change("max_write_bytes", e.currentTarget.value)} /></FormControl><FormControl><FormLabel>历史版本数</FormLabel><Input type="number" value={config().history_limit} onInput={(e) => change("history_limit", e.currentTarget.value)} /></FormControl></VStack>
    </Box>
    <Show when={token()}>{() => { const s = snippets(token()); return <Box w="$full" p="$4" borderRadius="$xl" border="1px solid $warning7"><Heading mb="$2">请立即复制 Token 与客户端配置</Heading><Text mb="$3">关闭或刷新页面后不会再次显示该 Token。</Text><FormControl mb="$3"><FormLabel>Token</FormLabel><HStack><Input readOnly value={token()} /><Button onClick={() => copy(token())}>复制</Button></HStack></FormControl><FormControl mb="$3"><FormLabel>Codex CLI</FormLabel><Textarea readOnly rows={6} value={s.codex} /><Button mt="$2" onClick={() => copy(s.codex)}>复制 Codex 配置</Button></FormControl><FormControl mb="$3"><FormLabel>Claude Code CLI</FormLabel><Textarea readOnly rows={3} value={s.claude} /><Button mt="$2" onClick={() => copy(s.claude)}>复制 Claude 配置</Button></FormControl><FormControl><FormLabel>通用 JSON</FormLabel><Textarea readOnly rows={7} value={s.json} /><Button mt="$2" onClick={() => copy(s.json)}>复制 JSON 配置</Button></FormControl></Box> }}</Show>
  </VStack></MaybeLoading>
}
export default MCPSettings
