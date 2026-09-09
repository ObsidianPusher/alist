# Markdown Knowledge MCP Design

> Historical design. Use `docs/mcp-docs.md` for the current tool contract.

## Scope

The MCP document library stores, reads, indexes and updates Markdown (`.md`)
only. It does not parse, store or index PDF, Office, HTML, EPUB or other file
formats. Existing file-management capabilities remain separate from this
knowledge interface.

## Index model

Each Markdown document is split deterministically into chunks. Headings start
a new section; long sections are split at paragraph boundaries into chunks of
at most 1,200 Unicode characters. A chunk records its document-relative path,
stable section identifier, heading breadcrumb, ordinal, source range, content,
document revision and modification time.

Bleve indexes one record per chunk. Rebuilding removes stale chunks for files
that no longer exist. Failed reads or invalid UTF-8 are recorded in index
status and do not stop other files from being indexed.

## MCP tools

- `docs_search`: keyword search over chunks. Supports `path_prefix`,
  `modified_after`, `limit`, and `cursor`; each hit returns a focused excerpt,
  path, section identifier, heading, source range, revision and score.
- `docs_read`: remains compatible and returns a byte range from a Markdown
  document.
- `docs_read_section`: returns an identified section/chunk plus optional
  adjacent chunks, allowing an Agent to inspect just the relevant context.
- `docs_list`: remains document metadata pagination, limited to Markdown.
- `docs_index_status`: reports index generation, total documents/chunks,
  last successful rebuild, and per-file extraction failures.
- `docs_rebuild_index`: requests a serialized full rebuild. It returns the
  generation identifier; callers use `docs_index_status` to observe progress.

`docs_update`, `docs_delete`, `docs_move`, and `docs_history` remain revision
guarded. Writes synchronously replace or remove that document's chunks. A
successful file operation is not rolled back if indexing later fails; status
will surface the failure and a rebuild repairs it.

## Reliability and limits

Rebuilds are single-flight. The active index remains available while a rebuild
runs. Search defaults to 20 results and caps at 100. Section reads use the
configured read-byte limit. Invalid paths, non-Markdown files, malformed
cursors, and unknown section identifiers return explicit errors.

## Verification

Tests cover Markdown chunk boundaries, heading metadata, search filters and
pagination, section reads, stale-chunk removal, failed-file status, rebuild
serialization, and existing update/delete/move index behavior. Full Go and
frontend builds remain green.
