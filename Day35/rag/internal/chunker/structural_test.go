package chunker

import (
	"strconv"
	"strings"
	"testing"

	"rag/internal/domain"
)

// mdDoc builds a markdown document whose "Deep" section is long enough to force
// a post-split at the given chunk size.
func mdDoc(bodyLen int) string {
	return "# Top\n\nintro text\n\n" +
		"## Middle\n\nmiddle text\n\n" +
		"### Deep\n\n" + strings.Repeat("a", bodyLen) + "\n"
}

func TestStructuralPostSplitsOversizedSection(t *testing.T) {
	c := &structuralChunker{cfg: Config{ChunkSize: 100, Overlap: 10}}

	// 500 runes at a 100-rune window (step 90) is well over three chunks.
	chunks := c.Chunk(mdDoc(500), domain.ChunkMeta{Source: "doc", Format: "markdown"})

	var deep []domain.Chunk
	for _, ch := range chunks {
		if strings.HasSuffix(ch.Meta.Section, "Deep") {
			deep = append(deep, ch)
		}
	}
	if len(deep) < 3 {
		t.Fatalf("oversized section produced %d chunks, want >= 3", len(deep))
	}

	for _, ch := range deep {
		if n := len([]rune(ch.Content)); n > 100 {
			t.Errorf("chunk %s is %d runes, exceeds ChunkSize 100", ch.ID, n)
		}
	}

	// The chunk counter must stay continuous across the whole document.
	for i, ch := range chunks {
		if ch.Meta.ChunkID != i {
			t.Fatalf("chunk %d has ChunkID %d, want %d", i, ch.Meta.ChunkID, i)
		}
		if want := "doc#" + strconv.Itoa(i); ch.ID != want {
			t.Fatalf("chunk %d has ID %q, want %q", i, ch.ID, want)
		}
	}
}

func TestStructuralChunkSizeZeroKeepsOneChunkPerSection(t *testing.T) {
	c := &structuralChunker{cfg: Config{ChunkSize: 0, Overlap: 10}}

	chunks := c.Chunk(mdDoc(500), domain.ChunkMeta{Source: "doc", Format: "markdown"})

	// Three non-empty sections → exactly three chunks, however long they are.
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3 (post-split disabled)", len(chunks))
	}
	if n := len([]rune(chunks[2].Content)); n < 500 {
		t.Errorf("last chunk is %d runes, want the whole 500-rune section", n)
	}
}

func TestStructuralModeFollowsFormatWithoutExtension(t *testing.T) {
	// A wiki page has no filename extension — the format has to drive the mode.
	meta := domain.ChunkMeta{Source: "page-42", File: "Возврат товара", Format: "markdown"}
	c := &structuralChunker{cfg: Config{Overlap: 0}}

	chunks := c.Chunk("# Заголовок\n\nтело\n", meta)

	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if chunks[0].Meta.Section != "Заголовок" {
		t.Fatalf("Section = %q, want %q — markdown mode was not selected",
			chunks[0].Meta.Section, "Заголовок")
	}
}

func TestStructuralModeFallsBackToExtension(t *testing.T) {
	// Empty Format keeps the old behaviour for the file source.
	c := &structuralChunker{cfg: Config{Overlap: 0}}

	md := c.Chunk("# Head\n\nbody\n", domain.ChunkMeta{Source: "s", File: "notes.md"})
	if len(md) != 1 || md[0].Meta.Section != "Head" {
		t.Fatalf("notes.md: got %d chunks, section %q — want markdown mode", len(md), md[0].Meta.Section)
	}

	txt := c.Chunk("# Head\n\nbody\n", domain.ChunkMeta{Source: "s", File: "notes.txt"})
	if len(txt) != 2 {
		t.Fatalf("notes.txt: got %d chunks, want 2 paragraphs", len(txt))
	}
	if txt[0].Meta.Section != "" {
		t.Errorf("paragraph mode set Section = %q, want empty", txt[0].Meta.Section)
	}
}

func TestStructuralSectionIsHeadingBreadcrumb(t *testing.T) {
	doc := "# Доставка\n\nо доставке\n\n" +
		"## Сроки\n\nсроки\n\n" +
		"### Регионы\n\nрегионы\n\n" +
		"## Стоимость\n\nстоимость\n"

	c := &structuralChunker{cfg: Config{Overlap: 0}}
	chunks := c.Chunk(doc, domain.ChunkMeta{Source: "doc", Format: "markdown"})

	want := []string{
		"Доставка",
		"Доставка > Сроки",
		"Доставка > Сроки > Регионы",
		// Back to H2: the H3 must be popped off the breadcrumb.
		"Доставка > Стоимость",
	}
	if len(chunks) != len(want) {
		t.Fatalf("got %d chunks, want %d", len(chunks), len(want))
	}
	for i, w := range want {
		if chunks[i].Meta.Section != w {
			t.Errorf("chunk %d Section = %q, want %q", i, chunks[i].Meta.Section, w)
		}
	}
}

func TestPrefixHeadingOffLeavesContentUntouched(t *testing.T) {
	meta := domain.ChunkMeta{Source: "doc", Title: "Доставка", Format: "markdown"}
	doc := "# Сроки\n\nтело раздела\n"

	off := &structuralChunker{cfg: Config{Overlap: 0}}
	chunks := off.Chunk(doc, meta)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if chunks[0].Content != "тело раздела" {
		t.Fatalf("content = %q, want it unchanged (%q)", chunks[0].Content, "тело раздела")
	}

	on := &structuralChunker{cfg: Config{Overlap: 0, PrefixHeading: true}}
	prefixed := on.Chunk(doc, meta)
	want := "Доставка / Сроки\n\nтело раздела"
	if prefixed[0].Content != want {
		t.Fatalf("content = %q, want %q", prefixed[0].Content, want)
	}
}
