package docs

import "testing"

func TestResolvePathRejectsRootEscape(t *testing.T) {
	_, err := ResolvePath("/knowledge", "../secret.md")
	if err == nil {
		t.Fatal("expected escaped path to be rejected")
	}
}

func TestResolvePathAcceptsRelativeMarkdown(t *testing.T) {
	got, err := ResolvePath("/knowledge", "guide/intro.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/knowledge/guide/intro.md" {
		t.Fatalf("unexpected path: %q", got)
	}
}

func TestResolvePathRejectsUnsupportedExtension(t *testing.T) {
	_, err := ResolvePath("/knowledge", "archive.zip")
	if err == nil {
		t.Fatal("expected unsupported extension to be rejected")
	}
}
