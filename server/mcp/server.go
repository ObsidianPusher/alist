package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
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
type listInput struct {
	PathPrefix string `json:"path_prefix,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}
type readInput struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	Limit  int64  `json:"limit,omitempty"`
}
type readSectionInput struct {
	Path      string `json:"path"`
	SectionID string `json:"section_id"`
	Adjacent  int    `json:"adjacent,omitempty"`
}
type updateInput struct {
	Path             string  `json:"path"`
	Content          string  `json:"content"`
	ExpectedRevision *string `json:"expected_revision"`
}
type replaceInput struct {
	Path             string `json:"path"`
	OldText          string `json:"old_text"`
	NewText          string `json:"new_text"`
	ExpectedRevision string `json:"expected_revision"`
}
type patchInput struct {
	Path             string `json:"path"`
	Patch            string `json:"patch"`
	ExpectedRevision string `json:"expected_revision"`
}
type sectionUpdateInput struct {
	Path             string `json:"path"`
	SectionID        string `json:"section_id"`
	Content          string `json:"content"`
	ExpectedRevision string `json:"expected_revision"`
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
type listOutput struct {
	Items      []docs.DocumentInfo `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}
type historyOutput struct {
	Items []docs.HistoryResult `json:"items"`
}
type rebuildOutput struct {
	Generation string `json:"generation"`
}

type runtimeState struct {
	handler   http.Handler
	svc       *docs.Service
	requests  atomic.Int64
	retired   atomic.Bool
	closeOnce sync.Once
}

func (s *runtimeState) release() {
	if s.requests.Add(-1) == 0 && s.retired.Load() {
		s.close()
	}
}
func (s *runtimeState) retire() {
	s.retired.Store(true)
	if s.requests.Load() == 0 {
		s.close()
	}
}
func (s *runtimeState) close() { s.closeOnce.Do(func() { _ = s.svc.Close() }) }

var active atomic.Pointer[runtimeState]

func Register(g *gin.RouterGroup) error {
	if err := Reload(); err != nil {
		return err
	}
	g.Any("", func(c *gin.Context) {
		state := active.Load()
		if state == nil {
			c.Status(http.StatusNotFound)
			return
		}
		state.requests.Add(1)
		defer state.release()
		state.handler.ServeHTTP(c.Writer, c.Request)
	})
	return nil
}

func Reload() error {
	config, err := LoadConfig()
	if err != nil {
		return err
	}
	if !config.Enabled {
		if old := active.Swap(nil); old != nil {
			old.retire()
		}
		return nil
	}
	state, err := newRuntime(config)
	if err != nil {
		return err
	}
	if old := active.Swap(state); old != nil {
		old.retire()
	}
	return nil
}

func newRuntime(config conf.MCPConfig) (*runtimeState, error) {
	if config.TokenHash == "" {
		return nil, fmt.Errorf("mcp token_hash is required when enabled")
	}
	svc, err := docs.New(config)
	if err != nil {
		return nil, err
	}
	_, _ = svc.RebuildAsync(context.Background())
	server := protocol.NewServer(&protocol.Implementation{Name: "alist-docs", Version: "1.0.0"}, nil)
	protocol.AddTool(server, &protocol.Tool{Name: "docs_search", Description: "Search document text by keywords"}, func(ctx context.Context, _ *protocol.CallToolRequest, in searchInput) (*protocol.CallToolResult, searchOutput, error) {
		r, e := svc.Search(in.Query, in.PathPrefix, in.Limit)
		return nil, searchOutput{Results: r}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_list", Description: "List documents under the configured root"}, func(ctx context.Context, _ *protocol.CallToolRequest, in listInput) (*protocol.CallToolResult, listOutput, error) {
		r, e := svc.List(ctx, in.PathPrefix, in.Cursor, in.Limit)
		return nil, listOutput(r), e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_read", Description: "Read a document and its revision"}, func(ctx context.Context, _ *protocol.CallToolRequest, in readInput) (*protocol.CallToolResult, docs.ReadResult, error) {
		r, e := svc.Read(ctx, in.Path, in.Offset, in.Limit)
		return nil, r, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_read_section", Description: "Read a Markdown section and adjacent chunks"}, func(ctx context.Context, _ *protocol.CallToolRequest, in readSectionInput) (*protocol.CallToolResult, docs.SectionResult, error) {
		r, e := svc.ReadSection(ctx, in.Path, in.SectionID, in.Adjacent)
		return nil, r, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_index_status", Description: "Get Markdown index status"}, func(context.Context, *protocol.CallToolRequest, struct{}) (*protocol.CallToolResult, docs.IndexStatus, error) {
		return nil, svc.Status(), nil
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_rebuild_index", Description: "Rebuild the Markdown index"}, func(ctx context.Context, _ *protocol.CallToolRequest, _ struct{}) (*protocol.CallToolResult, rebuildOutput, error) {
		generation, e := svc.RebuildAsync(ctx)
		return nil, rebuildOutput{Generation: generation}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_update", Description: "Create or conditionally replace a document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in updateInput) (*protocol.CallToolResult, map[string]string, error) {
		r, e := svc.Update(ctx, in.Path, in.Content, in.ExpectedRevision)
		return nil, map[string]string{"revision": r}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_replace", Description: "Replace one unique text fragment without sending the whole document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in replaceInput) (*protocol.CallToolResult, map[string]string, error) {
		r, e := svc.Replace(ctx, in.Path, in.OldText, in.NewText, in.ExpectedRevision)
		return nil, map[string]string{"revision": r}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_section_update", Description: "Replace one indexed Markdown section"}, func(ctx context.Context, _ *protocol.CallToolRequest, in sectionUpdateInput) (*protocol.CallToolResult, map[string]string, error) {
		r, e := svc.UpdateSection(ctx, in.Path, in.SectionID, in.Content, in.ExpectedRevision)
		return nil, map[string]string{"revision": r}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_patch", Description: "Apply a restricted unified diff"}, func(ctx context.Context, _ *protocol.CallToolRequest, in patchInput) (*protocol.CallToolResult, map[string]string, error) {
		r, e := svc.Patch(ctx, in.Path, in.Patch, in.ExpectedRevision)
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
	return &runtimeState{handler: auth(config, h), svc: svc}, nil
}

func auth(config conf.MCPConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(token))
		got := hex.EncodeToString(sum[:])
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(config.TokenHash)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
