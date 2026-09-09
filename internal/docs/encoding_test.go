package docs

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
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
