package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/alist-org/alist/v3/internal/conf"
)

func TestConfigFromValues(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	cfg, err := configFromValues(map[string]string{
		conf.MCPEnabled: "true", conf.MCPDocsRoot: "/docs", conf.MCPTokenHash: hex.EncodeToString(sum[:]),
		conf.MCPMaxReadBytes: "1024", conf.MCPMaxWriteBytes: "2048", conf.MCPHistoryLimit: "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.DocsRoot != "/docs" || cfg.MaxReadBytes != 1024 || cfg.MaxWriteBytes != 2048 || cfg.HistoryLimit != 2 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestConfigFromValuesRejectsInvalidEnabledConfig(t *testing.T) {
	_, err := configFromValues(map[string]string{conf.MCPEnabled: "true", conf.MCPDocsRoot: "docs", conf.MCPTokenHash: "bad"})
	if err == nil {
		t.Fatal("expected invalid configuration error")
	}
}
