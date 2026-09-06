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

Tools are `docs_search`, `docs_list`, `docs_read`, `docs_read_section`, `docs_update`, `docs_delete`, `docs_move`, `docs_history`, `docs_index_status`, and `docs_rebuild_index`. Markdown is indexed by heading and paragraph chunks, so `docs_search` returns a section identifier and source range. Use `docs_read_section` with that identifier to retrieve only the relevant chunk plus optional adjacent context. `docs_list` recursively lists eligible documents without returning their bodies; it accepts optional `path_prefix`, `cursor`, and `limit` (default and maximum 100). Continue until `next_cursor` is empty to enumerate the entire document root. Every path is relative to `docs_root`; only UTF-8 `.md` files are accepted. `docs_index_status` reports rebuild progress and `docs_rebuild_index` starts a serialized rebuild. Updates, moves, and deletes require the `revision` returned by `docs_read`, preventing stale Agents from silently overwriting a document.

The content index is distinct from AList's filename index. Disabling MCP leaves
all documents and their AList storage untouched.
