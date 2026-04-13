package cryptx

import (
	"bytes"
	"context"
	"crypto/aes"
	stdcipher "crypto/cipher"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Max-Sum/base32768"
	"github.com/rfjakob/eme"
	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
)

const (
	nameCipherBlockSize = aes.BlockSize
	fileMagic           = "RCLONE\x00\x00"
	fileMagicSize       = len(fileMagic)
	fileNonceSize       = 24
	fileHeaderSize      = fileMagicSize + fileNonceSize
	blockHeaderSize     = secretbox.Overhead
	blockDataSize       = 64 * 1024
	blockSize           = blockHeaderSize + blockDataSize
)

var (
	ErrBadDecryptUTF8          = errors.New("bad decryption - utf-8 invalid")
	ErrBadDecryptControlChar   = errors.New("bad decryption - contains control chars")
	ErrNotAMultipleOfBlocksize = errors.New("not a multiple of blocksize")
	ErrTooShortAfterDecode     = errors.New("too short after base32 decode")
	ErrTooLongAfterDecode      = errors.New("too long after base32 decode")
	ErrEncryptedFileTooShort   = errors.New("file is too short to be encrypted")
	ErrEncryptedFileBadHeader  = errors.New("file has truncated block header")
	ErrEncryptedBadMagic       = errors.New("not an encrypted file - bad magic string")
	ErrEncryptedBadBlock       = errors.New("failed to authenticate decrypted block - bad password?")
	ErrBadBase32Encoding       = errors.New("bad base32 filename encoding")
	ErrFileClosed              = errors.New("file already closed")
	ErrNotAnEncryptedFile      = errors.New("not an encrypted file - does not match suffix")
	ErrBadSeek                 = errors.New("Seek beyond end of file")
)

var defaultSalt = []byte{0xA8, 0x0D, 0xF4, 0x3A, 0x8F, 0xBD, 0x03, 0x08, 0xA7, 0xCA, 0xB8, 0x3E, 0x58, 0x1F, 0x86, 0xB1}

var fileMagicBytes = []byte(fileMagic)

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

type OpenRangeSeek func(ctx context.Context, offset, limit int64) (io.ReadCloser, error)

type NameEncryptionMode int

const (
	NameEncryptionOff NameEncryptionMode = iota
	NameEncryptionStandard
	NameEncryptionObfuscated
)

func NewNameEncryptionMode(s string) (NameEncryptionMode, error) {
	switch strings.ToLower(s) {
	case "off":
		return NameEncryptionOff, nil
	case "standard":
		return NameEncryptionStandard, nil
	case "obfuscate":
		return NameEncryptionObfuscated, nil
	default:
		return NameEncryptionOff, fmt.Errorf("unknown file name encryption mode %q", s)
	}
}

type fileNameEncoding interface {
	EncodeToString(src []byte) string
	DecodeString(s string) ([]byte, error)
}

type caseInsensitiveBase32Encoding struct{}

func (caseInsensitiveBase32Encoding) EncodeToString(src []byte) string {
	encoded := base32.HexEncoding.EncodeToString(src)
	encoded = strings.TrimRight(encoded, "=")
	return strings.ToLower(encoded)
}

func (caseInsensitiveBase32Encoding) DecodeString(s string) ([]byte, error) {
	if strings.HasSuffix(s, "=") {
		return nil, ErrBadBase32Encoding
	}
	roundUpToMultipleOf8 := (len(s) + 7) &^ 7
	equals := roundUpToMultipleOf8 - len(s)
	s = strings.ToUpper(s) + "========"[:equals]
	return base32.HexEncoding.DecodeString(s)
}

func NewNameEncoding(s string) (fileNameEncoding, error) {
	switch strings.ToLower(s) {
	case "base32":
		return caseInsensitiveBase32Encoding{}, nil
	case "base64":
		return base64.RawURLEncoding, nil
	case "base32768":
		return base32768.SafeEncoding, nil
	default:
		return nil, fmt.Errorf("unknown file name encoding mode %q", s)
	}
}

type Config struct {
	Password                string
	Salt                    string
	FilenameEncryption      string
	DirectoryNameEncryption bool
	FilenameEncoding        string
	EncryptedSuffix         string
	PassBadBlocks           bool
}

