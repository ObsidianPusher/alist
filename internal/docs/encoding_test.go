package docs

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	xunicode "golang.org/x/text/encoding/unicode"
)

func TestNormalizeMarkdownEncodingProducesUTF8WithoutBOM(t *testing.T) {
	gbk, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("钢渣外卖"))
	require.NoError(t, err)
	text, changed, err := normalizeMarkdownEncoding(gbk)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "钢渣外卖", string(text))
	text, changed, err = normalizeMarkdownEncoding(append([]byte{0xef, 0xbb, 0xbf}, []byte("ok")...))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "ok", string(text))
}

func TestNormalizeMarkdownEncodingRepairsHighConfidenceGB18030Mojibake(t *testing.T) {
	text, changed, err := normalizeMarkdownEncoding([]byte("鐩婂拰搴斾粯鍏宠仈缂栧彿鍏ㄤ负AAA"))
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "益和应付关联编号全为AAA", string(text))
}

func TestNormalizeMarkdownEncodingPreservesOrdinaryUTF8(t *testing.T) {
	text, changed, err := normalizeMarkdownEncoding([]byte("钢渣外卖"))
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, "钢渣外卖", string(text))
}

func TestNormalizeMarkdownEncodingConvertsUTF16WithBOM(t *testing.T) {
	for _, encoding := range []interface{ Bytes([]byte) ([]byte, error) }{
		xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM).NewEncoder(),
		xunicode.UTF16(xunicode.BigEndian, xunicode.UseBOM).NewEncoder(),
	} {
		raw, err := encoding.Bytes([]byte("钢渣外卖"))
		require.NoError(t, err)
		text, changed, err := normalizeMarkdownEncoding(raw)
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, "钢渣外卖", string(text))
	}
}

func TestNormalizeMarkdownEncodingRejectsUndecodableBytes(t *testing.T) {
	_, _, err := normalizeMarkdownEncoding([]byte{0xff})
	require.Error(t, err)
}
