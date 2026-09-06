# MCP Admin UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide database-backed, hot-reloadable MCP management with one-time Token retrieval and copyable client configurations.

**Architecture:** Persist non-secret MCP settings in `SettingItem` group `MCP`; a dedicated service owns the active MCP handler and atomically swaps it after validated saves. Dedicated admin APIs hide token hashes and issue a raw Token only on reset. The Solid page follows `S3.tsx` and uses `FolderTree` for a mounted document root.

**Tech Stack:** Go 1.23, Gin, GORM settings, SolidJS, Hope UI.

**Spec:** `docs/superpowers/specs/2026-09-06-mcp-admin-ui-design.md`

## Global Constraints

- MCP configuration must not be read from nor written to the AList configuration file.
- Only `AuthAdmin` APIs may read or mutate MCP settings.
- Never return a stored token hash or a historical raw token.
- Preserve WebDAV, existing setting APIs, and S3 behavior.

---

### Task 1: Database MCP settings and runtime reload

**Files:** Modify `internal/conf/const.go`, `internal/model/setting.go`, `internal/bootstrap/data/setting.go`, `internal/docs/service.go`, `server/mcp/server.go`; create `server/mcp/config.go` and tests.

- [ ] Add failing tests for settings-to-runtime conversion, invalid root/limits, and a reload that invalidates the old token.
- [ ] Add `MCP` setting group and default private setting rows.
- [ ] Replace file-config construction with a settings loader and an atomic active handler holder.
- [ ] Register setting hooks that debounce a validated hot reload.
- [ ] Run focused Go tests and commit.

### Task 2: Admin-only MCP configuration and Token APIs

**Files:** Modify `server/router.go`; create `server/handles/mcp.go`, `server/handles/mcp_test.go`, `web/src/api/admin/mcp.ts`.

- [ ] Add failing handler tests proving normal config responses omit hash/token and reset returns a raw Token only once.
- [ ] Implement config get/save with mount-directory validation and numeric bounds.
- [ ] Implement reset with `random.Token`, SHA-256 persistence, hot reload, and endpoint response.
- [ ] Register the APIs beneath the existing admin group and run handler tests.

### Task 3: MCP management page and client snippets

**Files:** Create `web/src/pages/manage/settings/MCP.tsx`; modify `web/src/types/setting.ts`, `web/src/pages/manage/sidemenu_items.tsx`, locale files.

- [ ] Add a failing frontend type/build check for the new route and API types.
- [ ] Build a S3-style form with `FolderTree`, enable toggle, limits, history count, save/refresh controls, and configured-state indicator.
- [ ] Implement a one-time Token dialog with copy actions for raw Token, Codex TOML plus environment export, Claude Code command, and generic JSON.
- [ ] Derive endpoint from `window.location.origin`, configured base path, and `/mcp`; do not persist it.
- [ ] Run frontend type/build checks and commit.

### Task 4: Full regression verification and operator documentation

**Files:** Modify `docs/mcp-docs.md` and `README.md`.

- [ ] Document database-backed setup, Token rotation, client snippets, and immediate-effect semantics.
- [ ] Run `go test ./...`, frontend build/type check, and `git diff --check`.
- [ ] Commit documentation and verification-complete changes.
