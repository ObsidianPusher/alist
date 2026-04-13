package baidu_netdisk

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlexibleStringUnmarshalJSON(t *testing.T) {
	t.Run("number", func(t *testing.T) {
		var v FlexibleString
		require.NoError(t, json.Unmarshal([]byte(`123456789`), &v))
		require.Equal(t, FlexibleString("123456789"), v)
	})

	t.Run("string", func(t *testing.T) {
		var v FlexibleString
		require.NoError(t, json.Unmarshal([]byte(`"abc123"`), &v))
		require.Equal(t, FlexibleString("abc123"), v)
	})

	t.Run("null", func(t *testing.T) {
		var v FlexibleString
		require.NoError(t, json.Unmarshal([]byte(`null`), &v))
		require.Equal(t, FlexibleString(""), v)
	})
}

func TestDownloadRespRequestIDFlexible(t *testing.T) {
	t.Run("number", func(t *testing.T) {
		var resp DownloadResp
		require.NoError(t, json.Unmarshal([]byte(`{"errno":0,"request_id":123,"list":[{"dlink":"https://example.com"}]}`), &resp))
		require.Equal(t, FlexibleString("123"), resp.RequestId)
	})

	t.Run("string", func(t *testing.T) {
		var resp DownloadResp
		require.NoError(t, json.Unmarshal([]byte(`{"errno":0,"request_id":"123","list":[{"dlink":"https://example.com"}]}`), &resp))
		require.Equal(t, FlexibleString("123"), resp.RequestId)
	})
}
