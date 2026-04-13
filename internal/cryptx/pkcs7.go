package cryptx

import "errors"

var (
	errPaddingNotFound      = errors.New("bad PKCS#7 padding - not padded")
	errPaddingNotAMultiple  = errors.New("bad PKCS#7 padding - not a multiple of blocksize")
	errPaddingTooLong       = errors.New("bad PKCS#7 padding - too long")
	errPaddingTooShort      = errors.New("bad PKCS#7 padding - too short")
	errPaddingNotAllTheSame = errors.New("bad PKCS#7 padding - not all the same")
)

func pkcs7Pad(n int, buf []byte) []byte {
	if n <= 1 || n >= 256 {
		panic("bad multiple")
	}
	padding := n - (len(buf) % n)
	for i := 0; i < padding; i++ {
		buf = append(buf, byte(padding))
	}
	return buf
}

func pkcs7Unpad(n int, buf []byte) ([]byte, error) {
	if n <= 1 || n >= 256 {
		panic("bad multiple")
	}
	length := len(buf)
	if length == 0 {
		return nil, errPaddingNotFound
	}
	if length%n != 0 {
		return nil, errPaddingNotAMultiple
	}
	padding := int(buf[length-1])
	if padding > n {
		return nil, errPaddingTooLong
	}
	if padding == 0 {
		return nil, errPaddingTooShort
	}
	for i := 0; i < padding; i++ {
		if buf[length-1-i] != byte(padding) {
			return nil, errPaddingNotAllTheSame
		}
	}
	return buf[:length-padding], nil
}