type Cipher struct {
	dataKey         [32]byte
	nameKey         [32]byte
	nameTweak       [nameCipherBlockSize]byte
	block           stdcipher.Block
	mode            NameEncryptionMode
	fileNameEnc     fileNameEncoding
	buffers         sync.Pool
	cryptoRand      io.Reader
	dirNameEncrypt  bool
	passBadBlocks   bool
	encryptedSuffix string
}

func NewCipher(cfg Config) (*Cipher, error) {
	mode, err := NewNameEncryptionMode(cfg.FilenameEncryption)
	if err != nil {
		return nil, err
	}
	enc, err := NewNameEncoding(cfg.FilenameEncoding)
	if err != nil {
		return nil, err
	}
	c := &Cipher{
		mode:            mode,
		fileNameEnc:     enc,
		cryptoRand:      rand.Reader,
		dirNameEncrypt:  cfg.DirectoryNameEncryption,
		passBadBlocks:   cfg.PassBadBlocks,
		encryptedSuffix: ".bin",
	}
	if cfg.EncryptedSuffix != "" {
		c.encryptedSuffix = cfg.EncryptedSuffix
	}
	c.buffers.New = func() any {
		return new([blockSize]byte)
	}
	if err := c.key(cfg.Password, cfg.Salt); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cipher) key(password, salt string) error {
	const keySize = 32 + 32 + nameCipherBlockSize
	saltBytes := defaultSalt
	if salt != "" {
		saltBytes = []byte(salt)
	}
	var key []byte
	var err error
	if password == "" {
		key = make([]byte, keySize)
	} else {
		key, err = scrypt.Key([]byte(password), saltBytes, 16384, 8, 1, keySize)
		if err != nil {
			return err
		}
	}
	copy(c.dataKey[:], key[:32])
	copy(c.nameKey[:], key[32:64])
	copy(c.nameTweak[:], key[64:])
	c.block, err = aes.NewCipher(c.nameKey[:])
	return err
}

func (c *Cipher) getBlock() *[blockSize]byte { return c.buffers.Get().(*[blockSize]byte) }
func (c *Cipher) putBlock(buf *[blockSize]byte) {
	if buf != nil {
		c.buffers.Put(buf)
	}
}

func (c *Cipher) EncryptFileName(in string) string {
	if c.mode == NameEncryptionOff {
		return in + c.encryptedSuffix
	}
	return c.encryptFileName(in)
}

func (c *Cipher) EncryptDirName(in string) string {
	if c.mode == NameEncryptionOff || !c.dirNameEncrypt {
		return in
	}
	return c.encryptFileName(in)
}

func (c *Cipher) DecryptFileName(in string) (string, error) {
	if c.mode == NameEncryptionOff {
		remainingLength := len(in) - len(c.encryptedSuffix)
		if remainingLength == 0 || !strings.HasSuffix(in, c.encryptedSuffix) {
			return "", ErrNotAnEncryptedFile
		}
		decrypted := in[:remainingLength]
		if matchVersion(decrypted) {
			_, unversioned := removeVersion(decrypted)
			if unversioned == "" {
				return "", ErrNotAnEncryptedFile
			}
		}
		return decrypted, nil
	}
	return c.decryptFileName(in)
}

func (c *Cipher) DecryptDirName(in string) (string, error) {
	if c.mode == NameEncryptionOff || !c.dirNameEncrypt {
		return in, nil
	}
	return c.decryptFileName(in)
}

func (c *Cipher) encryptFileName(in string) string {
	segments := strings.Split(in, "/")
	for i := range segments {
		if !c.dirNameEncrypt && i != len(segments)-1 {
			continue
		}
		hasVersion := false
		var ts time.Time
		if i == len(segments)-1 && matchVersion(segments[i]) {
			var s string
			ts, s = removeVersion(segments[i])
			if s != segments[i] {
				segments[i] = s
				hasVersion = true
			}
		}
		if c.mode == NameEncryptionStandard {
			segments[i] = c.encryptSegment(segments[i])
		} else {
			segments[i] = c.obfuscateSegment(segments[i])
		}
		if hasVersion {
			segments[i] = addVersion(segments[i], ts)
		}
	}
	return strings.Join(segments, "/")
}

