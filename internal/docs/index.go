package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
)

type SearchResult struct {
	Path      string    `json:"path"`
	SectionID string    `json:"section_id"`
	Heading   string    `json:"heading,omitempty"`
	Start     int       `json:"start"`
	End       int       `json:"end"`
	Snippet   string    `json:"snippet"`
	Score     float64   `json:"score"`
	Revision  string    `json:"revision"`
	Modified  time.Time `json:"modified"`
}
type Index struct {
	mu    sync.Mutex
	index bleve.Index
}
type indexedDoc struct {
	Path      string    `json:"path"`
	SectionID string    `json:"section_id"`
	Heading   string    `json:"heading"`
	Ordinal   int       `json:"ordinal"`
	Start     int       `json:"start"`
	End       int       `json:"end"`
	Content   string    `json:"content"`
	Revision  string    `json:"revision"`
	Modified  time.Time `json:"modified"`
}

func OpenIndex(dir string) (*Index, error) {
	if dir == "" {
		return nil, fmt.Errorf("mcp index_dir is required")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return nil, err
	}
	idx, err := bleve.Open(dir)
	if err != nil {
		if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) == 0 {
			err = bleve.ErrorIndexPathDoesNotExist
		}
	}
	if err == bleve.ErrorIndexPathDoesNotExist {
		m := bleve.NewIndexMapping()
		d := bleve.NewDocumentMapping()
		d.AddFieldMappingsAt("path", bleve.NewKeywordFieldMapping())
		d.AddFieldMappingsAt("section_id", bleve.NewKeywordFieldMapping())
		d.AddFieldMappingsAt("content", bleve.NewTextFieldMapping())
		m.AddDocumentMapping("doc", d)
		idx, err = bleve.New(dir, m)
	}
	if err != nil {
		return nil, err
	}
	return &Index{index: idx}, nil
}
func (i *Index) Close() error { i.mu.Lock(); defer i.mu.Unlock(); return i.index.Close() }
func (i *Index) Upsert(p, c, r string) error {
	return i.Replace(p, r, time.Now(), ChunkMarkdown(c, defaultChunkChars))
}
func (i *Index) Replace(p, revision string, modified time.Time, chunks []Chunk) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	q := bleve.NewTermQuery(p)
	q.SetField("path")
	res, err := i.index.Search(bleve.NewSearchRequest(q))
	if err != nil {
		return err
	}
	batch := i.index.NewBatch()
	for _, hit := range res.Hits {
		batch.Delete(hit.ID)
	}
	for _, chunk := range chunks {
		id := p + "#" + chunk.ID
		batch.Index(id, indexedDoc{Path: p, SectionID: chunk.ID, Heading: chunk.Heading, Ordinal: chunk.Ordinal, Start: chunk.Start, End: chunk.End, Content: chunk.Content, Revision: revision, Modified: modified})
	}
	return i.index.Batch(batch)
}
func (i *Index) Delete(p string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	q := bleve.NewTermQuery(p)
	q.SetField("path")
	res, err := i.index.Search(bleve.NewSearchRequest(q))
	if err != nil {
		return err
	}
	batch := i.index.NewBatch()
	for _, hit := range res.Hits {
		batch.Delete(hit.ID)
	}
	return i.index.Batch(batch)
}
func (i *Index) Search(q, prefix string, limit int) ([]SearchResult, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 20 {
		limit = 20
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	query := bleve.NewMatchQuery(q)
	query.SetField("content")
	req := bleve.NewSearchRequest(query)
	req.Size = limit
	req.Fields = []string{"path", "section_id", "heading", "start", "end", "revision", "modified", "content"}
	res, err := i.index.Search(req)
	if err != nil {
		return nil, err
	}
	out := []SearchResult{}
	for _, h := range res.Hits {
		p, _ := h.Fields["path"].(string)
		if prefix != "" && !strings.HasPrefix(p, prefix) {
			continue
		}
		c, _ := h.Fields["content"].(string)
		if len(c) > 240 {
			c = c[:240]
		}
		r, _ := h.Fields["revision"].(string)
		sectionID, _ := h.Fields["section_id"].(string)
		heading, _ := h.Fields["heading"].(string)
		start, _ := h.Fields["start"].(float64)
		end, _ := h.Fields["end"].(float64)
		out = append(out, SearchResult{Path: p, SectionID: sectionID, Heading: heading, Start: int(start), End: int(end), Snippet: c, Score: h.Score, Revision: r})
	}
	return out, nil
}
