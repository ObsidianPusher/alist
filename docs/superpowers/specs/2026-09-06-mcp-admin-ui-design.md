# MCP 管理配置页设计

## 目标

在 AList 管理后台提供与 S3 页面一致的 MCP 配置页。管理员可选择 AList 挂载目录下的文档根、即时启停服务、生成一次性可复制 Token，并复制 Codex CLI、Claude Code 和通用 JSON 客户端配置。

## 持久化与热加载

MCP 配置从 `model.SettingItem` 读取，不再读取配置文件。新增设置组 `MCP` 和以下私有设置：

- `mcp_enabled`：布尔值。
- `mcp_docs_root`：由目录选择器确定的 AList 挂载路径。
- `mcp_token_hash`：Token 的 SHA-256 十六进制摘要。
- `mcp_max_read_bytes`、`mcp_max_write_bytes`、`mcp_history_limit`：受限整数。

保存设置经过服务端校验：根目录必须存在且为目录，数值必须在预设范围内。设置变更 hook 原子替换运行中的 MCP 服务实例；启用、禁用、根目录和 Token 都在保存成功后立即生效。旧服务不再接受新请求，正在执行的请求保留其 30 秒上下文期限。

## Token 生命周期

Token 只由专用管理员 API `POST /api/admin/mcp/token/reset` 生成。API 用密码学安全随机源生成值、持久化其摘要，并且仅在该响应中返回明文。普通 MCP 设置读取接口只返回 `token_configured: true|false`；它们永不返回摘要、旧 Token 或 Token 明文。

前端弹窗一次性显示 Token，提供复制按钮和明确警告。刷新、关闭弹窗或重新进入页面后不可恢复；轮换立即使旧 Token 失效。保存其他设置不能覆盖或清空 Token。

## 管理 API

- `GET /api/admin/mcp/config`：返回可编辑的非敏感配置和 `token_configured`。
- `POST /api/admin/mcp/config`：保存并热加载配置；只接受挂载内目录与允许范围内的限制值。
- `POST /api/admin/mcp/token/reset`：轮换 Token，返回一次性明文 Token 和当前已生效的 endpoint。

所有 API 均在既有 `AuthAdmin` 组内。错误不泄漏 Token、摘要或存储驱动凭据。

## 管理页面

新增“管理 > MCP”菜单，布局沿用 S3 设置页：

- 启用开关、文档根目录只读输入和“选择目录”按钮、读写字节上限与历史保留数；目录选择器复用 `FolderTree`，从 `/` 浏览已挂载存储。
- 保存按钮、刷新按钮和“生成/轮换 Token”按钮。Token 状态只显示“已配置”或“未配置”。
- endpoint 由当前浏览器 `window.location.origin`、AList 的 base path 与 `/mcp` 拼接，不持久化域名。
- Token 生成后显示三种可复制配置：Codex CLI（环境变量与 `config.toml`）、Claude Code HTTP `claude mcp add` 命令、通用 JSON HTTP MCP 配置。每份都包含 endpoint；Token 只出现在复制内容及一次性弹窗，不出现在常规页面状态或日志。

## 客户端模板

Codex CLI 模板使用 `ALIST_MCP_TOKEN` 环境变量和远程 MCP URL；不将 Token 直接写入 `config.toml`。Claude Code 模板使用 `--transport http` 和 `Authorization: Bearer` header。通用 JSON 模板放入 `headers.Authorization`。

## 测试

- 后端：设置校验、Token 不可由普通配置 API 读取、轮换立即拒绝旧 Token、热加载更新文档根。
- 前端：加载状态、目录选择与保存负载、Token 一次性弹窗、三种复制文本使用当前 origin。
- 回归：S3 设置页与既有 Setting API 行为不变；`go test ./...` 与前端类型检查通过。
