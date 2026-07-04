.PHONY: build test vet clean help \
        run-index run-index-fixed run-index-structural \
        run-search run-stats \
        run-demo run-demo-fixed run-demo-structural run-demo-clean \
        run-demo-docs \
        run-integration run-integration-clean

BINARY_NAME := rag
GO          := go
LDFLAGS     := -s -w
SOURCES     := $(shell find . -name "*.go" -not -name "*_test.go")

# ---------------------------------------------------------------------------
# Embeddings settings
# Local Ollama (default): pull nomic-embed-text with  ollama pull nomic-embed-text
# LM Studio:              set EMBED_URL=http://localhost:1234
# Remote OpenAI-compat:   set EMBED_URL=https://api.openai.com  EMBED_KEY=sk-...
#                         set EMBED_MODEL=text-embedding-3-small
# ---------------------------------------------------------------------------

EMBED_URL   ?= http://localhost:11434
EMBED_MODEL ?= nomic-embed-text
EMBED_KEY   ?=

EMBED_KEY_FLAG = $(if $(EMBED_KEY),--embed-key "$(EMBED_KEY)",)

# ---------------------------------------------------------------------------
# Index / search settings
# ---------------------------------------------------------------------------

# File or directory to index (required for run-index).
INPUT      ?=
# SQLite database file.
DB         ?= rag.db
# Chunking strategy: fixed | structural
STRATEGY   ?= fixed
# Max chunk size in characters.
CHUNK_SIZE ?= 512
# Overlap between consecutive chunks in characters.
OVERLAP    ?= 64
# Search query text (required for run-search).
QUERY      ?=
# Number of top results to return.
TOP_K      ?= 5

# ---------------------------------------------------------------------------
# Demo settings (run-demo-*)
# Creates self-contained sample docs and a throw-away DB in /tmp.
# ---------------------------------------------------------------------------

DEMO_DIR ?= /tmp/rag_demo_docs
DEMO_DB  ?= /tmp/rag_demo.db

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

$(BINARY_NAME): $(SOURCES) ## Build the RAG binary (implicit target)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

build: $(BINARY_NAME) ## Build the RAG binary

test: ## Run unit tests (no embeddings API required)
	$(GO) test -v ./...

vet: ## Static analysis
	$(GO) vet ./...

clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)

# ---------------------------------------------------------------------------
# Index
# ---------------------------------------------------------------------------

run-index: build ## Index INPUT into DB using STRATEGY (INPUT=<path> DB=rag.db STRATEGY=fixed CHUNK_SIZE=512 OVERLAP=64)
	@if [ -z "$(INPUT)" ]; then \
		echo "Error: INPUT is not set"; \
		echo "  make run-index INPUT=./docs DB=rag.db"; \
		exit 1; \
	fi
	@echo "=========================================="
	@echo "  RAG Indexer"
	@echo "  Input:       $(INPUT)"
	@echo "  Database:    $(DB)"
	@echo "  Strategy:    $(STRATEGY)"
	@echo "  Chunk size:  $(CHUNK_SIZE)"
	@echo "  Overlap:     $(OVERLAP)"
	@echo "  Embed URL:   $(EMBED_URL)"
	@echo "  Embed model: $(EMBED_MODEL)"
	@echo "=========================================="
	./$(BINARY_NAME) index \
		--input       "$(INPUT)" \
		--db          "$(DB)" \
		--strategy    "$(STRATEGY)" \
		--chunk-size  $(CHUNK_SIZE) \
		--overlap     $(OVERLAP) \
		--embed-url   "$(EMBED_URL)" \
		--embed-model "$(EMBED_MODEL)" \
		$(EMBED_KEY_FLAG)
	@echo ""
	@echo "  Indexing complete. Run: make run-stats DB=$(DB)"
	@echo "=========================================="

run-index-fixed: build ## Index INPUT with fixed-size sliding-window chunking (INPUT=, DB=rag.db, CHUNK_SIZE=512, OVERLAP=64)
	@$(MAKE) --no-print-directory run-index STRATEGY=fixed

run-index-structural: build ## Index INPUT with structural chunking — headings for Markdown, paragraphs for txt/pdf (INPUT=, DB=rag.db, OVERLAP=64)
	@$(MAKE) --no-print-directory run-index STRATEGY=structural CHUNK_SIZE=0