func (c *Cipher) decryptFileName(in string) (string, error) {
	segments := strings.Split(in, "/")
	for i := range segments {
		if !c.dirNameEncrypt && i != len(segments)-1 {
			continue
		}
		hasVersion := false
		var ts time.Time
		if i == len(segments)-1 && matchVersion(segments[i]) {
			var s string
			ts, s = removeVersion(segments[i])
			if s != segments[i] {
				segments[i] = s
				hasVersion = true
			}
		}
		var err error
		if c.mode == NameEncryptionStandard {
			segments[i], err = c.decryptSegment(segments[i])
		} else {
			segments[i], err = c.deobfuscateSegment(segments[i])
		}
		if err != nil {
			return "", err
		}
		if hasVersion {
			segments[i] = addVersion(segments[i], ts)
		}
	}
	return strings.Join(segments, "/"), nil
}

func (c *Cipher) encryptSegment(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	padded := pkcs7Pad(nameCipherBlockSize, []byte(plaintext))
	ciphertext := eme.Transform(c.block, c.nameTweak[:], padded, eme.DirectionEncrypt)
	return c.fileNameEnc.EncodeToString(ciphertext)
}

func (c *Cipher) decryptSegment(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	raw, err := c.fileNameEnc.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	if len(raw)%nameCipherBlockSize != 0 {
		return "", ErrNotAMultipleOfBlocksize
	}
	if len(raw) == 0 {
		return "", ErrTooShortAfterDecode
	}
	if len(raw) > 2048 {
		return "", ErrTooLongAfterDecode
	}
	padded := eme.Transform(c.block, c.nameTweak[:], raw, eme.DirectionDecrypt)
	plaintext, err := pkcs7Unpad(nameCipherBlockSize, padded)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(plaintext) {
		return "", ErrBadDecryptUTF8
	}
	for _, r := range string(plaintext) {
		if r >= 0 && r < 0x20 {
			return "", ErrBadDecryptControlChar
		}
	}
	return string(plaintext), nil
}

const obfuscQuoteRune = '!'

func (c *Cipher) obfuscateSegment(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	if !utf8.ValidString(plaintext) {
		return "!." + plaintext
	}
	dir := 0
	for _, rv := range plaintext {
		dir += int(rv)
	}
	dir %= 256
	var result bytes.Buffer
	_, _ = result.WriteString(strconv.Itoa(dir) + ".")
	for i := 0; i < len(c.nameKey); i++ {
		dir += int(c.nameKey[i])
	}
	for _, rv := range plaintext {
		switch {
		case rv == obfuscQuoteRune:
			_, _ = result.WriteRune(obfuscQuoteRune)
			_, _ = result.WriteRune(obfuscQuoteRune)
		case rv >= '0' && rv <= '9':
			shift := (dir % 9) + 1
			_, _ = result.WriteRune(rune('0' + (int(rv)-'0'+shift)%10))
		case (rv >= 'A' && rv <= 'Z') || (rv >= 'a' && rv <= 'z'):
			shift := (dir % 25) + 1
			pos := int(rv - 'A')
			if pos >= 26 {
				pos -= 6
			}
			pos = (pos + shift) % 52
			if pos >= 26 {
				pos += 6
			}
			_, _ = result.WriteRune(rune('A' + pos))
		case rv >= 0xA0 && rv <= 0xFF:
			shift := (dir % 95) + 1
			_, _ = result.WriteRune(rune(0xA0 + (int(rv)-0xA0+shift)%96))
		case rv >= 0x100:
			shift := (dir % 127) + 1
			base := int(rv - rv%256)
			newRune := rune(base + (int(rv)-base+shift)%256)
			if !utf8.ValidRune(newRune) {
				_, _ = result.WriteRune(obfuscQuoteRune)
				_, _ = result.WriteRune(rv)
			} else {
				_, _ = result.WriteRune(newRune)
			}
		default:
			_, _ = result.WriteRune(rv)
		}
	}
	return result.String()
}

