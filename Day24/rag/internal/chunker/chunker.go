package chunker

import (
	"fmt"

	"rag/internal/domain"
)

// Config holds the parameters that control chunking behaviour.
type Config struct {
	// ChunkSize is the maximum chunk size in runes.
	// Used only by the fixed strategy; ignored by structural.
	ChunkSize int
	// Overlap is the number of runes carried from one chunk into the next.
	// Used by both strategies.
	Overlap int
}

// Chunker splits document text into overlapping chunks.
type Chunker interface {
	Chunk(content string, meta domain.ChunkMeta) []domain.Chunk
}

// New returns a Chunker for the given strategy name.
// strategy must be "fixed" or "structural".
func New(strategy string, cfg Config) (Chunker, error) {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 512
	}
	if cfg.Overlap < 0 {
		cfg.Overlap = 0
	}
	if cfg.Overlap >= cfg.ChunkSize {
		cfg.Overlap = cfg.ChunkSize / 4
	}

	switch strategy {
	case "fixed":
		return &fixedChunker{cfg: cfg}, nil
	case "structural":
		return &structuralChunker{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("unknown chunking strategy %q — use fixed or structural", strategy)
	}
}

// splitOverlap is the shared sliding-window primitive used by both chunkers.
// counter is a pointer to the document-global chunk index and is incremented in-place.
func splitOverlap(content string, meta domain.ChunkMeta, cfg Config, counter *int) []domain.Chunk {
	runes := []rune(content)
	if len(runes) == 0 {
		return nil
	}

	step := cfg.ChunkSize - cfg.Overlap
	if step <= 0 {
		step = 1
	}

	var chunks []domain.Chunk
	for start := 0; start < len(runes); start += step {
		end := start + cfg.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		m := meta
		m.ChunkID = *counter

		chunks = append(chunks, domain.Chunk{
			ID:      fmt.Sprintf("%s#%d", meta.Source, *counter),
			Meta:    m,
			Content: string(runes[start:end]),
		})
		*counter++

		if end >= len(runes) {
			break
		}
	}
	return chunks
}