# ---------------------------------------------------------------------------
# Search
# ---------------------------------------------------------------------------

run-search: build ## Semantic search over DB (QUERY=<text> DB=rag.db TOP_K=5)
	@if [ -z "$(QUERY)" ]; then \
		echo "Error: QUERY is not set"; \
		echo "  make run-search QUERY=\"your question here\""; \
		exit 1; \
	fi
	@echo "=========================================="
	@echo "  RAG Search"
	@echo "  Query:       $(QUERY)"
	@echo "  Database:    $(DB)"
	@echo "  Top-K:       $(TOP_K)"
	@echo "  Embed URL:   $(EMBED_URL)"
	@echo "  Embed model: $(EMBED_MODEL)"
	@echo "=========================================="
	./$(BINARY_NAME) search \
		--query       "$(QUERY)" \
		--db          "$(DB)" \
		--top-k       $(TOP_K) \
		--embed-url   "$(EMBED_URL)" \
		--embed-model "$(EMBED_MODEL)" \
		$(EMBED_KEY_FLAG)

# ---------------------------------------------------------------------------
# Stats
# ---------------------------------------------------------------------------

run-stats: build ## Show chunk / embedding counts for DB (DB=rag.db)
	@echo "=== RAG Index Stats: $(DB) ==="
	./$(BINARY_NAME) stats --db "$(DB)"

# ---------------------------------------------------------------------------
# Demo — requires Ollama running with the embed model pulled:
#   ollama pull nomic-embed-text
# ---------------------------------------------------------------------------

run-demo-docs: ## Create sample Markdown, txt and PDF-stub documents in DEMO_DIR
	@echo "--- Creating sample documents in $(DEMO_DIR) ---"
	@mkdir -p "$(DEMO_DIR)"
	@printf '# Introduction to RAG\n\nRetrieval-Augmented Generation (RAG) is an AI framework\nthat combines document retrieval with language model generation.\nInstead of relying solely on the model'"'"'s training data, RAG\nfetches relevant passages from an external knowledge base at\ninference time and feeds them into the prompt.\n\n## How It Works\n\nThe RAG pipeline has three main stages:\n\n1. **Indexing** — Documents are split into chunks, each chunk\n   is converted into a dense vector (embedding) and stored in\n   a vector index together with the original text.\n\n2. **Retrieval** — At query time the question is embedded with\n   the same model and the top-K most similar chunks are fetched\n   using cosine similarity.\n\n3. **Generation** — The retrieved chunks are injected into the\n   prompt as context.  The language model reads the context and\n   generates a grounded answer.\n\n## Chunking Strategies\n\nChunking is the process of splitting a document into smaller\npieces that fit within the embedding model'"'"'s token limit.\n\n### Fixed-Size Chunking\n\nFixed-size chunking slides a window of N characters across the\ntext with an overlap of M characters between consecutive chunks.\nIt is simple and works well for unstructured prose.\n\n### Structural Chunking\n\nStructural chunking respects the document'"'"'s logical boundaries:\nheadings for Markdown, blank-line-separated paragraphs for\nplain text, and page breaks for PDFs.  Each structural unit\nbecomes one or more chunks, preserving the section context.\n\n## Overlap\n\nOverlap ensures that sentences that fall near a chunk boundary\nappear in both the preceding and the following chunk.  This\nprevents the retriever from missing relevant context that would\notherwise be split across two non-overlapping chunks.\n' > "$(DEMO_DIR)/rag_intro.md"
	@printf '# Vector Databases and Embeddings\n\n## What Are Embeddings?\n\nEmbeddings are dense numerical representations of text.\nA good embedding model maps semantically similar texts to\nvectors that are close together in the high-dimensional space.\nPopular open-source models include nomic-embed-text (Nomic AI),\nall-minilm (Sentence Transformers) and mxbai-embed-large.\n\n## Cosine Similarity\n\nThe most common distance metric for retrieval is cosine\nsimilarity.  It measures the angle between two vectors and\nreturns 1.0 for identical directions and 0.0 for orthogonal\nvectors.  It is invariant to the magnitude of the vectors,\nwhich makes it robust to differences in text length.\n\n## Storage Options\n\n### SQLite with Manual Scan\n\nFor small corpora (up to ~100 k chunks) it is practical to\nstore embeddings as JSON blobs in SQLite and perform a linear\ncosine scan in the application.  This requires no additional\ninfrastructure and is trivial to set up.\n\n### Dedicated Vector Stores\n\nFor larger corpora consider a dedicated vector database such\nas Chroma, Qdrant, Weaviate or pgvector (PostgreSQL extension).\nThese engines maintain an HNSW or IVF index that enables\nsub-linear approximate nearest-neighbour search.\n' > "$(DEMO_DIR)/embeddings.md"
	@printf 'What is a Large Language Model\n\nA large language model (LLM) is a neural network trained on\nmassive text corpora to predict the next token in a sequence.\nModern LLMs are based on the Transformer architecture introduced\nin the 2017 paper "Attention Is All You Need" by Vaswani et al.\n\nLLMs learn statistical patterns from billions of documents,\nenabling them to perform a wide range of natural language tasks\nwithout task-specific training: question answering, summarisation,\ntranslation, code generation, and reasoning.\n\nContext Window\n\nThe context window is the maximum number of tokens an LLM can\nprocess in a single forward pass.  Older models like GPT-3 had\na context window of 2 048 tokens.  Modern models support 128 k\nor even 1 M tokens, which allows entire books to fit in-context.\n\nRAG vs Fine-Tuning\n\nFine-tuning updates the weights of a pre-trained model on\ndomain-specific data.  It is expensive and requires retraining\nwhenever the knowledge base changes.\n\nRAG, by contrast, keeps the model weights frozen and injects\nup-to-date information at inference time by retrieving relevant\npassages.  It is cheaper, more flexible, and keeps the knowledge\nbase easy to audit and update.\n' > "$(DEMO_DIR)/llm_basics.txt"
	@echo "  ✓ $(DEMO_DIR)/rag_intro.md"
	@echo "  ✓ $(DEMO_DIR)/embeddings.md"
	@echo "  ✓ $(DEMO_DIR)/llm_basics.txt"

