package domain

// RetrievedChunk is a single piece of indexed knowledge returned by a RAG
// retriever, together with its cosine similarity to the query.
type RetrievedChunk struct {
	ID         string  // globally unique chunk id ("<source>#<chunkID>")
	Source     string  // absolute file path the chunk came from
	File       string  // base filename
	Section    string  // header / section name (may be empty)
	ChunkID    int      // sequential index within the source document
	Content    string  // chunk text
	Similarity float64  // cosine similarity to the query vector (0..1)
}