func (c *Cipher) deobfuscateSegment(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	pos := strings.Index(ciphertext, ".")
	if pos == -1 {
		return "", ErrNotAnEncryptedFile
	}
	num := ciphertext[:pos]
	if num == "!" {
		return ciphertext[pos+1:], nil
	}
	dir, err := strconv.Atoi(num)
	if err != nil {
		return "", ErrNotAnEncryptedFile
	}
	for i := 0; i < len(c.nameKey); i++ {
		dir += int(c.nameKey[i])
	}
	var result bytes.Buffer
	inQuote := false
	for _, rv := range ciphertext[pos+1:] {
		switch {
		case inQuote:
			_, _ = result.WriteRune(rv)
			inQuote = false
		case rv == obfuscQuoteRune:
			inQuote = true
		case rv >= '0' && rv <= '9':
			shift := (dir % 9) + 1
			n := '0' + int(rv) - '0' - shift
			if n < '0' {
				n += 10
			}
			_, _ = result.WriteRune(rune(n))
		case (rv >= 'A' && rv <= 'Z') || (rv >= 'a' && rv <= 'z'):
			shift := (dir % 25) + 1
			pos := int(rv - 'A')
			if pos >= 26 {
				pos -= 6
			}
			pos -= shift
			if pos < 0 {
				pos += 52
			}
			if pos >= 26 {
				pos += 6
			}
			_, _ = result.WriteRune(rune('A' + pos))
		case rv >= 0xA0 && rv <= 0xFF:
			shift := (dir % 95) + 1
			n := 0xA0 + int(rv) - 0xA0 - shift
			if n < 0xA0 {
				n += 96
			}
			_, _ = result.WriteRune(rune(n))
		case rv >= 0x100:
			shift := (dir % 127) + 1
			base := int(rv - rv%256)
			newRune := rune(base + (int(rv) - base - shift))
			if int(newRune) < base {
				newRune += 256
			}
			_, _ = result.WriteRune(newRune)
		default:
			_, _ = result.WriteRune(rv)
		}
	}
	return result.String(), nil
}

type nonce [fileNonceSize]byte

func (n *nonce) pointer() *[fileNonceSize]byte { return (*[fileNonceSize]byte)(n) }
func (n *nonce) fromReader(in io.Reader) error {
	read, err := readFill(in, n[:])
	if read != fileNonceSize {
		return fmt.Errorf("short read of nonce: %w", err)
	}
	return nil
}
func (n *nonce) fromBuf(buf []byte) {
	if copy(n[:], buf) != fileNonceSize {
		panic("buffer too short to read nonce")
	}
}
func (n *nonce) carry(i int) {
	for ; i < len(*n); i++ {
		digit := (*n)[i]
		newDigit := digit + 1
		(*n)[i] = newDigit
		if newDigit >= digit {
			break
		}
	}
}
func (n *nonce) increment() { n.carry(0) }
func (n *nonce) add(x uint64) {
	carry := uint16(0)
	for i := 0; i < 8; i++ {
		digit := (*n)[i]
		xDigit := byte(x)
		x >>= 8
		carry += uint16(digit) + uint16(xDigit)
		(*n)[i] = byte(carry)
		carry >>= 8
	}
	if carry != 0 {
		n.carry(8)
	}
}

type encrypter struct {
	mu       sync.Mutex
	in       io.Reader
	c        *Cipher
	nonce    nonce
	buf      *[blockSize]byte
	readBuf  *[blockSize]byte
	bufIndex int
	bufSize  int
	err      error
}

func (c *Cipher) newEncrypter(in io.Reader, n *nonce) (*encrypter, error) {
	e := &encrypter{
		in:      in,
		c:       c,
		buf:     c.getBlock(),
		readBuf: c.getBlock(),
		bufSize: fileHeaderSize,
	}
	if n != nil {
		e.nonce = *n
	} else if err := e.nonce.fromReader(c.cryptoRand); err != nil {
		return nil, err
	}
	copy(e.buf[:], fileMagicBytes)
	copy(e.buf[fileMagicSize:], e.nonce[:])
	return e, nil
}

func (e *encrypter) Read(p []byte) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return 0, e.err
	}
	if e.bufIndex >= e.bufSize {
		readBuf := e.readBuf[:blockDataSize]
		n, err := readFill(e.in, readBuf)
		if n == 0 {
			return e.finish(err)
		}
		secretbox.Seal(e.buf[:0], readBuf[:n], e.nonce.pointer(), &e.c.dataKey)
		e.bufIndex = 0
		e.bufSize = blockHeaderSize + n
		e.nonce.increment()
	}
	n := copy(p, e.buf[e.bufIndex:e.bufSize])
	e.bufIndex += n
	return n, nil
}

