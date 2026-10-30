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

Use `docs_query` first for daily work: it returns `path`, `section_id`, `snippet`, and `revision` for direct use with `docs_edit`. Explicit `all` requires every searchable term; omitting the mode tries `all` and then reports any fallback to `any`. Use `any` for broad recall, `exact` for a literal content phrase, and `path` for a literal relative document path. `smart` remains a compatibility alias for `any`. `include_context` defaults to false to avoid repeating result snippets; enable it only for clients that need a concatenated text context. Search results are revalidated against document storage before being returned, so deleted or superseded index entries are never exposed. Use `docs_read` only for more detail: `section_id` reads the matching section, `outline=true` lists headings, and `offset`/`limit` pages a document. A read without those options returns a 4096-byte UTF-8-safe first page and `next_cursor` when truncated; pass it as `cursor` to continue (`next_offset` remains available for compatibility). `docs_list` pagination cursors are the last returned relative path. Use `docs_edit(kind=create)` to create a new document without a revision. Every other edit kind (`update`, `replace`, `append`, `insert_after`, `section_update`, `patch`, `delete`, or `move`) requires the revision returned by `docs_query` or `docs_read`. `docs_history(include_content=true)` returns each listed historical snapshot. Every path is relative to `docs_root`; only UTF-8 `.md` files are accepted. Invalid parameters include their allowed values in the error response.

The content index is distinct from AList's filename index. Disabling MCP leaves
all documents and their AList storage untouched.

Search behavior:

- `all`, `any`, and `exact` search both section headings and body text, including ancestor headings. Existing stored headings are queryable without rebuilding the index. Explicit `match_mode=all` requires each whitespace-separated term to match as a phrase in either field of the same section; different terms may match different fields. `exact` requires the phrase within one field and does not join heading and body text. Chinese phrases such as `启动检查` must appear consecutively in the original content (punctuation and whitespace cannot join separated characters); English stopwords are ignored consistently with the index analyzer. It never silently falls back to `any`.
- Omitting `match_mode` tries `all`, then falls back to `any` when no current document matches, reporting partial matching in `recommended_next`.
- `path_prefix` filters the full relative path, including hyphens and Chinese directory names. The index writer selects the `doc` mapping so `path` is indexed as a keyword rather than split into words. Existing indexes are repaired by the normal document rebuild after startup, including paginated cleanup of all legacy chunks with exact stored-path verification; wait for rebuild completion before checking scoped results.
- Snippets show context near a matching query term. Empty-result guidance avoids recommending the current mode or removing an absent path filter.
- Document reads return `next_cursor` when truncated. Pass it as `cursor` with the same path to continue; `offset`/`limit` remain compatible, and offsets inside UTF-8 characters round back to the character start. Editing the document invalidates its old cursors.
