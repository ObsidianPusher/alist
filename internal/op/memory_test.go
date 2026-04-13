package op

import "testing"

func TestClassifyMemoryPressure(t *testing.T) {
	const limit = int64(1024)
	cases := []struct {
		name  string
		alloc int64
		want  MemoryPressureLevel
	}{
		{name: "none", alloc: 100, want: MemoryPressureNone},
		{name: "moderate", alloc: 800, want: MemoryPressureModerate},
		{name: "high", alloc: 900, want: MemoryPressureHigh},
		{name: "critical", alloc: 980, want: MemoryPressureCritical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyMemoryPressure(limit, tc.alloc); got != tc.want {
				t.Fatalf("classifyMemoryPressure(%d, %d) = %v, want %v", limit, tc.alloc, got, tc.want)
			}
		})
	}
}

func TestTargetGCPercent(t *testing.T) {
	cases := []struct {
		level MemoryPressureLevel
		want  int
	}{
		{level: MemoryPressureNone, want: defaultGCPercent},
		{level: MemoryPressureModerate, want: moderatePressureGCPercent},
		{level: MemoryPressureHigh, want: highPressureGCPercent},
		{level: MemoryPressureCritical, want: criticalPressureGCPercent},
	}
	for _, tc := range cases {
		if got := targetGCPercent(tc.level); got != tc.want {
			t.Fatalf("targetGCPercent(%v) = %d, want %d", tc.level, got, tc.want)
		}
	}
}
