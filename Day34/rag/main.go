package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rag/internal/chunker"
	"rag/internal/domain"
	"rag/internal/embedder"
	"rag/internal/reader"
	"rag/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "index":
		err = cmdIndex(os.Args[2:])
	case "search":
		err = cmdSearch(os.Args[2:])
	case "stats":
		err = cmdStats(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage:
  rag index  --input <path> [options]   — index files into SQLite
  rag search --query <text> [options]   — semantic search
  rag stats  --db <path>                — show index statistics

Run 'rag <command> --help' for per-command flags.`)
}

// ---------------------------------------------------------------------------
// index
// ---------------------------------------------------------------------------

func cmdIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	input := fs.String("input", "", "file or directory to index (required)")
	dbPath := fs.String("db", "rag.db", "SQLite database path")
	strategy := fs.String("strategy", "fixed", "chunking strategy: fixed|structural")
	chunkSize := fs.Int("chunk-size", 512, "max chunk size in characters")
	overlap := fs.Int("overlap", 64, "overlap between chunks in characters")
	embedURL := fs.String("embed-url", "http://localhost:11434", "embeddings API base URL")
	embedModel := fs.String("embed-model", "nomic-embed-text", "embeddings model name")
	embedKey := fs.String("embed-key", "", "API key (leave empty for local LLMs)")
	_ = fs.Parse(args)

	if *input == "" {
		return fmt.Errorf("--input is required")
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	emb := embedder.New(*embedURL, *embedModel, *embedKey)

	ch, err := chunker.New(*strategy, chunker.Config{
		ChunkSize: *chunkSize,
		Overlap:   *overlap,
	})
	if err != nil {
		return err
	}

	return indexPath(context.Background(), *input, ch, emb, st)
}

func indexPath(ctx context.Context, path string, ch chunker.Chunker, emb *embedder.Client, st *store.Store) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return indexFile(ctx, path, ch, emb, st)
	}

	return filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".txt" || ext == ".md" || ext == ".markdown" || ext == ".pdf" {
			return indexFile(ctx, p, ch, emb, st)
		}
		return nil
	})
}

func indexFile(ctx context.Context, path string, ch chunker.Chunker, emb *embedder.Client, st *store.Store) error {
	fmt.Printf("indexing %s\n", path)

	content, err := reader.Read(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	meta := domain.ChunkMeta{
		Source: abs,
		File:   filepath.Base(abs),
	}

	chunks := ch.Chunk(content, meta)
	fmt.Printf("  → %d chunks\n", len(chunks))

	for i, c := range chunks {
		vec, err := emb.Embed(ctx, c.Content)
		if err != nil {
			return fmt.Errorf("embed chunk %d: %w", i, err)
		}
		if err := st.SaveChunk(ctx, c, vec); err != nil {
			return fmt.Errorf("save chunk %d: %w", i, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

func cmdSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	query := fs.String("query", "", "search query text (required)")
	dbPath := fs.String("db", "rag.db", "SQLite database path")
	topK := fs.Int("top-k", 5, "number of results to return")
	embedURL := fs.String("embed-url", "http://localhost:11434", "embeddings API base URL")
	embedModel := fs.String("embed-model", "nomic-embed-text", "embeddings model name")
	embedKey := fs.String("embed-key", "", "API key (leave empty for local LLMs)")
	_ = fs.Parse(args)

	if *query == "" {
		return fmt.Errorf("--query is required")
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	emb := embedder.New(*embedURL, *embedModel, *embedKey)

	vec, err := emb.Embed(context.Background(), *query)
	if err != nil {
		return fmt.Errorf("embed query: %w", err)
	}

	results, err := st.Search(context.Background(), vec, *topK)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("no results found")
		return nil
	}

	for i, r := range results {
		fmt.Printf("\n─── Result %d  (similarity %.4f) ───\n", i+1, r.Similarity)
		fmt.Printf("Source:   %s\n", r.Chunk.Meta.Source)
		fmt.Printf("File:     %s\n", r.Chunk.Meta.File)
		if r.Chunk.Meta.Section != "" {
			fmt.Printf("Section:  %s\n", r.Chunk.Meta.Section)
		}
		fmt.Printf("ChunkID:  %d\n", r.Chunk.Meta.ChunkID)
		fmt.Printf("\n%s\n", r.Chunk.Content)
	}
	return nil
}

// ---------------------------------------------------------------------------
// stats
// ---------------------------------------------------------------------------

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	dbPath := fs.String("db", "rag.db", "SQLite database path")
	_ = fs.Parse(args)

	st, err := store.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	total, embedded, err := st.Stats(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("Chunks total:    %d\n", total)
	fmt.Printf("Chunks embedded: %d\n", embedded)
	fmt.Printf("Chunks pending:  %d\n", total-embedded)
	return nil
}