func (e *encrypter) finish(err error) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	e.err = err
	e.c.putBlock(e.buf)
	e.c.putBlock(e.readBuf)
	e.buf = nil
	e.readBuf = nil
	return 0, err
}

func (c *Cipher) EncryptData(in io.Reader) (io.Reader, error) {
	return c.newEncrypter(in, nil)
}

type decrypter struct {
	mu           sync.Mutex
	rc           io.ReadCloser
	nonce        nonce
	initialNonce nonce
	c            *Cipher
	buf          *[blockSize]byte
	readBuf      *[blockSize]byte
	bufIndex     int
	bufSize      int
	err          error
	limit        int64
	open         OpenRangeSeek
}

func (c *Cipher) newDecrypter(rc io.ReadCloser) (*decrypter, error) {
	d := &decrypter{
		rc:      rc,
		c:       c,
		buf:     c.getBlock(),
		readBuf: c.getBlock(),
		limit:   -1,
	}
	readBuf := d.readBuf[:fileHeaderSize]
	n, err := readFill(d.rc, readBuf)
	if n < fileHeaderSize && err == io.EOF {
		return nil, d.finishAndClose(ErrEncryptedFileTooShort)
	} else if err != nil && err != io.EOF {
		return nil, d.finishAndClose(err)
	}
	if !bytes.Equal(readBuf[:fileMagicSize], fileMagicBytes) {
		return nil, d.finishAndClose(ErrEncryptedBadMagic)
	}
	d.nonce.fromBuf(readBuf[fileMagicSize:])
	d.initialNonce = d.nonce
	return d, nil
}

func (c *Cipher) newDecrypterSeek(ctx context.Context, open OpenRangeSeek, offset, limit int64) (*decrypter, error) {
	var rc io.ReadCloser
	var err error
	doRangeSeek := false
	setLimit := false
	switch {
	case offset == 0 && limit < 0:
		rc, err = open(ctx, 0, -1)
	case offset == 0:
		_, underlyingLimit, _, _ := calculateUnderlying(offset, limit)
		rc, err = open(ctx, 0, int64(fileHeaderSize)+underlyingLimit)
		setLimit = true
	default:
		rc, err = open(ctx, 0, int64(fileHeaderSize))
		doRangeSeek = true
	}
	if err != nil {
		return nil, err
	}
	d, err := c.newDecrypter(rc)
	if err != nil {
		return nil, err
	}
	d.open = open
	if doRangeSeek {
		if _, err := d.RangeSeek(ctx, offset, io.SeekStart, limit); err != nil {
			_ = d.Close()
			return nil, err
		}
	}
	if setLimit {
		d.limit = limit
	}
	return d, nil
}

func (d *decrypter) fillBuffer() error {
	n, err := readFill(d.rc, d.readBuf[:])
	if n == 0 {
		return err
	}
	if n <= blockHeaderSize {
		if err != nil && err != io.EOF {
			return err
		}
		return ErrEncryptedFileBadHeader
	}
	if _, ok := secretbox.Open(d.buf[:0], d.readBuf[:n], d.nonce.pointer(), &d.c.dataKey); !ok {
		if err != nil && err != io.EOF {
			return err
		}
		if !d.c.passBadBlocks {
			return ErrEncryptedBadBlock
		}
		for i := range d.buf[:n] {
			d.buf[i] = 0
		}
	}
	d.bufIndex = 0
	d.bufSize = n - blockHeaderSize
	d.nonce.increment()
	return nil
}

func (d *decrypter) Read(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return 0, d.err
	}
	if d.bufIndex >= d.bufSize {
		if err := d.fillBuffer(); err != nil {
			return 0, d.finish(err)
		}
	}
	toCopy := d.bufSize - d.bufIndex
	if d.limit >= 0 && d.limit < int64(toCopy) {
		toCopy = int(d.limit)
	}
	n := copy(p, d.buf[d.bufIndex:d.bufIndex+toCopy])
	d.bufIndex += n
	if d.limit >= 0 {
		d.limit -= int64(n)
		if d.limit == 0 {
			return n, d.finish(io.EOF)
		}
	}
	return n, nil
}

