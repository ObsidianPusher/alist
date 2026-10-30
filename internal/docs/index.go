package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/blevesearch/bleve/v2"
	querypkg "github.com/blevesearch/bleve/v2/search/query"
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
	Path       string    `json:"path"`
	PathSearch string    `json:"path_search"`
	SectionID  string    `json:"section_id"`
	Heading    string    `json:"heading"`
	Ordinal    int       `json:"ordinal"`
	Start      int       `json:"start"`
	End        int       `json:"end"`
	Content    string    `json:"content"`
	Revision   string    `json:"revision"`
	Modified   time.Time `json:"modified"`
}

// BleveType selects the keyword path mapping, including on existing indexes.
func (indexedDoc) BleveType() string { return "doc" }

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
		d.AddFieldMappingsAt("path_search", bleve.NewTextFieldMapping())
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
	return i.UpsertAt(p, c, r, time.Now())
}
func (i *Index) UpsertAt(p, c, r string, modified time.Time) error {
	return i.Replace(p, r, modified, ChunkMarkdown(c, defaultChunkChars))
}
func (i *Index) Replace(p, revision string, modified time.Time, chunks []Chunk) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	ids, err := i.documentIDs(p)
	if err != nil {
		return err
	}
	batch := i.index.NewBatch()
	for _, id := range ids {
		batch.Delete(id)
	}
	for _, chunk := range chunks {
		id := p + "#" + chunk.ID
		batch.Index(id, indexedDoc{Path: p, PathSearch: p, SectionID: chunk.ID, Heading: chunk.Heading, Ordinal: chunk.Ordinal, Start: chunk.Start, End: chunk.End, Content: chunk.Content, Revision: revision, Modified: modified})
	}
	return i.index.Batch(batch)
}
func (i *Index) Delete(p string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	ids, err := i.documentIDs(p)
	if err != nil {
		return err
	}
	batch := i.index.NewBatch()
	for _, id := range ids {
		batch.Delete(id)
	}
	return i.index.Batch(batch)
}

// documentIDs requires i.mu. Legacy paths were analyzed with the default
// analyzer; verify stored paths so token-equivalent neighbors remain untouched.
func (i *Index) documentIDs(p string) ([]string, error) {
	q := bleve.NewTermQuery(p)
	q.SetField("path")
	legacy := bleve.NewMatchPhraseQuery(p)
	legacy.SetField("path")
	legacy.Analyzer = "standard"
	req := bleve.NewSearchRequest(bleve.NewDisjunctionQuery(q, legacy))
	req.Size = 10000
	req.Fields = []string{"path"}
	ids := make([]string, 0)
	for {
		res, err := i.index.Search(req)
		if err != nil {
			return nil, err
		}
		for _, hit := range res.Hits {
			if hit.Fields["path"] == p {
				ids = append(ids, hit.ID)
			}
		}
		if len(res.Hits) < req.Size {
			break
		}
		req.From += len(res.Hits)
	}
	return ids, nil
}
func (i *Index) Search(q, prefix, mode string, limit int) ([]SearchResult, error) {
	if limit < 1 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	var query querypkg.Query
	searchTerms := strings.Fields(strings.ToLower(q))
	literalTerms := make([]string, 0)
	switch mode {
	case "all":
		mapping := i.index.Mapping()
		analyzerName := mapping.AnalyzerNameForPath("content")
		analyzer := mapping.AnalyzerNamed(analyzerName)
		if analyzer == nil {
			return nil, fmt.Errorf("no analyzer named %q registered", analyzerName)
		}
		terms := make([]querypkg.Query, 0)
		searchTerms = nil
		for _, term := range strings.Fields(strings.ToLower(q)) {
			// Stopwords must not turn an otherwise valid conjunction into MatchNone.
			if len(analyzer.Analyze([]byte(term))) == 0 {
				continue
			}
			searchTerms = append(searchTerms, term)
			if strings.ContainsFunc(term, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
				literalTerms = append(literalTerms, term)
			}
			match := bleve.NewMatchPhraseQuery(term)
			match.SetField("content")
			heading := bleve.NewMatchPhraseQuery(term)
			heading.SetField("heading")
			terms = append(terms, bleve.NewDisjunctionQuery(match, heading))
		}
		query = bleve.NewConjunctionQuery(terms...)
	case "exact":
		match := bleve.NewMatchPhraseQuery(q)
		match.SetField("content")
		heading := bleve.NewMatchPhraseQuery(q)
		heading.SetField("heading")
		query = bleve.NewDisjunctionQuery(match, heading)
		if strings.ContainsFunc(q, func(r rune) bool { return unicode.Is(unicode.Han, r) }) {
			literalTerms = append(literalTerms, strings.ToLower(q))
		}
	case "path":
		match := bleve.NewMatchPhraseQuery(strings.Trim(strings.TrimSpace(q), "/"))
		match.SetField("path_search")
		query = match
	default: // any
		match := bleve.NewMatchQuery(q)
		match.SetField("content")
		heading := bleve.NewMatchQuery(q)
		heading.SetField("heading")
		query = bleve.NewDisjunctionQuery(match, heading)
	}
	if prefix != "" {
		prefixQuery := bleve.NewPrefixQuery(prefix)
		prefixQuery.SetField("path")
		conjunction := bleve.NewConjunctionQuery(query, prefixQuery)
		query = conjunction
	}
	req := bleve.NewSearchRequest(query)
	req.Size = limit
	req.Fields = []string{"path", "section_id", "heading", "start", "end", "revision", "modified", "content"}
	out := []SearchResult{}
	seen := map[string]struct{}{}
	for len(out) < limit {
		res, err := i.index.Search(req)
		if err != nil {
			return nil, err
		}
		for _, h := range res.Hits {
			if len(out) >= limit {
				break
			}
			p, _ := h.Fields["path"].(string)
			if _, ok := seen[p]; ok {
				continue
			}
			c, _ := h.Fields["content"].(string)
			heading, _ := h.Fields["heading"].(string)
			lower := strings.ToLower(c)
			lowerHeading := strings.ToLower(heading)
			valid := true
			for _, term := range literalTerms {
				if !strings.Contains(lower, term) && !strings.Contains(lowerHeading, term) {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			seen[p] = struct{}{}
			// Center the preview on a searchable term, skipping ignored stopwords.
			for _, term := range searchTerms {
				if pos := strings.Index(lower, term); pos >= 0 {
					// Convert through runes because case folding may change byte lengths.
					start := len([]rune(lower[:pos])) - 20
					if start > 0 {
						c = string([]rune(c)[start:])
					}
					break
				}
			}
			if len([]rune(c)) > 80 {
				c = string([]rune(c)[:80])
			}
			r, _ := h.Fields["revision"].(string)
			sectionID, _ := h.Fields["section_id"].(string)
			start, _ := h.Fields["start"].(float64)
			end, _ := h.Fields["end"].(float64)
			var modified time.Time
			switch value := h.Fields["modified"].(type) {
			case time.Time:
				modified = value
			case string:
				modified, _ = time.Parse(time.RFC3339Nano, value)
			}
			out = append(out, SearchResult{Path: p, SectionID: sectionID, Heading: heading, Start: int(start), End: int(end), Snippet: c, Score: h.Score, Revision: r, Modified: modified})
		}

		if len(out) >= limit || len(res.Hits) < req.Size {
			break
		}
		req.From += len(res.Hits)
	}
	return out, nil
}
