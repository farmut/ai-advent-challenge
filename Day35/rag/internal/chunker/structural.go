package chunker

import (
	"fmt"
	"path/filepath"
	"strings"

	"rag/internal/domain"
)

// structuralChunker splits documents along their structural boundaries:
//   - Markdown: one chunk per ATX heading section, tagged with the heading
//     breadcrumb ("H1 > H2 > H3").
//   - All other formats: one chunk per blank-line-separated paragraph.
//
// A structural unit longer than cfg.ChunkSize is post-split with the shared
// sliding window (splitOverlap) so a single huge section cannot produce one
// oversized chunk. cfg.ChunkSize <= 0 disables the post-split.
//
// The last Overlap runes of the previous unit are prepended to the next chunk
// so context is not lost across boundaries.
type structuralChunker struct{ cfg Config }

func (c *structuralChunker) Chunk(content string, meta domain.ChunkMeta) []domain.Chunk {
	if isMarkdown(meta) {
		return c.chunkMarkdown(content, meta)
	}
	return c.chunkParagraphs(content, meta)
}

// isMarkdown picks the chunking mode from the logical format. Sources without a
// filename (wiki pages) carry Format; an empty Format falls back to the file
// extension so the file source keeps its previous behaviour.
func isMarkdown(meta domain.ChunkMeta) bool {
	switch strings.ToLower(strings.TrimSpace(meta.Format)) {
	case "markdown", "md":
		return true
	case "":
		ext := strings.ToLower(filepath.Ext(meta.File))
		return ext == ".md" || ext == ".markdown"
	default:
		return false
	}
}

// chunkMarkdown creates one chunk per ATX heading section, post-splitting
// sections longer than cfg.ChunkSize.
func (c *structuralChunker) chunkMarkdown(content string, meta domain.ChunkMeta) []domain.Chunk {
	type section struct {
		section string // heading breadcrumb, e.g. "Intro > Setup"
		body    strings.Builder
	}

	sections := []*section{{}}
	var stack []string // one entry per heading level currently open

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimLeft(line, "#")
		hashes := len(line) - len(trimmed)
		if hashes > 0 && (len(trimmed) == 0 || trimmed[0] == ' ') {
			title := strings.TrimSpace(trimmed)
			// Close every heading at this level or deeper, then open this one.
			if hashes-1 < len(stack) {
				stack = stack[:hashes-1]
			}
			stack = append(stack, title)
			sections = append(sections, &section{section: strings.Join(stack, " > ")})
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

		m := meta
		m.Section = sec.section

		chunks = append(chunks, c.emit(text, prevTail, m, &counter)...)
		prevTail = tailRunes(text, c.cfg.Overlap)
	}
	return chunks
}

// chunkParagraphs creates one chunk per blank-line-separated paragraph,
// post-splitting paragraphs longer than cfg.ChunkSize.
func (c *structuralChunker) chunkParagraphs(content string, meta domain.ChunkMeta) []domain.Chunk {
	var (
		chunks   []domain.Chunk
		counter  int
		prevTail string
	)

	for _, para := range parseParagraphs(content) {
		chunks = append(chunks, c.emit(para, prevTail, meta, &counter)...)
		prevTail = tailRunes(para, c.cfg.Overlap)
	}
	return chunks
}

// emit turns one structural unit into chunks: a single chunk when it fits into
// cfg.ChunkSize (or when the post-split is disabled), otherwise a sliding-window
// split. counter is the document-global chunk index and advances in place.
func (c *structuralChunker) emit(text, prevTail string, m domain.ChunkMeta, counter *int) []domain.Chunk {
	body := text
	if prevTail != "" {
		body = prevTail + "\n\n" + text
	}

	if c.cfg.ChunkSize > 0 && len([]rune(body)) > c.cfg.ChunkSize {
		parts := splitOverlap(body, m, c.cfg, counter)
		for i := range parts {
			parts[i].Content = c.withHeading(parts[i].Content, m)
		}
		return parts
	}

	m.ChunkID = *counter
	chunk := domain.Chunk{
		ID:      fmt.Sprintf("%s#%d", m.Source, *counter),
		Meta:    m,
		Content: c.withHeading(body, m),
	}
	*counter++
	return []domain.Chunk{chunk}
}

// withHeading prepends "<Title> / <Section>" to the chunk text when
// PrefixHeading is enabled. Disabled (the default) it returns text unchanged,
// so existing indexes stay byte-identical.
func (c *structuralChunker) withHeading(text string, m domain.ChunkMeta) string {
	if !c.cfg.PrefixHeading {
		return text
	}
	parts := make([]string, 0, 2)
	if m.Title != "" {
		parts = append(parts, m.Title)
	}
	if m.Section != "" {
		parts = append(parts, m.Section)
	}
	if len(parts) == 0 {
		return text
	}
	return strings.Join(parts, " / ") + "\n\n" + text
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
