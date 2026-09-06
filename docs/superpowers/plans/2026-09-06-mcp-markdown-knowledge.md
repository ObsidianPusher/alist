# Markdown Knowledge MCP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the embedded MCP a Markdown-only knowledge interface with precise, bounded retrieval and observable indexing.

**Architecture:** A deterministic Markdown chunker produces heading-aware chunks for one Bleve record per chunk. `Service` owns serialized rebuild state and exposes chunk search, section reads and status; the MCP layer only maps typed tool I/O.

**Tech Stack:** Go 1.23, Gin, Bleve v2, existing AList fs abstraction, MCP Go SDK.

**Spec:** `docs/superpowers/specs/2026-09-06-mcp-markdown-knowledge-design.md`

## Global Constraints

- Accept and index `.md` only; do not parse or index any other format.
- Retain existing revision-guarded document mutations and compatible `docs_read` / `docs_list` behavior.
- Keep full rebuild single-flight and retain search availability during rebuild.
- Do not add an external service or file-format parsing dependency.

---

### Task 1: Markdown chunks and chunk index

**Files:**
- Create: `internal/docs/chunk.go`, `internal/docs/chunk_test.go`
- Modify: `internal/docs/path.go`, `internal/docs/index.go`, `internal/docs/index_test.go`

**Interfaces:**
- Produces `ChunkMarkdown(content string, maxChars int) []Chunk` with `ID`, `Heading`, `Ordinal`, `Start`, `End`, `Content`.
- Produces `Index.Replace(path, revision string, modified time.Time, chunks []Chunk) error` and chunk-aware `Index.Search(SearchQuery) (SearchPage, error)`.

- [ ] **Step 1: Write failing chunk tests**

```go
func TestChunkMarkdownUsesHeadingBreadcrumbAndParagraphBoundaries(t *testing.T) {
 chunks := ChunkMarkdown("# Guide\nintro\n\n## Install\nfirst\n\nsecond", 12)
 require.Equal(t, "Guide / Install", chunks[1].Heading)
 require.Equal(t, "first", chunks[1].Content)
}
```

- [ ] **Step 2: Run the chunk test**

Run: `go test ./internal/docs -run TestChunkMarkdownUsesHeadingBreadcrumbAndParagraphBoundaries`
Expected: FAIL because `ChunkMarkdown` is undefined.

- [ ] **Step 3: Implement the minimal deterministic chunker**

```go
type Chunk struct { ID, Heading string; Ordinal, Start, End int; Content string }
func ChunkMarkdown(content string, maxChars int) []Chunk
```

Recognize ATX headings, retain the heading stack, split long sections only at paragraph boundaries where possible, and use a path-independent `section-N` ID.

- [ ] **Step 4: Restrict document paths to Markdown**

Replace `allowedExtensions` with only `.md`; extend `ResolvePath` tests to reject `.txt`, `.json`, and `.pdf`.

- [ ] **Step 5: Add chunk-index tests and implementation**

```go
func TestIndexSearchReturnsChunkMetadataAndCursor(t *testing.T) {
 // Index two chunks; assert path, section ID, heading, source range and next cursor.
}
```

Store path, section ID, heading, ordinal, ranges, revision, modified and content as Bleve fields. Delete a document's existing chunks before indexing replacements. Use a stable cursor `(score,path,ordinal)` encoded with base64 JSON; reject malformed cursors.

- [ ] **Step 6: Verify and commit**

Run: `go test ./internal/docs`

```bash
git add internal/docs/chunk.go internal/docs/chunk_test.go internal/docs/path.go internal/docs/path_test.go internal/docs/index.go internal/docs/index_test.go
git commit -m "feat: index Markdown document chunks"
```

### Task 2: Service retrieval and observable rebuild

**Files:**
- Modify: `internal/docs/service.go`, `internal/docs/service_test.go`
- Create: `internal/docs/status.go`, `internal/docs/status_test.go`

**Interfaces:**
- Produces `Service.Search(SearchInput) (SearchPage, error)`, `Service.ReadSection(ctx, path, sectionID string, adjacent int) (SectionResult, error)`, `Service.Status() IndexStatus`, `Service.RebuildAsync(ctx) (string, error)`.

- [ ] **Step 1: Write failing service tests**

```go
func TestReadSectionReturnsRequestedChunkAndNeighbor(t *testing.T) {
 result, err := svc.ReadSection(ctx, "guide.md", "section-2", 1)
 require.NoError(t, err)
 require.Len(t, result.Chunks, 3)
}
func TestRebuildStatusRecordsFailureWithoutStoppingOtherFiles(t *testing.T)
```

- [ ] **Step 2: Run those tests**

Run: `go test ./internal/docs -run 'Test(ReadSection|RebuildStatus)'`
Expected: FAIL because the service methods and status type do not exist.

- [ ] **Step 3: Implement section reads and indexed mutations**

Read Markdown once, verify its revision against the indexed document, split it with `ChunkMarkdown`, select the requested section plus bounded neighbors, and return path/revision/heading/ranges. Update, delete and move replace/delete all chunks for the affected path.

- [ ] **Step 4: Implement single-flight rebuild state**

```go
type IndexStatus struct { Generation string; Running bool; Documents, Chunks, Failed int; LastSuccess time.Time; Failures []IndexFailure }
func (s *Service) RebuildAsync(context.Context) (string, error)
```

Guard rebuild starts with a mutex. Scan only `.md`; process every eligible file; record a bounded list of file failures; remove stale indexed paths after successful scan; atomically publish status. The constructor starts the initial asynchronous rebuild through this method.

- [ ] **Step 5: Verify and commit**

Run: `go test ./internal/docs`

```bash
git add internal/docs/service.go internal/docs/service_test.go internal/docs/status.go internal/docs/status_test.go
git commit -m "feat: add Markdown section retrieval and index status"
```

### Task 3: MCP contracts and documentation

**Files:**
- Modify: `server/mcp/server.go`, `server/mcp/server_test.go`, `docs/mcp-docs.md`

**Interfaces:**
- Adds `docs_read_section`, `docs_index_status`, and `docs_rebuild_index`.
- Extends `docs_search` with `modified_after` and `cursor`.

- [ ] **Step 1: Write failing MCP registration tests**

```go
func TestMCPToolsExposeSectionAndIndexOperations(t *testing.T) {
 // Construct a runtime and assert the MCP initialize response lists all three tools.
}
```

- [ ] **Step 2: Run test and verify failure**

Run: `go test ./server/mcp -run TestMCPToolsExposeSectionAndIndexOperations`
Expected: FAIL because the tools are absent.

- [ ] **Step 3: Register typed tools**

Use inputs `path`, `section_id`, `adjacent`; `modified_after`, `cursor`; and no input for status/rebuild. `docs_rebuild_index` returns the generation. Validate integer ranges before service calls.

- [ ] **Step 4: Document the Markdown-only contract**

Document chunk metadata, section reads, search cursor behavior, index rebuild/status operations, and the fact that non-Markdown files are not parsed or indexed.

- [ ] **Step 5: Verify and commit**

Run: `go test ./server/mcp && go test ./... && git diff --check`

```bash
git add server/mcp/server.go server/mcp/server_test.go docs/mcp-docs.md
git commit -m "feat: expose Markdown knowledge MCP tools"
```
