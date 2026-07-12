package chunker

import (
	"fmt"
	"path/filepath"
	"strings"

	"rag/internal/domain"
)

// structuralChunker splits documents along their structural boundaries.
// Each logical unit becomes exactly one chunk — ChunkSize is not used.
//   - Markdown (.md / .markdown): one chunk per ATX heading section.
//   - All other formats: one chunk per blank-line-separated paragraph.
//
// The last Overlap runes of the previous chunk are prepended to the next
// chunk so context is not lost across boundaries.
type structuralChunker struct{ cfg Config }

func (c *structuralChunker) Chunk(content string, meta domain.ChunkMeta) []domain.Chunk {
	ext := strings.ToLower(filepath.Ext(meta.File))
	if ext == ".md" || ext == ".markdown" {
		return c.chunkMarkdown(content, meta)
	}
	return c.chunkParagraphs(content, meta)
}

// chunkMarkdown creates one chunk per ATX heading section.
func (c *structuralChunker) chunkMarkdown(content string, meta domain.ChunkMeta) []domain.Chunk {
	type section struct {
		header string
		body   strings.Builder
	}

	sections := []section{{header: ""}}

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, "#")
		hashes := len(line) - len(trimmed)
		if hashes > 0 && (len(trimmed) == 0 || trimmed[0] == ' ') {
			sections = append(sections, section{header: strings.TrimSpace(trimmed)})
			continue
		}
		sections[len(sections)-1].body.WriteString(line + "\n")
	}

	var (
		chunks   []domain.Chunk
		counter  int
		prevTail string
	)

	for _, sec := range sections {
		text := strings.TrimSpace(sec.body.String())
		if text == "" {
			continue
		}

		chunkText := text
		if prevTail != "" {
			chunkText = prevTail + "\n\n" + text
		}

		m := meta
		m.Section = sec.header
		m.ChunkID = counter

		chunks = append(chunks, domain.Chunk{
			ID:      fmt.Sprintf("%s#%d", meta.Source, counter),
			Meta:    m,
			Content: chunkText,
		})
		counter++

		prevTail = tailRunes(text, c.cfg.Overlap)
	}
	return chunks
}

// chunkParagraphs creates one chunk per blank-line-separated paragraph.
func (c *structuralChunker) chunkParagraphs(content string, meta domain.ChunkMeta) []domain.Chunk {
	paras := parseParagraphs(content)

	var (
		chunks   []domain.Chunk
		counter  int
		prevTail string
	)

	for _, para := range paras {
		chunkText := para
		if prevTail != "" {
			chunkText = prevTail + "\n\n" + para
		}

		m := meta
		m.ChunkID = counter

		chunks = append(chunks, domain.Chunk{
			ID:      fmt.Sprintf("%s#%d", meta.Source, counter),
			Meta:    m,
			Content: chunkText,
		})
		counter++

		prevTail = tailRunes(para, c.cfg.Overlap)
	}
	return chunks
}

// tailRunes returns the last n runes of s, or all of s when len(s) <= n.
func tailRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// parseParagraphs splits text on one or more blank lines and trims each piece.
func parseParagraphs(content string) []string {
	raw := strings.Split(content, "\n\n")
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
