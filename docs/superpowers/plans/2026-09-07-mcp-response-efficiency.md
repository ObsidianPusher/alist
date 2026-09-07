# MCP Response Efficiency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans task-by-task.

**Goal:** Eliminate duplicate MCP result payloads and enforce compact, reliable retrieval defaults.

**Architecture:** MCP handlers explicitly return an empty content list with typed output, preventing SDK JSON duplication. The document index owns path-level result aggregation, matching mode, and persisted modification time; service reads impose continuation-based limits.

**Tech Stack:** Go 1.23, MCP Go SDK, Bleve v2.

**Spec:** `docs/superpowers/specs/2026-09-07-mcp-response-efficiency-design.md`

### Task 1: Protocol and compact retrieval

- [ ] Add JSON-RPC regression tests for empty `content` and populated `structuredContent`.
- [ ] Make every success handler return `CallToolResult{Content: []Content{}}`.
- [ ] Add read/section bounds and continuation fields; test defaults and caps.
- [ ] Commit focused changes.

### Task 2: Search correctness

- [ ] Add failing tests for path-level deduplication, match modes, and modified time.
- [ ] Pass AList object `ModTime()` into index rebuild replacement.
- [ ] Aggregate best chunk per path; query exact/path modes and prefix before result limiting.
- [ ] Omit zero modification times; commit focused changes.

### Task 3: Verification and documentation

- [ ] Update `docs/mcp-docs.md` with response modes and the recommended call chain.
- [ ] Run `go test ./...`, `git diff --check`, and commit.
