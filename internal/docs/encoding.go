package docs

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

func normalizeMarkdownEncoding(raw []byte) ([]byte, bool, error) {
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return raw[3:], true, nil
	}
	if utf8.Valid(raw) {
		return raw, false, nil
	}
	var decoder interface{ Bytes([]byte) ([]byte, error) }
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}):
		decoder = unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder()
	case bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		decoder = unicode.UTF16(unicode.BigEndian, unicode.ExpectBOM).NewDecoder()
	default:
		decoder = simplifiedchinese.GB18030.NewDecoder()
	}
	decoded, err := decoder.Bytes(raw)
	if err != nil || !utf8.Valid(decoded) {
		return nil, false, fmt.Errorf("unsupported document encoding")
	}
	return decoded, true, nil
}

// trimIncompleteUTF8Suffix removes only a partial final UTF-8 rune. A byte
// limited read can end midway through a Chinese character; treating that page
// as GB18030 would corrupt every preceding UTF-8 character.
func trimIncompleteUTF8Suffix(raw []byte) []byte {
	if utf8.Valid(raw) {
		return raw
	}
	start := len(raw) - utf8.UTFMax + 1
	if start < 0 {
		start = 0
	}
	for ; start < len(raw); start++ {
		if utf8.Valid(raw[:start]) && utf8.RuneStart(raw[start]) && !utf8.FullRune(raw[start:]) {
			return raw[:start]
		}
	}
	return raw
}
