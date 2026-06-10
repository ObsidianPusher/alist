package model

import "testing"

func TestCompileHidePatternsSkipsInvalidRules(t *testing.T) {
	regs := CompileHidePatterns("video\n(\n   \n.*\\.tmp$")
	if len(regs) != 2 {
		t.Fatalf("expected 2 valid rules, got %d", len(regs))
	}

	if matched, _ := regs[0].MatchString("video"); !matched {
		t.Fatal("expected first rule to match")
	}
	if matched, _ := regs[1].MatchString("demo.tmp"); !matched {
		t.Fatal("expected second rule to match")
	}
}
