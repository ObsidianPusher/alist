import type { PEmptyResp, PResp } from "~/types"
import { r } from "~/utils"

export type MCPConfig = {
  enabled: boolean
  docs_root: string
  max_read_bytes: string
  max_write_bytes: string
  history_limit: string
  token_configured: boolean
  index_status?: {
    running: boolean
    documents: number
    chunks: number
    scanned: number
    normalized: number
    failed: number
    last_success?: string
  }
}

export type MCPConfigInput = {
  enabled: boolean
  docs_root: string
  max_read_bytes: number
  max_write_bytes: number
  history_limit: number
}

export const getMCPConfig = (): PResp<MCPConfig> => r.get("/admin/setting/mcp/config")
export const saveMCPConfig = (config: MCPConfigInput): PResp<MCPConfig> => r.post("/admin/setting/mcp/config", config)
export const resetMCPToken = (): PResp<{ token: string }> => r.post("/admin/setting/mcp/token/reset")