func calculateUnderlying(offset, limit int64) (underlyingOffset, underlyingLimit, discard, blocks int64) {
	blocks, discard = offset/blockDataSize, offset%blockDataSize
	underlyingOffset = int64(fileHeaderSize) + blocks*(blockHeaderSize+blockDataSize)
	underlyingLimit = -1
	if limit >= 0 {
		bytesToRead := limit - (blockDataSize - discard)
		blocksToRead := int64(1)
		if bytesToRead > 0 {
			extra, endBytes := bytesToRead/blockDataSize, bytesToRead%blockDataSize
			if endBytes != 0 {
				extra++
			}
			blocksToRead += extra
		}
		underlyingLimit = blocksToRead * (blockHeaderSize + blockDataSize)
	}
	return
}

func (d *decrypter) RangeSeek(ctx context.Context, offset int64, whence int, limit int64) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.open == nil {
		return 0, d.finish(errors.New("can't seek - not initialised with newDecrypterSeek"))
	}
	if whence != io.SeekStart {
		return 0, d.finish(errors.New("can only seek from the start"))
	}
	if d.err == io.EOF {
		d.unFinish()
	} else if d.err != nil {
		return 0, d.err
	}
	underlyingOffset, underlyingLimit, discard, blocks := calculateUnderlying(offset, limit)
	d.nonce = d.initialNonce
	d.nonce.add(uint64(blocks))
	_ = d.rc.Close()
	d.rc = nil
	rc, err := d.open(ctx, underlyingOffset, underlyingLimit)
	if err != nil {
		return 0, d.finish(fmt.Errorf("couldn't reopen file with offset and limit: %w", err))
	}
	d.rc = rc
	if err := d.fillBuffer(); err != nil {
		return 0, d.finish(err)
	}
	if int(discard) > d.bufSize {
		return 0, d.finish(ErrBadSeek)
	}
	d.bufIndex = int(discard)
	d.limit = limit
	return offset, nil
}

func (d *decrypter) Seek(offset int64, whence int) (int64, error) {
	return d.RangeSeek(context.TODO(), offset, whence, -1)
}

func (d *decrypter) finish(err error) error {
	if d.err != nil {
		return d.err
	}
	d.err = err
	d.c.putBlock(d.buf)
	d.c.putBlock(d.readBuf)
	d.buf = nil
	d.readBuf = nil
	return err
}

func (d *decrypter) unFinish() {
	d.err = nil
	d.buf = d.c.getBlock()
	d.readBuf = d.c.getBlock()
	d.bufIndex = 0
	d.bufSize = 0
}

func (d *decrypter) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err == ErrFileClosed {
		return d.err
	}
	if d.err == nil {
		_ = d.finish(io.EOF)
	}
	d.err = ErrFileClosed
	if d.rc == nil {
		return nil
	}
	return d.rc.Close()
}

func (d *decrypter) finishAndClose(err error) error {
	_ = d.finish(err)
	_ = d.Close()
	return err
}

func (c *Cipher) DecryptDataSeek(ctx context.Context, open OpenRangeSeek, offset, limit int64) (ReadSeekCloser, error) {
	return c.newDecrypterSeek(ctx, open, offset, limit)
}

func (c *Cipher) EncryptedSize(size int64) int64 {
	blocks, residue := size/blockDataSize, size%blockDataSize
	encryptedSize := int64(fileHeaderSize) + blocks*(blockHeaderSize+blockDataSize)
	if residue != 0 {
		encryptedSize += blockHeaderSize + residue
	}
	return encryptedSize
}

func (c *Cipher) DecryptedSize(size int64) (int64, error) {
	size -= int64(fileHeaderSize)
	if size < 0 {
		return 0, ErrEncryptedFileTooShort
	}
	blocks, residue := size/blockSize, size%blockSize
	decryptedSize := blocks * blockDataSize
	if residue != 0 {
		residue -= blockHeaderSize
		if residue <= 0 {
			return 0, ErrEncryptedFileBadHeader
		}
	}
	decryptedSize += residue
	return decryptedSize, nil
}

func readFill(r io.Reader, buf []byte) (n int, err error) {
	for n < len(buf) && err == nil {
		var nn int
		nn, err = r.Read(buf[n:])
		n += nn
	}
	return n, err
}
