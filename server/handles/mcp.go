package handles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/setting"
	"github.com/alist-org/alist/v3/pkg/utils/random"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/alist-org/alist/v3/server/mcp"
	"github.com/gin-gonic/gin"
)

type mcpConfigRequest struct {
	Enabled       bool   `json:"enabled"`
	DocsRoot      string `json:"docs_root" binding:"required"`
	MaxReadBytes  int64  `json:"max_read_bytes" binding:"required"`
	MaxWriteBytes int64  `json:"max_write_bytes" binding:"required"`
	HistoryLimit  int    `json:"history_limit" binding:"required"`
}

func GetMCPConfig(c *gin.Context) {
	common.SuccessResp(c, gin.H{
		"enabled": setting.GetBool(conf.MCPEnabled), "docs_root": setting.GetStr(conf.MCPDocsRoot),
		"max_read_bytes": setting.GetStr(conf.MCPMaxReadBytes), "max_write_bytes": setting.GetStr(conf.MCPMaxWriteBytes),
		"history_limit": setting.GetStr(conf.MCPHistoryLimit), "token_configured": setting.GetStr(conf.MCPTokenHash) != "",
	})
}

func SaveMCPConfig(c *gin.Context) {
	var req mcpConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ErrorResp(c, err, 400)
		return
	}
	req.DocsRoot = strings.TrimSpace(req.DocsRoot)
	if req.DocsRoot == "" || !strings.HasPrefix(req.DocsRoot, "/") || req.MaxReadBytes <= 0 || req.MaxWriteBytes <= 0 || req.HistoryLimit <= 0 {
		common.ErrorResp(c, fmt.Errorf("invalid MCP configuration"), 400)
		return
	}
	obj, err := fs.Get(context.Background(), req.DocsRoot, &fs.GetArgs{NoLog: true})
	if err != nil || !obj.IsDir() {
		if err == nil {
			err = fmt.Errorf("docs root must be a directory")
		}
		common.ErrorResp(c, err, 400)
		return
	}
	items := []model.SettingItem{
		{Key: conf.MCPEnabled, Value: strconv.FormatBool(req.Enabled), Type: conf.TypeBool, Group: model.MCP, Flag: model.PRIVATE},
		{Key: conf.MCPDocsRoot, Value: req.DocsRoot, Type: conf.TypeString, Group: model.MCP, Flag: model.PRIVATE},
		{Key: conf.MCPMaxReadBytes, Value: strconv.FormatInt(req.MaxReadBytes, 10), Type: conf.TypeNumber, Group: model.MCP, Flag: model.PRIVATE},
		{Key: conf.MCPMaxWriteBytes, Value: strconv.FormatInt(req.MaxWriteBytes, 10), Type: conf.TypeNumber, Group: model.MCP, Flag: model.PRIVATE},
		{Key: conf.MCPHistoryLimit, Value: strconv.Itoa(req.HistoryLimit), Type: conf.TypeNumber, Group: model.MCP, Flag: model.PRIVATE},
	}
	if err := op.SaveSettingItems(items); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if err := mcp.Reload(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	GetMCPConfig(c)
}

func ResetMCPToken(c *gin.Context) {
	token := random.Token()
	sum := sha256.Sum256([]byte(token))
	item := model.SettingItem{Key: conf.MCPTokenHash, Value: hex.EncodeToString(sum[:]), Type: conf.TypeString, Group: model.MCP, Flag: model.PRIVATE}
	if err := op.SaveSettingItem(&item); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	if err := mcp.Reload(); err != nil {
		common.ErrorResp(c, err, 500)
		return
	}
	common.SuccessResp(c, gin.H{"token": token})
}
