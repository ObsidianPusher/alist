package conf

import (
	"path/filepath"

	"github.com/alist-org/alist/v3/cmd/flags"
)

type MCPConfig struct {
	Enabled       bool   `json:"enabled" env:"ENABLED"`
	DocsRoot      string `json:"docs_root" env:"DOCS_ROOT"`
	TokenHash     string `json:"token_hash" env:"TOKEN_HASH"`
	IndexDir      string `json:"index_dir" env:"INDEX_DIR"`
	MaxReadBytes  int64  `json:"max_read_bytes" env:"MAX_READ_BYTES"`
	MaxWriteBytes int64  `json:"max_write_bytes" env:"MAX_WRITE_BYTES"`
	HistoryLimit  int    `json:"history_limit" env:"HISTORY_LIMIT"`
}

func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		IndexDir:      filepath.Join(flags.DataDir, "docs-bleve"),
		MaxReadBytes:  1024 * 1024,
		MaxWriteBytes: 1024 * 1024,
		HistoryLimit:  50,
	}
}
