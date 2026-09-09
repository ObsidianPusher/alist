package docs

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	xunicode "golang.org/x/text/encoding/unicode"
)

func normalizeMarkdownEncoding(raw []byte) ([]byte, bool, error) {
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return raw[3:], true, nil
	}
	if utf8.Valid(raw) {
		if repaired, ok := repairHighConfidenceGB18030Mojibake(raw); ok {
			return repaired, true, nil
		}
		return raw, false, nil
	}
	var decoder interface{ Bytes([]byte) ([]byte, error) }
	switch {
	case bytes.HasPrefix(raw, []byte{0xff, 0xfe}):
		decoder = xunicode.UTF16(xunicode.LittleEndian, xunicode.ExpectBOM).NewDecoder()
	case bytes.HasPrefix(raw, []byte{0xfe, 0xff}):
		decoder = xunicode.UTF16(xunicode.BigEndian, xunicode.ExpectBOM).NewDecoder()
	default:
		decoder = simplifiedchinese.GB18030.NewDecoder()
	}
	decoded, err := decoder.Bytes(raw)
	if err != nil || !utf8.Valid(decoded) {
		return nil, false, fmt.Errorf("unsupported document encoding")
	}
	return decoded, true, nil
}

// repairHighConfidenceGB18030Mojibake repairs UTF-8 text that was previously
// decoded as GB18030. This is deliberately conservative: conversion must
// round-trip and materially improve the count of common Han characters.
func repairHighConfidenceGB18030Mojibake(raw []byte) ([]byte, bool) {
	decoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes(raw)
	if err != nil || !utf8.Valid(decoded) {
		return nil, false
	}
	roundTrip, err := simplifiedchinese.GB18030.NewDecoder().Bytes(decoded)
	if err != nil || !bytes.Equal(roundTrip, raw) {
		return nil, false
	}
	rawHan, rawScore := hanScore(string(raw))
	decodedHan, decodedScore := hanScore(string(decoded))
	if rawHan < 4 || decodedHan < 4 || decodedScore < rawScore+3 {
		return nil, false
	}
	return decoded, true
}

const commonHan = "的一是在不了有和人这中大为上个国我以要他时来用们生到作地于出就分对成会可主发年动同工也能下过子说产种面而方后多定行学法所民得经十应付关联编号全益"

func hanScore(value string) (han, common int) {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			han++
			if strings.ContainsRune(commonHan, r) {
				common++
			}
		}
	}
	return han, common
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
