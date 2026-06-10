package common

import (
	"testing"

	"github.com/alist-org/alist/v3/internal/model"
)

func TestIsApply(t *testing.T) {
	datas := []struct {
		metaPath string
		reqPath  string
		applySub bool
		result   bool
	}{
		{
			metaPath: "/",
			reqPath:  "/test",
			applySub: true,
			result:   true,
		},
	}
	for i, data := range datas {
		if IsApply(data.metaPath, data.reqPath, data.applySub) != data.result {
			t.Errorf("TestIsApply %d failed", i)
		}
	}
}

func TestCanAccessIgnoreInvalidHideRule(t *testing.T) {
	user := &model.User{}
	meta := &model.Meta{
		Path: "/parent",
		Hide: "(\nsecret",
		HSub: true,
	}

	if CanAccess(user, meta, "/parent/visible", "") != true {
		t.Fatal("expected invalid hide rule to be ignored")
	}
	if CanAccess(user, meta, "/parent/secret", "") != false {
		t.Fatal("expected valid hide rule to still apply")
	}
}

func TestCanAccessNilUser(t *testing.T) {
	meta := &model.Meta{
		Path: "/parent",
		Hide: "secret",
		HSub: true,
	}

	if CanAccess(nil, meta, "/parent/secret", "") {
		t.Fatal("expected nil user to be treated as guest for hide checks")
	}
}
