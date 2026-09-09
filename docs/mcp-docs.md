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

Use `docs_query` first for daily work: it searches and returns compact context plus the recommended next action. `match_mode` is `any` (default), `exact`, or `path`; `smart` remains a compatibility alias for `any`. Use `docs_read` only for more detail: `section_id` reads the matching section, `outline=true` lists headings, and `offset`/`limit` pages a document. Use `docs_edit` for all mutations; its `kind` is `update`, `replace`, `append`, `insert_after`, `section_update`, `patch`, `delete`, or `move`, and each mutation requires the revision returned by `docs_read`. `docs_list` discovers paths and `docs_history` retrieves revisions. Every path is relative to `docs_root`; only UTF-8 `.md` files are accepted. Invalid parameters include their allowed values in the error response.

The content index is distinct from AList's filename index. Disabling MCP leaves
all documents and their AList storage untouched.