run-demo-clean: ## Remove demo database and sample files
	@rm -rf "$(DEMO_DIR)" "$(DEMO_DB)"
	@echo "  Demo files removed: $(DEMO_DIR)  $(DEMO_DB)"

run-demo-fixed: build run-demo-docs ## Demo: index sample docs with fixed-size strategy then run a sample search (requires Ollama + nomic-embed-text)
	@echo "=========================================="
	@echo "  RAG Demo — Fixed Chunking"
	@echo "  Requires: ollama pull $(EMBED_MODEL)"
	@echo "=========================================="
	@rm -f "$(DEMO_DB)"
	@$(MAKE) --no-print-directory run-index \
		INPUT="$(DEMO_DIR)" DB="$(DEMO_DB)" \
		STRATEGY=fixed CHUNK_SIZE=400 OVERLAP=80
	@echo ""
	@$(MAKE) --no-print-directory run-stats DB="$(DEMO_DB)"
	@echo ""
	@echo "--- Sample search: 'how does retrieval augmented generation work' ---"
	@$(MAKE) --no-print-directory run-search \
		QUERY="how does retrieval augmented generation work" \
		DB="$(DEMO_DB)" TOP_K=3
	@echo ""
	@echo "=========================================="
	@echo "  Demo complete. DB: $(DEMO_DB)"
	@echo "  Try your own query: make run-search QUERY=\"...\" DB=$(DEMO_DB)"
	@echo "=========================================="

run-demo-structural: build run-demo-docs ## Demo: index sample docs with structural strategy then run a sample search (requires Ollama + nomic-embed-text)
	@echo "=========================================="
	@echo "  RAG Demo — Structural Chunking"
	@echo "  Requires: ollama pull $(EMBED_MODEL)"
	@echo "=========================================="
	@rm -f "$(DEMO_DB)"
	@$(MAKE) --no-print-directory run-index \
		INPUT="$(DEMO_DIR)" DB="$(DEMO_DB)" \
		STRATEGY=structural CHUNK_SIZE=0 OVERLAP=100
	@echo ""
	@$(MAKE) --no-print-directory run-stats DB="$(DEMO_DB)"
	@echo ""
	@echo "--- Sample search: 'cosine similarity vector distance' ---"
	@$(MAKE) --no-print-directory run-search \
		QUERY="cosine similarity vector distance" \
		DB="$(DEMO_DB)" TOP_K=3
	@echo ""
	@echo "=========================================="
	@echo "  Demo complete. DB: $(DEMO_DB)"
	@echo "  Try your own query: make run-search QUERY=\"...\" DB=$(DEMO_DB)"
	@echo "=========================================="

