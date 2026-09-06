package conf

import "testing"

func TestDefaultMCPConfigIsDisabledAndBounded(t *testing.T) {
	c := DefaultConfig().MCP
	if c.Enabled {
		t.Fatal("MCP must be disabled by default")
	}
	if c.MaxReadBytes != 1024*1024 || c.MaxWriteBytes != 1024*1024 {
		t.Fatalf("unexpected document limits: read=%d write=%d", c.MaxReadBytes, c.MaxWriteBytes)
	}
	if c.HistoryLimit != 50 {
		t.Fatalf("unexpected history limit: %d", c.HistoryLimit)
	}
}
