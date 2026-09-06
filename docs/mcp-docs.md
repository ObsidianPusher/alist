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

Tools are `docs_search`, `docs_read`, `docs_update`, `docs_delete`, `docs_move`, and `docs_history`. Every path is relative to `docs_root`; only UTF-8 `.md`, `.mdx`, `.txt`, `.rst`, `.json`, `.yaml`, and `.yml` files are accepted. Updates, moves, and deletes require the `revision` returned by `docs_read`, preventing stale Agents from silently overwriting a document.

The content index is distinct from AList's filename index. Disabling MCP leaves
all documents and their AList storage untouched.
