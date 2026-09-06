package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/setting"
)

func LoadConfig() (conf.MCPConfig, error) {
	return configFromValues(map[string]string{
		conf.MCPEnabled: setting.GetStr(conf.MCPEnabled), conf.MCPDocsRoot: setting.GetStr(conf.MCPDocsRoot),
		conf.MCPTokenHash: setting.GetStr(conf.MCPTokenHash), conf.MCPMaxReadBytes: setting.GetStr(conf.MCPMaxReadBytes),
		conf.MCPMaxWriteBytes: setting.GetStr(conf.MCPMaxWriteBytes), conf.MCPHistoryLimit: setting.GetStr(conf.MCPHistoryLimit),
	})
}

func configFromValues(values map[string]string) (conf.MCPConfig, error) {
	cfg := conf.DefaultMCPConfig()
	cfg.Enabled = values[conf.MCPEnabled] == "true" || values[conf.MCPEnabled] == "1"
	cfg.DocsRoot = strings.TrimSpace(values[conf.MCPDocsRoot])
	cfg.TokenHash = strings.TrimSpace(values[conf.MCPTokenHash])
	if !cfg.Enabled {
		return cfg, nil
	}
	if !filepath.IsAbs(cfg.DocsRoot) {
		return cfg, fmt.Errorf("mcp docs_root must be an absolute mounted path")
	}
	if len(cfg.TokenHash) != sha256.Size*2 {
		return cfg, fmt.Errorf("mcp token is not configured")
	}
	if _, err := hex.DecodeString(cfg.TokenHash); err != nil {
		return cfg, fmt.Errorf("mcp token hash is invalid")
	}
	var err error
	if cfg.MaxReadBytes, err = positiveInt64(values[conf.MCPMaxReadBytes], cfg.MaxReadBytes); err != nil {
		return cfg, fmt.Errorf("mcp max_read_bytes: %w", err)
	}
	if cfg.MaxWriteBytes, err = positiveInt64(values[conf.MCPMaxWriteBytes], cfg.MaxWriteBytes); err != nil {
		return cfg, fmt.Errorf("mcp max_write_bytes: %w", err)
	}
	history, err := positiveInt64(values[conf.MCPHistoryLimit], int64(cfg.HistoryLimit))
	if err != nil {
		return cfg, fmt.Errorf("mcp history_limit: %w", err)
	}
	cfg.HistoryLimit = int(history)
	rootSum := sha256.Sum256([]byte(cfg.DocsRoot))
	cfg.IndexDir = filepath.Join(cfg.IndexDir, hex.EncodeToString(rootSum[:8]))
	return cfg, nil
}

func positiveInt64(raw string, fallback int64) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return v, nil
}
