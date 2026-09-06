# Embedded document MCP

Enable the service only after configuring a dedicated document root and a dedicated secret:

```json
{
  "mcp": {
    "enabled": true,
    "docs_root": "/knowledge",
    "token_hash": "<sha256-of-a-random-token>",
    "index_dir": "data/docs-bleve",
    "max_read_bytes": 1048576,
    "max_write_bytes": 1048576,
    "history_limit": 50
  }
}
```

Expose `https://host/mcp` only through HTTPS. Clients send `Authorization: Bearer <token>`. The raw token is never stored by AList; calculate its SHA-256 digest and store the lowercase hexadecimal result as `token_hash`.

Tools are `docs_search`, `docs_read`, `docs_update`, `docs_delete`, `docs_move`, and `docs_history`. Every path is relative to `docs_root`; only UTF-8 `.md`, `.mdx`, `.txt`, `.rst`, `.json`, `.yaml`, and `.yml` files are accepted. Updates, moves, and deletes require the `revision` returned by `docs_read`, preventing stale Agents from silently overwriting a document.

The content index is distinct from AList's filename index. If it becomes stale, disable MCP, remove the configured `index_dir`, then restart AList to rebuild it during the next writes. Disabling MCP leaves all documents and their AList storage untouched.
