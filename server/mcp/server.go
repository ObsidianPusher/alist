package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/docs"
	"github.com/gin-gonic/gin"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

type searchInput struct {
	Query      string `json:"query"`
	PathPrefix string `json:"path_prefix,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}
type readInput struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	Limit  int64  `json:"limit,omitempty"`
}
type updateInput struct {
	Path             string  `json:"path"`
	Content          string  `json:"content"`
	ExpectedRevision *string `json:"expected_revision"`
}
type deleteInput struct {
	Path             string `json:"path"`
	ExpectedRevision string `json:"expected_revision"`
}
type moveInput struct {
	From             string `json:"from"`
	To               string `json:"to"`
	ExpectedRevision string `json:"expected_revision"`
}
type historyInput struct {
	Path     string `json:"path"`
	Revision string `json:"revision,omitempty"`
}
type searchOutput struct {
	Results []docs.SearchResult `json:"results"`
}
type historyOutput struct {
	Items []docs.HistoryResult `json:"items"`
}

func Register(g *gin.RouterGroup, config conf.MCPConfig) error {
	if !config.Enabled {
		return nil
	}
	if config.TokenHash == "" {
		return fmt.Errorf("mcp token_hash is required when enabled")
	}
	svc, err := docs.New(config)
	if err != nil {
		return err
	}
	go func() { _ = svc.Rebuild(context.Background()) }()
	server := protocol.NewServer(&protocol.Implementation{Name: "alist-docs", Version: "1.0.0"}, nil)
	protocol.AddTool(server, &protocol.Tool{Name: "docs_search", Description: "Search document text by keywords"}, func(ctx context.Context, _ *protocol.CallToolRequest, in searchInput) (*protocol.CallToolResult, searchOutput, error) {
		r, e := svc.Search(in.Query, in.PathPrefix, in.Limit)
		return nil, searchOutput{Results: r}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_read", Description: "Read a document and its revision"}, func(ctx context.Context, _ *protocol.CallToolRequest, in readInput) (*protocol.CallToolResult, docs.ReadResult, error) {
		r, e := svc.Read(ctx, in.Path, in.Offset, in.Limit)
		return nil, r, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_update", Description: "Create or conditionally replace a document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in updateInput) (*protocol.CallToolResult, map[string]string, error) {
		r, e := svc.Update(ctx, in.Path, in.Content, in.ExpectedRevision)
		return nil, map[string]string{"revision": r}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_delete", Description: "Conditionally delete a document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in deleteInput) (*protocol.CallToolResult, map[string]string, error) {
		e := svc.Delete(ctx, in.Path, in.ExpectedRevision)
		return nil, map[string]string{"status": "deleted"}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_move", Description: "Conditionally move or rename a document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in moveInput) (*protocol.CallToolResult, map[string]string, error) {
		e := svc.Move(ctx, in.From, in.To, in.ExpectedRevision)
		return nil, map[string]string{"status": "moved"}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_history", Description: "List document history or retrieve a historical snapshot"}, func(ctx context.Context, _ *protocol.CallToolRequest, in historyInput) (*protocol.CallToolResult, historyOutput, error) {
		r, e := svc.History(in.Path, in.Revision)
		return nil, historyOutput{Items: r}, e
	})
	h := protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, &protocol.StreamableHTTPOptions{JSONResponse: true})
	g.Any("", auth(config, h))
	return nil
}

func auth(config conf.MCPConfig, next http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(token))
		got := hex.EncodeToString(sum[:])
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(config.TokenHash)) != 1 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		next.ServeHTTP(c.Writer, c.Request.WithContext(ctx))
	}
}
