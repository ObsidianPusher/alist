# MCP Document Encoding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the embedded Markdown document MCP consistently read, maintain, index, and emit UTF-8 without BOM.

**Architecture:** A single canonical-document loader reads bounded source bytes, normalizes encoding once, persists deterministic repairs, then supplies every document operation. Pagination occurs only after canonicalization. Rebuild and periodic maintenance use the same loader.

**Tech Stack:** Go, AList fs/stream APIs, Bleve, Go MCP SDK, testify.

**Spec:** `docs/superpowers/specs/2026-09-09-mcp-doc-encoding-design.md`

## Global Constraints

- Only `.md` files are eligible.
- Canonical persisted format is UTF-8 without BOM.
- Do not add MCP tools or file configuration.
- Do not modify an encoding that cannot be determined safely.

---

### Task 1: Canonical encoding loader

**Files:**
- Modify: `internal/docs/encoding.go`, `internal/docs/service.go`, `internal/docs/encoding_test.go`, `internal/docs/service_test.go`

- [ ] Write failing tests for UTF-8 BOM, UTF-16 BOM, GB18030, malformed content, UTF-8 page boundaries, and BOM write normalization.
- [ ] Run `go test ./internal/docs -run 'Test.*Encoding|TestRead' -count=1` and verify new cases fail.
- [ ] Add a bounded full-document canonical loader and make reads paginate its UTF-8 result.
- [ ] Run the targeted tests and commit the verified change.

### Task 2: Uniform derived data

**Files:**
- Modify: `internal/docs/service.go`, `internal/docs/status_test.go`, `internal/docs/service_test.go`

- [ ] Write failing tests proving rebuild, history, and all document reads use canonical content.
- [ ] Run the targeted tests and verify failure.
- [ ] Route rebuild/history/write through the loader and update status counters.
- [ ] Run `go test ./internal/docs -count=1` and commit the verified change.

### Task 3: Runtime maintenance and MCP regression

**Files:**
- Modify: `server/mcp/server.go`, `server/mcp/server_test.go`, `internal/docs/status_test.go`

- [ ] Write failing tests for scheduled lifecycle cancellation and MCP JSON Chinese round trips.
- [ ] Run targeted server and docs tests and verify failure.
- [ ] Add cancellable low-frequency maintenance using the existing rebuild path; add JSON response regression coverage.
- [ ] Run `go test ./...`, `git diff --check`, and commit the verified change.
