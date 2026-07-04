package domain

// RetrievedChunk is a single piece of indexed knowledge returned by a RAG
// retriever, together with its cosine similarity to the query.
type RetrievedChunk struct {
	ID         string  // globally unique chunk id ("<source>#<chunkID>")
	Source     string  // absolute file path the chunk came from
	File       string  // base filename
	Section    string  // header / section name (may be empty)
	ChunkID     int      // sequential index within the source document
	Content     string   // chunk text
	Similarity  float64  // cosine similarity to the query vector (0..1)
	RerankScore float64  // relevance score assigned by the reranker (0..1); 0 until reranked
}

// Score returns the score used for ranking/filtering: the reranker score when
// the chunk has been reranked, otherwise the retrieval similarity. Callers that
// enable the rerank stage compare against RerankScore; plain retrieval falls
// back to Similarity so thresholds keep working without a reranker.
func (c RetrievedChunk) Score(reranked bool) float64 {
	if reranked {
		return c.RerankScore
	}
	return c.Similarity
}
