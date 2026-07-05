//go:build integration

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rag/internal/chunker"
	"rag/internal/domain"
	"rag/internal/embedder"
	"rag/internal/reader"
	"rag/internal/store"
)

const (
	intPDF       = "../postgresql_internals-15.pdf"
	intReport    = "integration_result.txt"
	intChunkSize = 1000
	intOverlap   = 200
	intTopK      = 2
)

var intQueries = []string{
	"Стандарт SQL описывает четыре уровня изоляции транзакций",
	"Как работает механизм MVCC в PostgreSQL",
	"Что такое WAL и зачем он нужен",
}

// queryScore holds top-1 similarity per strategy for one query.
type queryScore struct {
	query      string
	scores     [2]float64 // [fixed, structural]
	chunkIDs   [2]int
}

func TestIntegration(t *testing.T) {
	if _, err := os.Stat(intPDF); err != nil {
		t.Skipf("PDF not found at %s — skipping integration test", intPDF)
	}

	embedURL := os.Getenv("EMBED_URL")
	if embedURL == "" {
		embedURL = "http://127.0.0.1:1234"
	}
	embedModel := os.Getenv("EMBED_MODEL")
	if embedModel == "" {
		embedModel = "nomic-embed-text"
	}
	embedKey := os.Getenv("EMBED_KEY")

	f, err := os.Create(intReport)
	if err != nil {
		t.Fatalf("create report: %v", err)
	}
	defer f.Close()

	log := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		fmt.Fprintln(f, line)
	}
	section := func(text string) {
		t.Log(text)
		fmt.Fprintln(f, text)
	}

	log("==========================================")
	log("  RAG Integration Test")
	log("  Fixed (chunk=%d) vs Structural (overlap=%d)", intChunkSize, intOverlap)
	log("  Document: %s", intPDF)
	log("  Date:     %s", time.Now().Format("2006-01-02 15:04:05"))
	log("==========================================")
	log("")

	// ── Read PDF ──────────────────────────────────────────────────────────────
	t.Log("reading PDF…")
	content, err := reader.Read(intPDF)
	if err != nil {
		t.Fatalf("read PDF: %v", err)
	}

	absPath, _ := filepath.Abs(intPDF)
	meta := domain.ChunkMeta{
		Source: absPath,
		File:   filepath.Base(intPDF),
	}

	emb := embedder.New(embedURL, embedModel, embedKey)
	ctx := context.Background()

	// ── Index with both strategies ─────────────────────────────────────────────
	type index struct {
		label    string
		strategy string
		size     int
		chunks   int
		st       *store.Store
	}

	indexes := []index{
		{fmt.Sprintf("fixed (chunk=%d, overlap=%d)", intChunkSize, intOverlap), "fixed", intChunkSize, 0, nil},
		{fmt.Sprintf("structural (overlap=%d)", intOverlap), "structural", 0, 0, nil},
	}

	for i := range indexes {
		idx := &indexes[i]
		log("--- [%d/2] Indexing: %s ---", i+1, idx.label)

		dbPath := filepath.Join(t.TempDir(), "rag.db")
		st, err := store.Open(dbPath)
		if err != nil {
			t.Fatalf("open store: %v", err)
		}
		t.Cleanup(func() { st.Close() })

		ch, err := chunker.New(idx.strategy, chunker.Config{
			ChunkSize: idx.size,
			Overlap:   intOverlap,
		})
		if err != nil {
			t.Fatalf("new chunker: %v", err)
		}

		chunks := ch.Chunk(content, meta)
		idx.chunks = len(chunks)
		log("  → %d chunks", idx.chunks)

		for _, chunk := range chunks {
			vec, err := emb.Embed(ctx, chunk.Content)
			if err != nil {
				t.Fatalf("embed chunk %s: %v", chunk.ID, err)
			}
			if err := st.SaveChunk(ctx, chunk, vec); err != nil {
				t.Fatalf("save chunk: %v", err)
			}
		}

		idx.st = st
	}

	// ── Stats ─────────────────────────────────────────────────────────────────
	log("")
	for _, idx := range indexes {
		total, embedded, err := idx.st.Stats(ctx)
		if err != nil {
			t.Errorf("stats %s: %v", idx.label, err)
			continue
		}
		log("--- Stats: %s ---", idx.label)
		log("Chunks total:    %d", total)
		log("Chunks embedded: %d", embedded)
		log("Chunks pending:  %d", total-embedded)
	}
	log("")

	// ── Queries ───────────────────────────────────────────────────────────────
	scores := make([]queryScore, len(intQueries))

	for qi, q := range intQueries {
		scores[qi].query = q
		log("=== Query: %s ===", q)

		queryVec, err := emb.Embed(ctx, q)
		if err != nil {
			t.Errorf("embed query %q: %v", q, err)
			continue
		}

		for si, idx := range indexes {
			log("--- %s ---", idx.label)
			results, err := idx.st.Search(ctx, queryVec, intTopK)
			if err != nil {
				t.Errorf("search: %v", err)
				continue
			}
			if len(results) == 0 {
				log("no results found")
				continue
			}

			scores[qi].scores[si] = results[0].Similarity
			scores[qi].chunkIDs[si] = results[0].Chunk.Meta.ChunkID

			for i, r := range results {
				section(fmt.Sprintf("\n─── Result %d  (similarity %.4f) ───", i+1, r.Similarity))
				section(fmt.Sprintf("Source:   %s", r.Chunk.Meta.Source))
				section(fmt.Sprintf("File:     %s", r.Chunk.Meta.File))
				if r.Chunk.Meta.Section != "" {
					section(fmt.Sprintf("Section:  %s", r.Chunk.Meta.Section))
				}
				section(fmt.Sprintf("ChunkID:  %d", r.Chunk.Meta.ChunkID))
				section("")
				section(truncate(r.Chunk.Content, 400))
			}
		}
		log("")
	}

	// ── Summary table ─────────────────────────────────────────────────────────
	log("==========================================")
	log("  ИТОГИ: сравнение стратегий chunking")
	log("==========================================")
	log("")

	const colW = 46
	header := fmt.Sprintf("%-*s  %-8s  %-8s  %s", colW, "Запрос", "Fixed", "Struct", "Победитель")
	log(header)
	log(strings.Repeat("-", len(header)))

	var fixedWins, structWins int
	var fixedSum, structSum float64

	for _, sc := range scores {
		f0, s0 := sc.scores[0], sc.scores[1]
		fixedSum += f0
		structSum += s0

		winner := "="
		switch {
		case f0 > s0+0.005:
			winner = "fixed ✓"
			fixedWins++
		case s0 > f0+0.005:
			winner = "struct ✓"
			structWins++
		default:
			winner = "tie"
		}

		q := sc.query
		if len([]rune(q)) > colW {
			q = string([]rune(q)[:colW-1]) + "…"
		}
		log("%-*s  %.4f    %.4f    %s", colW, q, f0, s0, winner)
	}

	n := float64(len(scores))
	log(strings.Repeat("-", len(header)))
	log("%-*s  %.4f    %.4f", colW, "Среднее (top-1 similarity)", fixedSum/n, structSum/n)
	log("")

	// Chunk count comparison
	log("Количество чанков:")
	log("  fixed:      %d", indexes[0].chunks)
	log("  structural: %d", indexes[1].chunks)
	log("  Компактность structural: %.1fx меньше", float64(indexes[0].chunks)/float64(indexes[1].chunks))
	log("")

	// Verdict
	log("Вывод:")
	switch {
	case fixedWins > structWins:
		log("  Fixed chunking точнее по %d из %d запросов.", fixedWins, len(scores))
		log("  Крупный чанк (%d симв.) захватывает достаточно контекста,", intChunkSize)
		log("  чтобы эмбеддинг покрывал тему целиком.")
		log("  Structural создаёт более компактный индекс, но фрагментирует")
		log("  параграфы по заголовкам, которых в PDF нет — итого хуже.")
	case structWins > fixedWins:
		log("  Structural chunking точнее по %d из %d запросов.", structWins, len(scores))
		log("  Семантически цельные секции дают более точные эмбеддинги.")
		log("  Fixed режет текст вслепую и размывает контекст.")
	default:
		log("  Стратегии показали сопоставимое качество (%d/%d побед).", fixedWins, structWins)
		log("  Structural предпочтительнее: индекс в %.1fx компактнее.",
			float64(indexes[0].chunks)/float64(indexes[1].chunks))
	}
	log("")
	log("==========================================")
	log("  Report: %s", intReport)
	log("==========================================")

	if err := f.Sync(); err != nil {
		t.Errorf("sync report: %v", err)
	}
}

// truncate cuts s to at most n runes, appending "…" if trimmed.
func truncate(s string, n int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return string(runes[:n]) + "…"
}
