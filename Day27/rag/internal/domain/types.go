package domain

// ChunkMeta holds metadata attached to every chunk.
type ChunkMeta struct {
	Source  string // absolute file path
	File    string // base filename
	Section string // header / section name (empty when not applicable)
	ChunkID int    // sequential index within the document (0-based)
}

// Chunk is a piece of text together with its metadata.
type Chunk struct {
	ID      string // globally unique: "<source>#<chunkID>"
	Meta    ChunkMeta
	Content string
}
