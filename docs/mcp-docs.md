# Embedded document MCP

Use **Management → Settings → MCP Documents** to configure the embedded MCP
service. Configuration is stored in AList's database, never in the application
configuration file. Choose a mounted folder as the document root, set limits
and history count, then save; the running MCP handler reloads immediately.

The endpoint uses the domain and base path currently open in the management
page: `https://host[/base]/mcp`. Use **Generate / Reset Token** to create a
bearer token. The raw token is shown only once in the reset response, together
with copyable Codex CLI, Claude Code CLI, and generic JSON snippets. Resetting
it immediately invalidates the prior token; AList stores only its SHA-256 hash.

Expose the endpoint only through HTTPS. Clients send `Authorization: Bearer <token>`.

Use `docs_query` first for daily work: it returns `path`, `section_id`, `snippet`, and `revision` for direct use with `docs_edit`. Its default multi-word mode is `all` (every term must match); use `any` only for broad recall, `exact` for a literal content phrase, and `path` for a literal relative document path. `smart` remains a compatibility alias for `any`. `include_context` defaults to false to avoid repeating result snippets; enable it only for clients that need a concatenated text context. Search results are revalidated against document storage before being returned, so deleted or superseded index entries are never exposed. Use `docs_read` only for more detail: `section_id` reads the matching section, `outline=true` lists headings, and `offset`/`limit` pages a document. A read without those options returns a 4096-byte UTF-8-safe first page and `next_offset` when truncated. `docs_list` pagination cursors are the last returned relative path. Use `docs_edit(kind=create)` to create a new document without a revision. Every other edit kind (`update`, `replace`, `append`, `insert_after`, `section_update`, `patch`, `delete`, or `move`) requires the revision returned by `docs_query` or `docs_read`. `docs_history(include_content=true)` returns each listed historical snapshot. Every path is relative to `docs_root`; only UTF-8 `.md` files are accepted. Invalid parameters include their allowed values in the error response.

The content index is distinct from AList's filename index. Disabling MCP leaves
all documents and their AList storage untouched.
