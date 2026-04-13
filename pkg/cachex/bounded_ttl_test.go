package cachex

import (
	"testing"
	"time"
)

func TestBoundedTTLCache_EvictByMaxEntries(t *testing.T) {
	c := NewBoundedTTLCache[int](2, 2)
	if ok := c.Set("k1", 1, time.Minute); !ok {
		t.Fatalf("set k1 failed")
	}
	if ok := c.Set("k2", 2, time.Minute); !ok {
		t.Fatalf("set k2 failed")
	}
	_, _ = c.Get("k1") // touch k1 to make k2 least-recently-used
	if ok := c.Set("k3", 3, time.Minute); !ok {
		t.Fatalf("set k3 failed")
	}

	if _, ok := c.Get("k2"); ok {
		t.Fatalf("expect k2 evicted as LRU")
	}
	if v, ok := c.Get("k1"); !ok || v != 1 {
		t.Fatalf("expect k1 cache hit")
	}
	if v, ok := c.Get("k3"); !ok || v != 3 {
		t.Fatalf("expect k3 cache hit")
	}
}

func TestBoundedTTLCache_Expire(t *testing.T) {
	c := NewBoundedTTLCache[int](4, 2)
	if ok := c.Set("ttl", 1, 40*time.Millisecond); !ok {
		t.Fatalf("set ttl failed")
	}
	if _, ok := c.Get("ttl"); !ok {
		t.Fatalf("expect ttl key immediately available")
	}
	time.Sleep(80 * time.Millisecond)
	if _, ok := c.Get("ttl"); ok {
		t.Fatalf("expect ttl key expired")
	}
}

func TestBoundedTTLCache_GetDel(t *testing.T) {
	c := NewBoundedTTLCache[int](4, 2)
	if ok := c.Set("k", 7, time.Minute); !ok {
		t.Fatalf("set k failed")
	}
	v, ok := c.GetDel("k")
	if !ok || v != 7 {
		t.Fatalf("expect GetDel returns existing value")
	}
	if _, ok := c.Get("k"); ok {
		t.Fatalf("expect key deleted after GetDel")
	}
}

func TestBoundedTTLCache_TrimTo(t *testing.T) {
	c := NewBoundedTTLCache[int](10, 2)
	if ok := c.Set("k1", 1, time.Minute); !ok {
		t.Fatalf("set k1 failed")
	}
	if ok := c.Set("k2", 2, time.Minute); !ok {
		t.Fatalf("set k2 failed")
	}
	if ok := c.Set("k3", 3, time.Minute); !ok {
		t.Fatalf("set k3 failed")
	}
	_, _ = c.Get("k1")

	if removed := c.TrimTo(1); removed != 2 {
		t.Fatalf("TrimTo removed = %d, want 2", removed)
	}
	if _, ok := c.Get("k1"); !ok {
		t.Fatalf("expect k1 retained as most recent")
	}
	if _, ok := c.Get("k2"); ok {
		t.Fatalf("expect k2 trimmed")
	}
	if _, ok := c.Get("k3"); ok {
		t.Fatalf("expect k3 trimmed")
	}
}
