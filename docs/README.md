# docs 索引

## 背景

本目录用于沉淀 AList 仓库的业务链路、模块分工、问题排查和改造记录，便于后续快速定位代码入口、状态流和影响面。

## 现状链路

- 服务入口：`main.go` -> `cmd/` -> `server/`，负责启动配置、路由、WebDAV、任务等服务能力。
- 后端核心：
  - `internal/op/`：统一文件系统操作抽象，封装 List/Get/Link/Copy/Move/Rename/Cache 等核心流程。
  - `internal/fs/`：面向业务动作的文件管理编排，包含跨存储复制/移动、任务化、进度和重试。
  - `drivers/`：各类存储驱动实现，`drivers/local` 为本地盘实现，其他目录对应网盘/协议驱动。
  - `internal/model/`：存储、文件、链接、用户等模型定义。
  - `internal/task/`、`server/handles/task.go`：异步任务管理与接口暴露。
- 前端核心：
  - `web/src/pages/home/`：文件列表、复制/移动弹窗、任务中心等主要页面。
  - `web/src/api/`：前端调用的接口封装。
- 外部依赖：
  - Gin 提供 HTTP 服务。
  - SolidJS 提供前端页面。
  - `github.com/otiai10/copy` 用于本地跨设备复制降级。

## 关键证据

- `README.md`：确认项目是 Gin + SolidJS 的多存储文件列表程序。
- `internal/fs/copy.go`：跨存储复制/移动和任务化主链路。
- `internal/fs/local_move.go`：本地存储跨设备移动的本地盘特化逻辑。
- `internal/op/fs.go`：通用文件系统操作与缓存逻辑。

## 数据/状态流

1. 前端发起复制/移动请求。
2. `server/handles/` 进入文件操作接口。
3. `internal/fs/` 判断同存储、跨存储、本地盘等策略。
4. `internal/op/` 与 `drivers/*` 执行实际存储读写。
5. 如为长耗时操作，进入任务中心持久化进度与状态。
6. 完成后清理相关目录缓存并回传结果。

## 结论

- 日常定位文件移动、复制、缓存、任务类问题时，优先从 `internal/fs/` 和 `internal/op/` 入手。
- 涉及某一类存储的差异行为，再下钻到对应 `drivers/<driver>`。

## 风险

- 本索引是面向快速定位的压缩版说明，不替代完整实现细节。
- 部分驱动的外部 API 限制和平台差异需要结合驱动代码与运行环境确认。

## 待确认

- 不同远端驱动的原生复制/移动能力边界，需要按驱动逐个补专题文档。

## 已归档主题

- [跨本地存储移动对 FAT/exFAT 外接设备的元数据兼容修复](./cross-storage-local-move-metadata-compat.md)
