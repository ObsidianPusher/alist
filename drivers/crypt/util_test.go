package crypt

import (
	"testing"

	"github.com/alist-org/alist/v3/internal/cryptx"
)

func newTestCipher(t *testing.T) *cryptx.Cipher {
	t.Helper()
	c, err := cryptx.NewCipher(cryptx.Config{
		Password:                "test-password",
		Salt:                    "test-salt",
		FilenameEncryption:      "standard",
		DirectoryNameEncryption: true,
		FilenameEncoding:        "base64",
		EncryptedSuffix:         ".bin",
	})
	if err != nil {
		t.Fatalf("new test cipher failed: %v", err)
	}
	return c
}

func TestGuessPathEdgeCases(t *testing.T) {
	cases := []struct {
		path      string
		isFolder  bool
		secondTry bool
	}{
		{path: "", isFolder: true, secondTry: false},
		{path: "/", isFolder: true, secondTry: false},
		{path: "/dir/", isFolder: true, secondTry: false},
		{path: "/dir/file", isFolder: true, secondTry: true},
		{path: "/dir/file.txt", isFolder: false, secondTry: true},
		{path: "plain", isFolder: true, secondTry: true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			isFolder, secondTry := guessPath(tc.path)
			if isFolder != tc.isFolder || secondTry != tc.secondTry {
				t.Fatalf("guessPath(%q) = (%v, %v), want (%v, %v)", tc.path, isFolder, secondTry, tc.isFolder, tc.secondTry)
			}
		})
	}
}

func TestCryptCachesKeepStableResults(t *testing.T) {
	d := &Crypt{
		Addition: Addition{
			RemotePath: "/remote",
		},
		cipher: newTestCipher(t),
	}
	d.initCaches()

	encrypted1 := d.encryptFileName("demo.txt")
	encrypted2 := d.encryptFileName("demo.txt")
	if encrypted1 != encrypted2 {
		t.Fatalf("encrypt cache changed output")
	}
	if got := d.nameEncCache.Len(); got < 1 {
		t.Fatalf("expected encrypt cache entry, got len=%d", got)
	}

	decrypted1, err := d.decryptFileName(encrypted1)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	decrypted2, err := d.decryptFileName(encrypted1)
	if err != nil {
		t.Fatalf("cached decrypt failed: %v", err)
	}
	if decrypted1 != "demo.txt" || decrypted2 != "demo.txt" {
		t.Fatalf("decrypt cache changed output: %q %q", decrypted1, decrypted2)
	}
	if got := d.nameDecCache.Len(); got < 1 {
		t.Fatalf("expected decrypt cache entry, got len=%d", got)
	}

	remote1 := d.getPathForRemote("/dir/demo.txt", false)
	remote2 := d.getPathForRemote("/dir/demo.txt", false)
	if remote1 != remote2 {
		t.Fatalf("path cache changed output")
	}
	if got := d.pathCache.Len(); got < 1 {
		t.Fatalf("expected path cache entry, got len=%d", got)
	}
}

func TestCryptInvalidDecryptDoesNotPolluteCache(t *testing.T) {
	d := &Crypt{cipher: newTestCipher(t)}
	d.initCaches()
	if _, err := d.decryptFileName("not-a-valid-encrypted-name"); err == nil {
		t.Fatalf("expected decrypt error for invalid name")
	}
	if got := d.nameDecCache.Len(); got != 0 {
		t.Fatalf("expected invalid decrypt not cached, len=%d", got)
	}
}

func TestResolveSecretSupportsObfuscatedAndPlaintext(t *testing.T) {
	const plain = "test-password"

	got, err := resolveSecret(plain)
	if err != nil {
		t.Fatalf("resolveSecret plaintext failed: %v", err)
	}
	if got != plain {
		t.Fatalf("resolveSecret plaintext = %q, want %q", got, plain)
	}

	obscured, err := cryptx.Obscure(plain)
	if err != nil {
		t.Fatalf("Obscure failed: %v", err)
	}
	got, err = resolveSecret(obfuscatedPrefix + obscured)
	if err != nil {
		t.Fatalf("resolveSecret obscured failed: %v", err)
	}
	if got != plain {
		t.Fatalf("resolveSecret obscured = %q, want %q", got, plain)
	}
}

func TestResolveSecretEmptyString(t *testing.T) {
	got, err := resolveSecret("")
	if err != nil {
		t.Fatalf("resolveSecret empty failed: %v", err)
	}
	if got != "" {
		t.Fatalf("resolveSecret empty = %q, want empty", got)
	}
}

func TestUpdateObfusParmKeepsEmptyUnchanged(t *testing.T) {
	d := &Crypt{}
	var secret string
	if err := d.updateObfusParm(&secret); err != nil {
		t.Fatalf("updateObfusParm failed: %v", err)
	}
	if secret != "" {
		t.Fatalf("updateObfusParm empty = %q, want empty", secret)
	}
}
