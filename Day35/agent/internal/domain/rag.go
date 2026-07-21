package domain

import "strings"

// RetrievedChunk is a single piece of indexed knowledge returned by a RAG
// retriever, together with its cosine similarity to the query.
type RetrievedChunk struct {
	ID      string // globally unique chunk id ("<source>#<chunkID>")
	Source  string // stable source id: absolute file path (file source) | page_id (wiki)
	File    string // base filename | page title
	Section string // header / section name (may be empty)
	// Title is the human-readable document title. Empty for indexes built before
	// the column existed (the agent opens indexes read-only and cannot migrate
	// them), so every consumer must tolerate "".
	Title string
	// URL is the link back to the source page, e.g. the wiki page a chunk was
	// indexed from. Empty for local-file sources and for old-schema indexes —
	// callers must not print an empty link.
	URL         string
	ChunkID     int     // sequential index within the source document
	Content     string  // chunk text
	Similarity  float64 // cosine similarity to the query vector (0..1)
	RerankScore float64 // relevance score assigned by the reranker (0..1); 0 until reranked
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

// AnswerSource identifies one chunk the answer relies on. It is the citation
// surfaced to the user: which document, which section, which chunk.
type AnswerSource struct {
	Source  string  // base filename the chunk came from
	Section string  // header / section name (may be empty)
	URL     string  // link to the source page ("" for local files / old indexes)
	ChunkID int     // sequential index within the source document
	Score   float64 // relevance score (rerank score if reranked, else similarity)
}

// AnswerQuote is a verbatim fragment lifted from a retrieved chunk that supports
// a claim in the answer. It carries the same source coordinates as AnswerSource
// so the reader can trace each quote back to its origin.
type AnswerQuote struct {
	Source  string // base filename the quote came from
	Section string // header / section name (may be empty)
	URL     string // link to the source page ("" for local files / old indexes)
	ChunkID int    // sequential index within the source document
	Text    string // the quoted fragment (verbatim from the chunk)
}

// RAGAnswer is the structured, grounded result of the RAG pipeline: the model's
// answer plus the sources and quotes it stands on. When Grounded is false no
// chunk cleared the relevance threshold, so Answer is an honest "I don't know"
// with a request to clarify, and Sources/Quotes are empty.
type RAGAnswer struct {
	Answer   string
	Sources  []AnswerSource
	Quotes   []AnswerQuote
	Grounded bool
}

// TaskMemory captures the evolving state of a RAG dialogue so the assistant can
// stay coherent across turns. Per the Day25 requirement it records exactly three
// things:
//
//   - Goal        — what the dialogue is ultimately trying to achieve
//   - Clarified   — points the user has already clarified or confirmed
//   - Constraints — fixed constraints, definitions and terms agreed in the dialogue
//
// It is persisted alongside the chat history and injected into every answer so
// the assistant does not re-ask what was settled and keeps to the agreed terms.
type TaskMemory struct {
	Goal        string   `json:"goal"`
	Clarified   []string `json:"clarified"`
	Constraints []string `json:"constraints"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

// IsEmpty reports whether the task memory carries no information yet.
func (m TaskMemory) IsEmpty() bool {
	return strings.TrimSpace(m.Goal) == "" && len(m.Clarified) == 0 && len(m.Constraints) == 0
}
