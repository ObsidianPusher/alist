# AList 文档 MCP 设计

## 目标

在 AList 进程内提供受 HTTPS 保护的 MCP Streamable HTTP 服务，使 Agent 只能在配置的文档根目录内查询、全文搜索和直接维护 Markdown/文本文件。

## 范围

- 新增单一 MCP 端点：`<base-path>/mcp`。
- 使用官方 Go MCP SDK 的 Streamable HTTP transport，不实现自定义 JSON-RPC 协议。
- 支持 `docs_search`、`docs_read`、`docs_update`、`docs_delete`、`docs_move`、`docs_history`。
- 仅索引 UTF-8 文本文件；首期允许 `.md`、`.mdx`、`.txt`、`.rst`、`.json`、`.yaml`、`.yml`。
- 不改变 `/dav`、既有 `/api/fs/*` 行为和既有文件名搜索索引。

## 非目标

- 不提供向量或语义检索。
- 不解析 Office/PDF/图片正文。
- 不在首期实现 MCP OAuth；使用独立静态 Bearer Token。
- 不把 MCP 权限扩大到文档根目录之外。

## 架构

`server/mcp` 只处理 MCP transport、Bearer 鉴权和工具注册；`internal/docs` 处理受限路径、正文读取、写入、版本历史和全文索引。所有文件操作经 `internal/fs`，以保持挂载路径、缓存和各驱动的兼容性。

正文索引使用独立 Bleve 目录 `data/docs-bleve`，不得复用 `conf.Conf.BleveDir`：现有索引只存文件名、父路径和大小，且不支持稳定文档 ID 的更新删除。

## 配置

新增配置字段：

- `mcp.enabled`：默认 `false`；关闭时不注册 `/mcp`。
- `mcp.docs_root`：AList 挂载路径；所有工具路径均相对此路径。
- `mcp.token_hash`：Bearer Token 的 SHA-256 十六进制摘要；原文只通过启动配置注入，不持久化。
- `mcp.index_dir`：默认 `<data-dir>/docs-bleve`。
- `mcp.max_read_bytes` 和 `mcp.max_write_bytes`：默认 `1048576`。
- `mcp.history_limit`：默认每文件 50 个版本。

服务启动时若 MCP 启用但根路径、token hash 或索引目录无效，应拒绝启动 MCP 子系统并记录明确错误；AList 主服务继续工作。

## 安全

- 认证仅接受 `Authorization: Bearer <token>`；使用常量时间比较摘要，日志、错误和历史记录均不得包含 Token 或正文。
- 输入路径拒绝空路径、绝对路径和清理后逃离根目录的路径。经 `path.Clean` 后将其拼接到 `docs_root`，再复核最终路径仍在根目录内。
- `docs_read`、`docs_update`、`docs_delete`、`docs_move` 只接受允许的文本扩展名；目录只允许用于 move/delete 的目录语义。
- 每请求 30 秒 context 超时；搜索 query 最大 256 字节、结果最大 20 条。超限返回工具错误。

## 工具契约

### docs_search

输入：`query`、可选 `path_prefix`、可选 `limit`。输出为命中列表：相对路径、内容片段、分数、revision、修改时间。只返回当前索引可见结果，并附带索引状态。

### docs_read

输入：`path`、可选 `offset`、可选 `limit`。输出：UTF-8 文本、`revision`（内容 SHA-256）、字节总数和截断标记。读取超出最大限制必须显式分页，不隐式丢内容。

### docs_update

输入：`path`、`content`、`expected_revision`。新建文件必须传 `null`；覆盖必须传最近读取的 revision。revision 不一致返回 `conflict` 和当前 revision，绝不静默覆盖。文件写入成功后更新索引和历史。

### docs_delete

输入：`path`、`expected_revision`。文件删除要求当前 revision 匹配；目录只能在为空时删除。删除前内容进入历史快照。

### docs_move

输入：`from`、`to`、`expected_revision`。源文件 revision 必须匹配，目标不得已存在；成功后删除旧索引条目并建立新条目。

### docs_history

输入：`path`、可选 `revision`。未指定 revision 时列出变更记录；指定时返回已压缩保存的历史快照及其元数据。

## 索引与历史一致性

版本表保存路径、revision、动作、时间、request ID、内容大小和写前 gzip 快照。正文不进入常规日志。每文件清除早于 `history_limit` 的记录。

文件操作成功后同步更新文档索引。若索引或历史写入失败，文件变更不回滚；记录待修复项并返回 `index_pending`。全量重建会遍历文档根、重建索引并清除已修复标记；重建中继续对外提供最后成功索引。

## 错误模型

工具错误使用稳定代码：`unauthorized`、`invalid_path`、`unsupported_type`、`not_found`、`conflict`、`limit_exceeded`、`storage_error`、`index_pending`。错误文本不泄漏底层驱动凭据、Token 或正文。

## 验证

- 路径规范化、根目录限制、扩展名、Token 验证及限流单元测试。
- 内存 SQLite 和 fake storage 测试创建、读取、条件更新、冲突、删除、移动、版本保留。
- Bleve 测试新增、覆盖、删除、路径过滤和全量重建。
- `httptest` 做 MCP transport 初始化、工具发现、Bearer 鉴权和端到端工具调用。
- 回归执行 `go test ./server/... ./internal/docs/... ./internal/fs/...`，最终执行 `go test ./...`。
