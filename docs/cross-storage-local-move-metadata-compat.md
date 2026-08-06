# 跨本地存储移动对 FAT/exFAT 外接设备的元数据兼容修复

## 背景

在 AList 的“跨存储文件操作（复制或移动）”场景里，源和目标都为本地存储时，会优先走本地盘特化逻辑。用户反馈目标路径位于 Android 挂载的外接 USB/SD 设备时，任务报错 `chown ...: operation not permitted`，但文件系统本身不需要也不支持 `chown`。

## 现状链路

1. 前端任务中心发起跨存储复制/移动。
2. `internal/fs/copy.go` 根据源/目标存储类型选择策略。
3. 当源和目标都是 `Local` 且不是同一存储时，进入 `internal/fs/local_move.go:moveBetweenLocalStorages`。
4. 若 `os.Rename` 因跨设备返回 `EXDEV`，逻辑降级为 `github.com/otiai10/copy` 的递归复制。
5. 原逻辑开启 `PreserveOwner: true`，会在目标文件系统执行 `chown`；FAT32/exFAT 等不支持 ownership 元数据，导致复制流程报错。

## 关键证据

- `internal/fs/copy.go`
  - `copyOrMoveAsTask` 在跨本地存储 `move` 时调用 `moveBetweenLocalStorages`。
- `internal/fs/local_move.go`
  - 原实现中，`os.Rename` 跨设备失败后使用 `cp.Copy(..., PreserveOwner: true)`。
- `github.com/otiai10/copy@v1.14.0/preserve_owner.go`
  - `PreserveOwner` 最终调用 `os.Chown(dest, uid, gid)`。
- 用户现场报错
  - `chown /mnt/media_rw/... : operation not permitted`
  - `/mnt/media_rw/*` 在 Android 场景下通常为外接存储挂载点，常见格式为 FAT32/exFAT。

## 数据/状态流

1. 用户选择跨存储移动。
2. 后端尝试同机文件系统重命名。
3. 跨设备时降级为“复制到目标 -> 删除源文件”。
4. 复制阶段先尝试保留时间和属主元数据。
5. 若目标文件系统不支持 ownership / permission 元数据，捕获 `EPERM` / `ENOTSUP` / `EOPNOTSUPP` / `operation not permitted`。
6. 清理目标半成品后，自动重试为兼容模式：
   - 保留时间
   - 不保留 owner
   - 不强制回写权限位
7. 复制成功后再删除源目录，维持原 move 语义。

## 结论

- 根因是跨设备降级复制默认保留 owner 元数据，而 FAT/exFAT 等外接设备不支持 `chown/chmod`。
- 最小且兼容的修复方式是仅在识别到“目标文件系统不支持元数据保留”时自动回退，而不是一刀切关闭所有本地盘的 owner 保留。

## 风险

- 当前降级条件基于典型错误码和错误文案匹配，覆盖常见 Linux/Android 场景；极少数平台如果返回其他错误文本，仍可能需要补充识别。
- 回退模式会放弃 owner 和权限位保留，但这正是 FAT/exFAT 这类文件系统的能力边界。

## 待确认

- 如果后续发现某些挂载驱动在不支持 `chmod` 时返回不同错误文案，需要补充判定条件。
- 若前端仍将该场景显示为失败，需要继续核查任务状态聚合和错误上报路径。

## 改动点清单与影响面

- 改动文件：
  - `internal/fs/local_move.go`
  - `internal/fs/local_move_test.go`
- 核心改动：
  - 抽出 `copyBetweenLocalPaths`，统一处理本地跨设备复制。
  - 首次复制保留时间和 owner。
  - 命中不支持元数据的错误后，删除目标半成品并使用兼容参数重试。
  - 新增单测覆盖“第一次因 `chown` 失败，第二次降级成功”的场景。
- 影响面：
  - 仅影响 `Local -> Local` 且 `os.Rename` 跨设备失败后的降级复制链路。
  - 同盘移动、远端存储复制、普通支持 ownership 的 Linux 文件系统行为保持不变。
