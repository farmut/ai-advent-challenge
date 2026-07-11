package chunker

import "rag/internal/domain"

// fixedChunker splits text using a fixed-size sliding window with overlap.
// The window advances by (ChunkSize - Overlap) runes on every step.
type fixedChunker struct{ cfg Config }

func (c *fixedChunker) Chunk(content string, meta domain.ChunkMeta) []domain.Chunk {
	counter := 0
	return splitOverlap(content, meta, c.cfg, &counter)
}
