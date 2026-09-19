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
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/gin-gonic/gin"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

type searchInput struct {
	Query      string `json:"query"`
	PathPrefix string `json:"path_prefix,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	MatchMode  string `json:"match_mode,omitempty"`
}
type queryInput struct {
	Query          string `json:"query"`
	PathPrefix     string `json:"path_prefix,omitempty"`
	MatchMode      string `json:"match_mode,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	BudgetChars    int    `json:"budget_chars,omitempty"`
	IncludeContext bool   `json:"include_context,omitempty"`
}
type listInput struct {
	PathPrefix string `json:"path_prefix,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}
type readInput struct {
	Path            string `json:"path"`
	Offset          int64  `json:"offset,omitempty"`
	Limit           int64  `json:"limit,omitempty"`
	IncludeRevision *bool  `json:"include_revision,omitempty"`
	SectionID       string `json:"section_id,omitempty"`
	Adjacent        int    `json:"adjacent,omitempty"`
	Outline         bool   `json:"outline,omitempty"`
}
type documentOutput struct {
	Path       string                `json:"path"`
	Revision   string                `json:"revision"`
	Content    string                `json:"content,omitempty"`
	Truncated  bool                  `json:"truncated,omitempty"`
	NextOffset int64                 `json:"next_offset,omitempty"`
	Sections   []docs.OutlineSection `json:"sections,omitempty"`
	Chunks     []docs.Chunk          `json:"chunks,omitempty"`
}
type editInput struct {
	Kind             string  `json:"kind"`
	Path             string  `json:"path"`
	To               string  `json:"to,omitempty"`
	ExpectedRevision *string `json:"expected_revision,omitempty"`
	Content          string  `json:"content,omitempty"`
	OldText          string  `json:"old_text,omitempty"`
	NewText          string  `json:"new_text,omitempty"`
	SectionID        string  `json:"section_id,omitempty"`
	Anchor           string  `json:"anchor,omitempty"`
	Patch            string  `json:"patch,omitempty"`
}

func requiredRevision(value *string) (string, error) {
	if value == nil || *value == "" {
		return "", fmt.Errorf("expected_revision is required for editing an existing document; use kind=create for a new document")
	}
	return *value, nil
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
type outlineInput struct {
	Path string `json:"path"`
}
type appendInput struct {
	Path             string `json:"path"`
	SectionID        string `json:"section_id"`
	Content          string `json:"content"`
	ExpectedRevision string `json:"expected_revision"`
}
type insertAfterInput struct {
	Path             string `json:"path"`
	Anchor           string `json:"anchor"`
	Content          string `json:"content"`
	ExpectedRevision string `json:"expected_revision"`
}
type contextInput struct {
	Query       string `json:"query"`
	PathPrefix  string `json:"path_prefix,omitempty"`
	BudgetChars int    `json:"budget_chars,omitempty"`
}
type diffInput struct {
	Path         string `json:"path"`
	FromRevision string `json:"from_revision"`
}
type batchInput struct {
	Edits []docs.BatchEdit `json:"edits"`
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
	Path           string `json:"path"`
	Revision       string `json:"revision,omitempty"`
	IncludeContent bool   `json:"include_content,omitempty"`
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

func success() *protocol.CallToolResult {
	// Leaving Content unset lets the SDK emit a JSON text projection of the
	// structured result for clients that do not consume structuredContent.
	return &protocol.CallToolResult{}
}

type runtimeState struct {
	handler         http.Handler
	svc             *docs.Service
	maintenanceStop chan struct{}
	requests        atomic.Int64
	retired         atomic.Bool
	closeOnce       sync.Once
}

const encodingMaintenanceInterval = 6 * time.Hour

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
func (s *runtimeState) close() {
	s.closeOnce.Do(func() {
		if s.maintenanceStop != nil {
			close(s.maintenanceStop)
		}
		if s.svc != nil {
			_ = s.svc.Close()
		}
	})
}

func (s *runtimeState) startMaintenance() {
	if s.maintenanceStop == nil || s.svc == nil {
		return
	}
	utils.GoSafe("mcp-document-maintenance", func() {
		ticker := time.NewTicker(encodingMaintenanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = s.svc.RebuildAsync(context.Background())
			case <-s.maintenanceStop:
				return
			}
		}
	})
}

var active atomic.Pointer[runtimeState]

func Status() (docs.IndexStatus, bool) {
	state := active.Load()
	if state == nil || state.svc == nil {
		return docs.IndexStatus{}, false
	}
	return state.svc.Status(), true
}

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
	protocol.AddTool(server, &protocol.Tool{Name: "docs_query", Description: "Recommended daily entry point: search results include path, section_id, snippet, and revision. Set include_context=true only when a concatenated text context is needed. match_mode is all (default), any (broad recall; smart is a legacy alias), exact (literal content phrase), or path (literal relative document path). Use docs_read only when the returned snippet is insufficient."}, func(ctx context.Context, _ *protocol.CallToolRequest, in queryInput) (*protocol.CallToolResult, docs.QueryResult, error) {
		r, e := svc.Query(in.Query, in.PathPrefix, in.MatchMode, in.Limit, in.BudgetChars)
		if !in.IncludeContext {
			r.Context = ""
		}
		return success(), r, e
	})
	if false { // retired low-level tools; retained temporarily only as implementation reference
		protocol.AddTool(server, &protocol.Tool{Name: "docs_diff", Description: "Compare a historical snapshot with the current document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in diffInput) (*protocol.CallToolResult, docs.DiffResult, error) {
			r, e := svc.Diff(ctx, in.Path, in.FromRevision)
			return success(), r, e
		})
	}
	if false {
		protocol.AddTool(server, &protocol.Tool{Name: "docs_batch_edit", Description: "Prevalidate and apply up to 20 one-per-document edits"}, func(ctx context.Context, _ *protocol.CallToolRequest, in batchInput) (*protocol.CallToolResult, docs.BatchResult, error) {
			r, e := svc.BatchEdit(ctx, in.Edits)
			return success(), r, e
		})
	}
	protocol.AddTool(server, &protocol.Tool{Name: "docs_list", Description: "List Markdown documents. path_prefix is optional; limit is 1-100. Pass next_cursor, which is the last returned relative path, to continue."}, func(ctx context.Context, _ *protocol.CallToolRequest, in listInput) (*protocol.CallToolResult, listOutput, error) {
		r, e := svc.List(ctx, in.PathPrefix, in.Cursor, in.Limit)
		return success(), listOutput(r), e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_read", Description: "Read one document. With no section_id or outline, returns the first UTF-8-safe 4096-byte page and next_offset when truncated. Set section_id for a matching query section, outline=true for headings, or offset/limit for a page. adjacent defaults to 0 and is 0-5."}, func(ctx context.Context, _ *protocol.CallToolRequest, in readInput) (*protocol.CallToolResult, documentOutput, error) {
		if in.Outline {
			r, e := svc.Outline(ctx, in.Path)
			return success(), documentOutput{Path: r.Path, Revision: r.Revision, Sections: r.Sections}, e
		}
		if in.SectionID != "" {
			r, e := svc.ReadSection(ctx, in.Path, in.SectionID, in.Adjacent)
			return success(), documentOutput{Path: r.Path, Revision: r.Revision, Chunks: r.Chunks}, e
		}
		includeRevision := true
		if in.Limit == 0 {
			in.Limit = 4096
		}
		if in.IncludeRevision != nil {
			includeRevision = *in.IncludeRevision
		}
		r, e := svc.ReadWithRevision(ctx, in.Path, in.Offset, in.Limit, includeRevision)
		return success(), documentOutput{Path: in.Path, Revision: r.Revision, Content: r.Content, Truncated: r.Truncated, NextOffset: r.NextOffset}, e
	})
	protocol.AddTool(server, &protocol.Tool{Name: "docs_edit", Description: "Edit one document with revision protection. kind=create creates a new path without a revision; kind=update, replace, append, insert_after, section_update, patch, delete, and move require expected_revision."}, func(ctx context.Context, _ *protocol.CallToolRequest, in editInput) (*protocol.CallToolResult, map[string]string, error) {
		var revision string
		var err error
		expected, expectedErr := requiredRevision(in.ExpectedRevision)
		switch in.Kind {
		case "create":
			revision, err = svc.Update(ctx, in.Path, in.Content, nil)
		case "update":
			if expectedErr != nil {
				err = expectedErr
			} else {
				revision, err = svc.Update(ctx, in.Path, in.Content, in.ExpectedRevision)
			}
		case "replace":
			if expectedErr != nil {
				err = expectedErr
			} else {
				revision, err = svc.Replace(ctx, in.Path, in.OldText, in.NewText, expected)
			}
		case "append":
			if expectedErr != nil {
				err = expectedErr
			} else {
				revision, err = svc.Append(ctx, in.Path, in.SectionID, in.Content, expected)
			}
		case "insert_after":
			if expectedErr != nil {
				err = expectedErr
			} else {
				revision, err = svc.InsertAfter(ctx, in.Path, in.Anchor, in.Content, expected)
			}
		case "section_update":
			if expectedErr != nil {
				err = expectedErr
			} else {
				revision, err = svc.UpdateSection(ctx, in.Path, in.SectionID, in.Content, expected)
			}
		case "patch":
			if expectedErr != nil {
				err = expectedErr
			} else {
				revision, err = svc.Patch(ctx, in.Path, in.Patch, expected)
			}
		case "delete":
			if expectedErr != nil {
				err = expectedErr
			} else {
				err = svc.Delete(ctx, in.Path, expected)
			}
			revision = "deleted"
		case "move":
			if expectedErr != nil {
				err = expectedErr
			} else {
				err = svc.Move(ctx, in.Path, in.To, expected)
			}
			revision = "moved"
		default:
			err = fmt.Errorf("invalid edit kind %q: expected create, update, replace, append, insert_after, section_update, patch, delete, or move", in.Kind)
		}
		return success(), map[string]string{"revision": revision}, err
	})
	if false { // retired low-level tools; replaced by docs_read and docs_edit
		protocol.AddTool(server, &protocol.Tool{Name: "docs_append", Description: "Append text to a Markdown section"}, func(ctx context.Context, _ *protocol.CallToolRequest, in appendInput) (*protocol.CallToolResult, map[string]string, error) {
			r, e := svc.Append(ctx, in.Path, in.SectionID, in.Content, in.ExpectedRevision)
			return success(), map[string]string{"revision": r}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_insert_after", Description: "Insert text after one unique anchor"}, func(ctx context.Context, _ *protocol.CallToolRequest, in insertAfterInput) (*protocol.CallToolResult, map[string]string, error) {
			r, e := svc.InsertAfter(ctx, in.Path, in.Anchor, in.Content, in.ExpectedRevision)
			return success(), map[string]string{"revision": r}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_read_section", Description: "Read a Markdown section and adjacent chunks"}, func(ctx context.Context, _ *protocol.CallToolRequest, in readSectionInput) (*protocol.CallToolResult, docs.SectionResult, error) {
			r, e := svc.ReadSection(ctx, in.Path, in.SectionID, in.Adjacent)
			return success(), r, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_index_status", Description: "Get Markdown index status"}, func(context.Context, *protocol.CallToolRequest, struct{}) (*protocol.CallToolResult, docs.IndexStatus, error) {
			return success(), svc.Status(), nil
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_rebuild_index", Description: "Rebuild the Markdown index"}, func(ctx context.Context, _ *protocol.CallToolRequest, _ struct{}) (*protocol.CallToolResult, rebuildOutput, error) {
			generation, e := svc.RebuildAsync(ctx)
			return success(), rebuildOutput{Generation: generation}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_update", Description: "Create or conditionally replace a document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in updateInput) (*protocol.CallToolResult, map[string]string, error) {
			r, e := svc.Update(ctx, in.Path, in.Content, in.ExpectedRevision)
			return success(), map[string]string{"revision": r}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_replace", Description: "Replace one unique text fragment without sending the whole document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in replaceInput) (*protocol.CallToolResult, map[string]string, error) {
			r, e := svc.Replace(ctx, in.Path, in.OldText, in.NewText, in.ExpectedRevision)
			return success(), map[string]string{"revision": r}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_section_update", Description: "Replace one indexed Markdown section"}, func(ctx context.Context, _ *protocol.CallToolRequest, in sectionUpdateInput) (*protocol.CallToolResult, map[string]string, error) {
			r, e := svc.UpdateSection(ctx, in.Path, in.SectionID, in.Content, in.ExpectedRevision)
			return success(), map[string]string{"revision": r}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_patch", Description: "Apply a restricted unified diff"}, func(ctx context.Context, _ *protocol.CallToolRequest, in patchInput) (*protocol.CallToolResult, map[string]string, error) {
			r, e := svc.Patch(ctx, in.Path, in.Patch, in.ExpectedRevision)
			return success(), map[string]string{"revision": r}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_delete", Description: "Conditionally delete a document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in deleteInput) (*protocol.CallToolResult, map[string]string, error) {
			e := svc.Delete(ctx, in.Path, in.ExpectedRevision)
			return success(), map[string]string{"status": "deleted"}, e
		})
		protocol.AddTool(server, &protocol.Tool{Name: "docs_move", Description: "Conditionally move or rename a document"}, func(ctx context.Context, _ *protocol.CallToolRequest, in moveInput) (*protocol.CallToolResult, map[string]string, error) {
			e := svc.Move(ctx, in.From, in.To, in.ExpectedRevision)
			return success(), map[string]string{"status": "moved"}, e
		})
	}
	protocol.AddTool(server, &protocol.Tool{Name: "docs_history", Description: "List document history or retrieve a historical snapshot"}, func(ctx context.Context, _ *protocol.CallToolRequest, in historyInput) (*protocol.CallToolResult, historyOutput, error) {
		r, e := svc.History(in.Path, in.Revision, in.IncludeContent)
		return success(), historyOutput{Items: r}, e
	})
	h := protocol.NewStreamableHTTPHandler(func(*http.Request) *protocol.Server { return server }, &protocol.StreamableHTTPOptions{JSONResponse: true})
	state := &runtimeState{handler: auth(config, h), svc: svc, maintenanceStop: make(chan struct{})}
	state.startMaintenance()
	return state, nil
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
