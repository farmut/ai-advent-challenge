package domain

// ChunkMeta holds metadata attached to every chunk.
type ChunkMeta struct {
	// Source is the stable document id: absolute path for files, page_id for wiki
	// pages. It is the key of Chunk.ID ("<source>#<chunkID>"), so it must not
	// change when a document is renamed.
	Source string
	// File is the base filename for files, the page title for wiki pages.
	File string
	// Title is the human-readable document title ("" when unknown).
	Title string
	// URL is the public link to the document ("" for local files).
	URL string
	// Format is the logical content format — "markdown" | "text" | "pdf".
	// It is NOT a file extension; an empty value means "unknown, infer it".
	Format string
	// Section is the heading breadcrumb of the chunk, e.g. "H1 > H2 > H3"
	// (empty when not applicable).
	Section string
	// ChunkID is the sequential index within the document (0-based).
	ChunkID int
}

// Chunk is a piece of text together with its metadata.
type Chunk struct {
	ID      string // globally unique: "<source>#<chunkID>"
	Meta    ChunkMeta
	Content string
}
