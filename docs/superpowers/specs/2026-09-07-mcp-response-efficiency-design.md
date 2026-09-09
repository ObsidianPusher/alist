# MCP Response Efficiency Design

> Historical design. Use `docs/mcp-docs.md` for the current tool contract.

## Goal

Reduce agent token consumption while preserving reliable Markdown knowledge
retrieval and explicit boundaries from source-code, database, and runtime
inspection.

## Response protocol

Every successful tool call returns only `structuredContent`. The server must
set `CallToolResult.Content` to an empty list so the MCP Go SDK does not
duplicate typed output as JSON text. Error responses keep a single concise text
content item and set the protocol error flag.

## Search

The index remains chunk-based, but `docs_search` groups hits by document path
and returns only the best chunk for each path. It accepts `match_mode`:
`smart` (default text match), `exact` (exact phrase), and `path` (path/filename
match); `path_prefix` is applied in the Bleve query. Default limit is three.
Search results contain compact path, section id, heading, snippet, revision,
and a valid modification time only. They never infer that a missing document
means a missing code symbol, database object, or runtime state.

## Read limits

`docs_read` defaults to 4 KiB and caps at 32 KiB; requesting more requires
`mode=full`, still bounded by configured maximum. `docs_read_section` defaults
to 800 characters and no adjacent chunks, caps at 4 KiB. `docs_context`
defaults to 2,000 characters and caps at 5,000. Truncated reads include
`truncated` and `next_offset` for continuation.

## Modification time

Rebuild indexes AList object modification time. Direct writes use the write
time. Index search reads that stored time into results; zero times are omitted
rather than encoded as year one.

## Validation

Test JSON-RPC responses for absent duplicate text content, document-level
deduplication, exact/path matching, prefix filtering, valid time propagation,
and read continuation. Run the full Go suite and diff check.
