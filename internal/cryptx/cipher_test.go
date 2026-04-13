package cryptx

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"testing"
)

func newTestCipher(t *testing.T, mode, enc string, dirEncrypt bool, suffix string) *Cipher {
	t.Helper()
	c, err := NewCipher(Config{
		Password:                "test-password",
		Salt:                    "test-salt",
		FilenameEncryption:      mode,
		DirectoryNameEncryption: dirEncrypt,
		FilenameEncoding:        enc,
		EncryptedSuffix:         suffix,
	})
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}
	return c
}

func TestCipherFileNameCompatibilityVectors(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		encoding   string
		dirEncrypt bool
		suffix     string
		path       string
		encrypted  string
	}{
		{
			name:       "standard-base64",
			mode:       "standard",
			encoding:   "base64",
			dirEncrypt: true,
			suffix:     ".bin",
			path:       "/dir/demo.txt",
			encrypted:  "/ZgMKcHezgBrXBYOsRj7fKQ/6Aj4MvpL96hEJ4DuklIb1A",
		},
		{
			name:       "standard-base32",
			mode:       "standard",
			encoding:   "base32",
			dirEncrypt: true,
			suffix:     ".bin",
			path:       "/dir/demo.txt",
			encrypted:  "/co1gks3nme01llo5gem4cfmv54/t04fgcnq9frqgh17g3n94kgrqg",
		},
		{
			name:       "obfuscate",
			mode:       "obfuscate",
			encoding:   "base64",
			dirEncrypt: true,
			suffix:     ".bin",
			path:       "/dir/demo.txt",
			encrypted:  "/63.rwF/51.fgoq.vzv",
		},
		{
			name:       "off",
			mode:       "off",
			encoding:   "base64",
			dirEncrypt: true,
			suffix:     ".bin",
			path:       "/dir/demo.txt",
			encrypted:  "/dir/demo.txt.bin",
		},
		{
			name:       "dir-not-encrypted",
			mode:       "standard",
			encoding:   "base64",
			dirEncrypt: false,
			suffix:     ".bin",
			path:       "/dir/demo.txt",
			encrypted:  "/dir/6Aj4MvpL96hEJ4DuklIb1A",
		},
		{
			name:       "base32768",
			mode:       "standard",
			encoding:   "base32768",
			dirEncrypt: true,
			suffix:     ".bin",
			path:       "/你好/世界.txt",
			encrypted:  "/ᛊ姘褔亴飋㨿拲監潟/屿鐜ꚰ娆⚭帙瀣㻑㤿",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestCipher(t, tc.mode, tc.encoding, tc.dirEncrypt, tc.suffix)
			if got := c.EncryptFileName(tc.path); got != tc.encrypted {
				t.Fatalf("EncryptFileName() = %q, want %q", got, tc.encrypted)
			}
			got, err := c.DecryptFileName(tc.encrypted)
			if err != nil {
				t.Fatalf("DecryptFileName failed: %v", err)
			}
			if got != tc.path {
				t.Fatalf("DecryptFileName() = %q, want %q", got, tc.path)
			}
		})
	}
}

func TestRevealCompatibility(t *testing.T) {
	const obscured = "L4qNNaHawP72eKuXecHdJKtQdYaMcFdja-PzPR0"
	got, err := Reveal(obscured)
	if err != nil {
		t.Fatalf("Reveal failed: %v", err)
	}
	if got != "test-password" {
		t.Fatalf("Reveal() = %q, want %q", got, "test-password")
	}
}

func TestDecryptDataSeekCompatibilityVector(t *testing.T) {
	c := newTestCipher(t, "standard", "base64", true, ".bin")
	raw, err := hex.DecodeString("52434c4f4e450000007ac3c46d911e882dbc1e27272b63e6b0f1fff956ada1d815a7733f79745de3ac5a815157669e0f4384b15e246a169f3de2c0d02bfccd23248c4aeba8fd0c7228ab7b3a23fec33f48aecce65d6a41177dc828cd5f9cfe9c4ea3")
	if err != nil {
		t.Fatalf("DecodeString failed: %v", err)
	}
	open := func(ctx context.Context, offset, limit int64) (io.ReadCloser, error) {
		if offset > int64(len(raw)) {
			offset = int64(len(raw))
		}
		end := int64(len(raw))
		if limit >= 0 && offset+limit < end {
			end = offset + limit
		}
		return io.NopCloser(bytes.NewReader(raw[offset:end])), nil
	}
	rc, err := c.DecryptDataSeek(context.Background(), open, 6, 11)
	if err != nil {
		t.Fatalf("DecryptDataSeek failed: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(got) != "world, this" {
		t.Fatalf("DecryptDataSeek() = %q, want %q", string(got), "world, this")
	}
}

func TestEncryptedAndDecryptedSize(t *testing.T) {
	c := newTestCipher(t, "standard", "base64", true, ".bin")
	if got := c.EncryptedSize(50); got != 98 {
		t.Fatalf("EncryptedSize() = %d, want 98", got)
	}
	if got, err := c.DecryptedSize(98); err != nil || got != 50 {
		t.Fatalf("DecryptedSize() = (%d, %v), want (50, nil)", got, err)
	}
}
