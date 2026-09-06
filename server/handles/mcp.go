package handles

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/pkg/utils/random"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
)

func GetMCPConfig(c *gin.Context) {
	common.SuccessResp(c, gin.H{
		"enabled": setting.GetBool(conf.MCPEnabled), "docs_root": setting.GetStr(conf.MCPDocsRoot),
		"max_read_bytes": setting.GetStr(conf.MCPMaxReadBytes), "max_write_bytes": setting.GetStr(conf.MCPMaxWriteBytes),
		"history_limit": setting.GetStr(conf.MCPHistoryLimit), "token_configured": setting.GetStr(conf.MCPTokenHash) != "",
	})
}

func ResetMCPToken(c *gin.Context) {
	token := random.Token()
	sum := sha256.Sum256([]byte(token))
	item := model.SettingItem{Key: conf.MCPTokenHash, Value: hex.EncodeToString(sum[:]), Type: conf.TypeString, Group: model.MCP, Flag: model.PRIVATE}
	if err := op.SaveSettingItem(&item); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, gin.H{"token": token})
}
