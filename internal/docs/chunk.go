package docs

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const defaultChunkChars = 1200

type Chunk struct {
	ID      string `json:"section_id"`
	Heading string `json:"heading,omitempty"`
	Ordinal int    `json:"ordinal"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Content string `json:"content"`
}

// ChunkMarkdown splits Markdown at headings and paragraph boundaries. Start and
// End are byte offsets into the source, so callers can read the exact excerpt.
func ChunkMarkdown(content string, maxChars int) []Chunk {
	if maxChars <= 0 {
		maxChars = defaultChunkChars
	}
	var chunks []Chunk
	headings := make([]string, 6)
	offset := 0
	section := ""
	var paragraph []string
	paragraphStart := 0
	flush := func() {
		text := strings.TrimSpace(strings.Join(paragraph, "\n"))
		if text == "" {
			paragraph = nil
			return
		}
		start := paragraphStart
		for _, part := range splitChunk(text, maxChars) {
			chunks = append(chunks, Chunk{ID: fmt.Sprintf("section-%d", len(chunks)+1), Heading: section, Ordinal: len(chunks), Start: start, End: start + len(part), Content: part})
			start += len(part)
		}
		paragraph = nil
	}
	for _, line := range strings.SplitAfter(content, "\n") {
		raw := strings.TrimSuffix(line, "\n")
		level := headingLevel(raw)
		if level > 0 {
			flush()
			title := strings.TrimSpace(raw[level:])
			headings[level-1] = title
			for i := level; i < len(headings); i++ {
				headings[i] = ""
			}
			section = strings.Join(nonEmpty(headings), " / ")
		} else if strings.TrimSpace(raw) == "" {
			flush()
		} else {
			if len(paragraph) == 0 {
				paragraphStart = offset
			}
			paragraph = append(paragraph, raw)
		}
		offset += len(line)
	}
	flush()
	return chunks
}

func headingLevel(line string) int {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i == len(line) || line[i] != ' ' {
		return 0
	}
	return i
}
func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
func splitChunk(text string, maxChars int) []string {
	if utf8.RuneCountInString(text) <= maxChars {
		return []string{text}
	}
	runes := []rune(text)
	out := make([]string, 0)
	for len(runes) > maxChars {
		cut := maxChars
		for cut > maxChars/2 && runes[cut] != '\n' && runes[cut] != ' ' {
			cut--
		}
		if cut <= maxChars/2 {
			cut = maxChars
		}
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	if tail := strings.TrimSpace(string(runes)); tail != "" {
		out = append(out, tail)
	}
	return out
}
