package docs

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/db"
	"github.com/alist-org/alist/v3/internal/errs"
	"github.com/alist-org/alist/v3/internal/fs"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/pkg/http_range"
)

var ErrConflict = errors.New("document revision conflict")

type ConflictError struct{ ActualRevision string }

func (e *ConflictError) Error() string { return ErrConflict.Error() }

type ReadResult struct {
	Content   string `json:"content"`
	Revision  string `json:"revision"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}
type HistoryResult struct {
	Revision string `json:"revision"`
	Action   string `json:"action"`
	Content  string `json:"content,omitempty"`
}
type DocumentInfo struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}
type ListResult struct {
	Items      []DocumentInfo `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type Service struct {
	config conf.MCPConfig
	index  *Index
}

func New(config conf.MCPConfig) (*Service, error) {
	if config.DocsRoot == "" {
		return nil, fmt.Errorf("mcp docs_root is required")
	}
	idx, err := OpenIndex(config.IndexDir)
	if err != nil {
		return nil, err
	}
	return &Service{config: config, index: idx}, nil
}
func (s *Service) Close() error {
	if s.index == nil {
		return nil
	}
	return s.index.Close()
}
func revision(b []byte) string                       { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func (s *Service) fullPath(p string) (string, error) { return ResolvePath(s.config.DocsRoot, p) }

func (s *Service) readBytes(ctx context.Context, full string, offset, limit int64) ([]byte, int64, bool, error) {
	obj, err := fs.Get(ctx, full, &fs.GetArgs{NoLog: true})
	if err != nil {
		return nil, 0, false, err
	}
	if obj.IsDir() {
		return nil, 0, false, fmt.Errorf("document path is a directory")
	}
	if offset < 0 || limit < 0 {
		return nil, 0, false, fmt.Errorf("invalid offset or limit")
	}
	if limit == 0 || limit > s.config.MaxReadBytes {
		limit = s.config.MaxReadBytes
	}
	link, _, err := fs.Link(ctx, full, model.LinkArgs{})
	if err != nil {
		return nil, 0, false, err
	}
	var r io.Reader
	var closeFn func() error
	if link.MFile != nil {
		if _, err = link.MFile.Seek(offset, io.SeekStart); err == nil {
			r = link.MFile
			closeFn = link.MFile.Close
		}
	} else {
		rrc := link.RangeReadCloser
		if rrc == nil {
			rrc, err = stream.GetRangeReadCloserFromLink(obj.GetSize(), link)
		}
		if err == nil {
			var rc io.ReadCloser
			rc, err = rrc.RangeRead(ctx, http_range.Range{Start: offset, Length: limit + 1})
			if err == nil {
				r = rc
				closeFn = func() error { _ = rc.Close(); return rrc.Close() }
			}
		}
	}
	if err != nil {
		return nil, 0, false, err
	}
	if closeFn != nil {
		defer closeFn()
	}
	if r == nil {
		return nil, 0, false, fmt.Errorf("storage does not provide readable link")
	}
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, 0, false, err
	}
	truncated := int64(len(b)) > limit
	if truncated {
		b = b[:limit]
	}
	return b, obj.GetSize(), truncated, nil
}

func (s *Service) Read(ctx context.Context, p string, offset, limit int64) (ReadResult, error) {
	full, err := s.fullPath(p)
	if err != nil {
		return ReadResult{}, err
	}
	b, size, truncated, err := s.readBytes(ctx, full, offset, limit)
	if err != nil {
		return ReadResult{}, err
	}
	if !utf8.Valid(b) {
		return ReadResult{}, fmt.Errorf("document is not valid UTF-8")
	}
	all, _, _, err := s.readBytes(ctx, full, 0, size)
	if err != nil {
		return ReadResult{}, err
	}
	return ReadResult{Content: string(b), Revision: revision(all), Size: size, Truncated: truncated}, nil
}

func (s *Service) Update(ctx context.Context, p, content string, expected *string) (string, error) {
	full, err := s.fullPath(p)
	if err != nil {
		return "", err
	}
	if int64(len(content)) > s.config.MaxWriteBytes || !utf8.ValidString(content) {
		return "", fmt.Errorf("invalid document content")
	}
	var before []byte
	old, err := s.Read(ctx, p, 0, 0)
	if err == nil {
		before = []byte(old.Content)
		if expected == nil || *expected != old.Revision {
			return "", &ConflictError{ActualRevision: old.Revision}
		}
	} else if !errs.IsObjectNotFound(err) {
		return "", err
	} else if expected != nil {
		return "", &ConflictError{}
	}
	file := &stream.FileStream{Obj: &model.Object{Name: path.Base(full), Size: int64(len(content))}, Reader: strings.NewReader(content), Mimetype: "text/plain"}
	if err := fs.PutDirectly(ctx, path.Dir(full), file); err != nil {
		return "", err
	}
	next := revision([]byte(content))
	s.record(p, next, "update", before)
	if err := s.index.Upsert(p, content, next); err != nil {
		return next, err
	}
	return next, nil
}
func (s *Service) Delete(ctx context.Context, p string, expected string) error {
	old, err := s.Read(ctx, p, 0, 0)
	if err != nil {
		return err
	}
	if old.Revision != expected {
		return &ConflictError{ActualRevision: old.Revision}
	}
	full, _ := s.fullPath(p)
	if err = fs.Remove(ctx, full); err != nil {
		return err
	}
	s.record(p, old.Revision, "delete", []byte(old.Content))
	return s.index.Delete(p)
}
func (s *Service) Move(ctx context.Context, from, to, expected string) error {
	old, err := s.Read(ctx, from, 0, 0)
	if err != nil {
		return err
	}
	if old.Revision != expected {
		return &ConflictError{ActualRevision: old.Revision}
	}
	src, _ := s.fullPath(from)
	dst, err := s.fullPath(to)
	if err != nil {
		return err
	}
	if err = fs.Move(ctx, src, path.Dir(dst)); err != nil {
		return err
	}
	if path.Base(src) != path.Base(dst) {
		if err = fs.Rename(ctx, path.Join(path.Dir(dst), path.Base(src)), path.Base(dst)); err != nil {
			return err
		}
	}
	_ = s.index.Delete(from)
	return s.index.Upsert(to, old.Content, old.Revision)
}
func (s *Service) record(p, rev, action string, before []byte) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write(before)
	_ = w.Close()
	_ = db.CreateDocsHistory(&model.DocsHistory{Path: p, Revision: rev, Action: action, Snapshot: b.Bytes()}, s.config.HistoryLimit)
}
func (s *Service) History(p, rev string) ([]HistoryResult, error) {
	items, err := db.ListDocsHistory(p)
	if err != nil {
		return nil, err
	}
	out := make([]HistoryResult, 0, len(items))
	for _, it := range items {
		if rev != "" && it.Revision != rev {
			continue
		}
		r := HistoryResult{Revision: it.Revision, Action: it.Action}
		if rev != "" {
			z, e := gzip.NewReader(bytes.NewReader(it.Snapshot))
			if e != nil {
				return nil, e
			}
			b, e := io.ReadAll(z)
			_ = z.Close()
			if e != nil {
				return nil, e
			}
			r.Content = string(b)
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) Search(query, prefix string, limit int) ([]SearchResult, error) {
	return s.index.Search(query, prefix, limit)
}

func (s *Service) List(ctx context.Context, prefix, cursor string, limit int) (ListResult, error) {
	if limit < 1 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}
	prefix = strings.Trim(path.Clean("/"+prefix), "/")
	if strings.HasPrefix(cursor, "/") || strings.Contains(cursor, "../") {
		return ListResult{}, fmt.Errorf("invalid document cursor")
	}
	items := make([]DocumentInfo, 0)
	root := strings.TrimSuffix(s.config.DocsRoot, "/")
	var walk func(string) error
	walk = func(full string) error {
		objs, err := fs.List(ctx, full, &fs.ListArgs{NoLog: true})
		if err != nil {
			return err
		}
		for _, obj := range objs {
			child := path.Join(full, obj.GetName())
			if obj.IsDir() {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			rel := strings.TrimPrefix(child, root+"/")
			if _, err := s.fullPath(rel); err != nil {
				continue
			}
			if prefix != "" && rel != prefix && !strings.HasPrefix(rel, prefix+"/") {
				continue
			}
			items = append(items, DocumentInfo{Path: rel, Size: obj.GetSize(), Modified: obj.ModTime()})
		}
		return nil
	}
	if err := walk(s.config.DocsRoot); err != nil {
		return ListResult{}, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	start := 0
	for start < len(items) && items[start].Path <= cursor {
		start++
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	result := ListResult{Items: items[start:end]}
	if end < len(items) {
		result.NextCursor = items[end-1].Path
	}
	return result, nil
}

// Rebuild indexes every eligible document below the configured root. Files larger
// than the configured read limit are intentionally skipped: they cannot be read
// through docs_read either, so indexing them would create inconsistent behavior.
func (s *Service) Rebuild(ctx context.Context) error {
	var walk func(string) error
	walk = func(full string) error {
		objs, err := fs.List(ctx, full, &fs.ListArgs{NoLog: true})
		if err != nil {
			return err
		}
		for _, obj := range objs {
			child := path.Join(full, obj.GetName())
			if obj.IsDir() {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			rel := strings.TrimPrefix(child, strings.TrimSuffix(s.config.DocsRoot, "/")+"/")
			if _, err := s.fullPath(rel); err != nil || obj.GetSize() > s.config.MaxReadBytes {
				continue
			}
			read, err := s.Read(ctx, rel, 0, obj.GetSize())
			if err != nil {
				continue
			}
			if err := s.index.Upsert(rel, read.Content, read.Revision); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(s.config.DocsRoot)
}