run-demo: build run-demo-clean run-demo-docs ## Demo: compare fixed vs structural strategies — index same docs twice, show stats for both (requires Ollama + nomic-embed-text)
	@echo "=========================================="
	@echo "  RAG Demo — Strategy Comparison"
	@echo "  Documents: $(DEMO_DIR)"
	@echo "  Requires:  ollama pull $(EMBED_MODEL)"
	@echo "=========================================="
	@echo ""
	@FIXED_DB="$(DEMO_DB).fixed.db"; \
	STRUCT_DB="$(DEMO_DB).struct.db"; \
	rm -f "$$FIXED_DB" "$$STRUCT_DB"; \
	\
	echo "--- [1/4] Indexing with FIXED strategy (chunk-size=400, overlap=80) ---"; \
	./$(BINARY_NAME) index \
		--input "$(DEMO_DIR)" --db "$$FIXED_DB" \
		--strategy fixed --chunk-size 400 --overlap 80 \
		--embed-url "$(EMBED_URL)" --embed-model "$(EMBED_MODEL)" \
		$(EMBED_KEY_FLAG); \
	echo ""; \
	echo "--- [2/4] Indexing with STRUCTURAL strategy (overlap=100) ---"; \
	./$(BINARY_NAME) index \
		--input "$(DEMO_DIR)" --db "$$STRUCT_DB" \
		--strategy structural --overlap 100 \
		--embed-url "$(EMBED_URL)" --embed-model "$(EMBED_MODEL)" \
		$(EMBED_KEY_FLAG); \
	echo ""; \
	echo "--- Stats: fixed ---"; ./$(BINARY_NAME) stats --db "$$FIXED_DB"; \
	echo "--- Stats: structural ---"; ./$(BINARY_NAME) stats --db "$$STRUCT_DB"; \
	echo ""; \
	Q="what is the difference between RAG and fine-tuning"; \
	echo "--- [3/4] Search (fixed):      $$Q ---"; \
	./$(BINARY_NAME) search \
		--query "$$Q" --db "$$FIXED_DB" --top-k 2 \
		--embed-url "$(EMBED_URL)" --embed-model "$(EMBED_MODEL)" \
		$(EMBED_KEY_FLAG); \
	echo ""; \
	echo "--- [4/4] Search (structural): $$Q ---"; \
	./$(BINARY_NAME) search \
		--query "$$Q" --db "$$STRUCT_DB" --top-k 2 \
		--embed-url "$(EMBED_URL)" --embed-model "$(EMBED_MODEL)" \
		$(EMBED_KEY_FLAG); \
	echo ""; \
	echo "=========================================="; \
	echo "  Comparison complete."; \
	echo "  fixed DB:      $$FIXED_DB"; \
	echo "  structural DB: $$STRUCT_DB"; \
	echo "  make run-search QUERY=\"...\" DB=$$FIXED_DB"; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Integration test — compare fixed vs structural chunking with overlap=200
#
# Uses the same sample documents as run-demo-docs (created automatically).
# Override the document set with INT_DOC=<path> and the report file with
# INT_REPORT=<filename>.
#
# Requires: Ollama (or compatible) running with the embed model pulled:
#   ollama pull nomic-embed-text
# ---------------------------------------------------------------------------

INT_DOC        ?= ../postgresql_internals-15.pdf
INT_CHUNK_SIZE := 1000
INT_OVERLAP    := 200
INT_REPORT     ?= integration_result.txt

run-integration: build ## Integration test: index INT_DOC with fixed+structural (overlap=200), compare 3 queries → integration_result.txt
	EMBED_URL="$(EMBED_URL)" EMBED_MODEL="$(EMBED_MODEL)" EMBED_KEY="$(EMBED_KEY)" \
	$(GO) test -v -tags integration -run TestIntegration -timeout 30m .

run-integration-clean: ## Remove integration report
	@rm -f "$(INT_REPORT)"
	@echo "  Integration artifacts removed: $(INT_REPORT)"

# ---------------------------------------------------------------------------
# Help
# ---------------------------------------------------------------------------

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*##"}{printf "  %-28s %s\n", $$1, $$2}'
