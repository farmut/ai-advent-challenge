.PHONY: test clean help check-env build run-integration run-integration-batch

BINARY_NAME := ai-adv-agent-day5
GO := go
GOFLAGS :=
LDFLAGS := -s -w
SOURCES := $(shell find . -name "*.go" -not -name "*_test.go")

export LLM_PROVIDER ?= openrouter
export LLM_MODEL ?= openai/gpt-4o-mini
export LLM_BASE_URL ?= https://openrouter.ai/api/v1
export LLM_API_KEY

QUERY_FILE ?= query.txt

QWEN_MODELS := qwen/qwen3.5-9b qwen/qwen3.5-122b-a10b qwen/qwen3.5-397b-a17b
BATCH_OUTPUT ?= test_output.txt
EXPERT_MODEL ?= openai/gpt-oss-120b:free
SYSTEM_EXPERT_FILE ?= system_expert.txt

API_KEY_MASKED = $(shell if [ -n "$(LLM_API_KEY)" ]; then echo "$(LLM_API_KEY)" | awk '{l=length($$0); if(l>8) print substr($$0,1,4)"****"substr($$0,l-3); else if(l>4) print substr($$0,1,2)"****"; else print "****"}'; else echo "<not set>"; fi)

check-env: ## Verify environment variables
	@if [ -z "$(LLM_API_KEY)" ]; then \
		echo "Error: LLM_API_KEY is not set"; \
		echo "Run: export LLM_API_KEY=your-api-key"; \
		exit 1; \
	fi
	@echo "LLM_API_KEY=$(API_KEY_MASKED)"
	@echo "LLM_PROVIDER=$(LLM_PROVIDER)"
	@echo "LLM_MODEL=$(LLM_MODEL)"

$(BINARY_NAME): $(SOURCES) ## Build the application (implicit target)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

build: $(BINARY_NAME) ## Explicit build target

test: ## Run unit tests
	$(GO) test -v ./...

test-coverage: ## Generate coverage.html
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

vet: ## Static analysis
	$(GO) vet ./...

run-integration: $(BINARY_NAME) check-env ## Run integration test with --query and --debug (QUERY= or QUERY_FILE=)
	@echo "=== Integration test (debug) ==="
	@if [ -n "$(QUERY)" ]; then \
		echo "Query (inline): $(QUERY)"; \
		echo "$$ ./$(BINARY_NAME) --query \"$(QUERY)\" --debug"; \
		./$(BINARY_NAME) --query "$(QUERY)" --debug; \
	else \
		echo "Query file: $(QUERY_FILE)"; \
		if [ ! -f "$(QUERY_FILE)" ]; then \
			echo "Error: query file '$(QUERY_FILE)' not found"; \
			echo "Run: make run-integration QUERY_FILE=path/to/query.txt"; \
			echo "  or: make run-integration QUERY=\"your query\""; \
			exit 1; \
		fi; \
		echo "$$ ./$(BINARY_NAME) --query \"\$$(cat $(QUERY_FILE))\" --debug"; \
		./$(BINARY_NAME) --query "$$(cat $(QUERY_FILE))" --debug; \
	fi

run-integration-batch: $(BINARY_NAME) check-env ## Batch integration: qwen models -> final expert evaluation
	@if [ ! -f "$(QUERY_FILE)" ]; then \
		echo "Error: query file '$(QUERY_FILE)' not found"; \
		echo "Run: make run-integration-batch QUERY_FILE=path/to/query.txt"; \
		exit 1; \
	fi
	@if [ ! -f "$(SYSTEM_EXPERT_FILE)" ]; then \
		echo "Error: system expert file '$(SYSTEM_EXPERT_FILE)' not found"; \
		exit 1; \
	fi
	@rm -f $(BATCH_OUTPUT)
	@echo "=== Batch integration test ==="
	@echo "Query file:   $(QUERY_FILE)"
	@echo "Models:       $(QWEN_MODELS)"
	@echo "Expert model: $(EXPERT_MODEL)"
	@echo "Expert file:  $(SYSTEM_EXPERT_FILE)"
	@echo "Output file:  $(BATCH_OUTPUT)"
	@echo ""
	@QUERY_CONTENT="$$(cat $(QUERY_FILE))"; \
	for model in $(QWEN_MODELS); do \
		echo "--- [Model: $$model] ---"; \
		echo "=== Model: $$model ===" >> $(BATCH_OUTPUT); \
		ANS_LOG=$$(mktemp); \
		DEBUG_LOG=$$(mktemp); \
		LLM_MODEL="$$model" ./$(BINARY_NAME) --query "$$QUERY_CONTENT" --debug \
			>"$$ANS_LOG" 2>"$$DEBUG_LOG"; \
		grep '^\[usage\]' "$$DEBUG_LOG" >> $(BATCH_OUTPUT); \
		cat "$$ANS_LOG" >> $(BATCH_OUTPUT); \
		rm -f "$$DEBUG_LOG" "$$ANS_LOG"; \
		printf '\n#####\n\n' >> $(BATCH_OUTPUT); \
		echo ""; \
	done
	@echo "--- [Final evaluation: $(EXPERT_MODEL)] ---"
	@SYSTEM_CONTENT="$$(cat $(SYSTEM_EXPERT_FILE))"; \
	BATCH_CONTENT="$$(cat $(BATCH_OUTPUT))"; \
	LLM_MODEL=$(EXPERT_MODEL) ./$(BINARY_NAME) --query "$$BATCH_CONTENT" --system "$$SYSTEM_CONTENT"
	@echo ""
	@echo "=========================================="
	@echo "Batch integration test completed"
	@echo "Results saved to: $(BATCH_OUTPUT)"
	@echo "=========================================="

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -f $(BATCH_OUTPUT)
	$(GO) clean -testcache

install-deps: ## Install and tidy dependencies
	$(GO) mod download
	$(GO) mod tidy

help: ## Show this help
	@echo "Available targets:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Build & test:"
	@echo "  make build                   # Build the application"
	@echo "  make test                    # Run unit tests"
	@echo "  make vet                     # Static analysis"
	@echo "  make test-coverage           # Generate coverage.html"
	@echo ""
	@echo "Integration test:"
	@echo "  make run-integration             # Run with --query from QUERY_FILE and --debug"
	@echo "  make run-integration QUERY=\"...\"  # Run with inline query and --debug"
	@echo ""
	@echo "Integration batch test:"
	@echo "  make run-integration-batch        # qwen models -> expert evaluation"
	@echo ""
	@echo "Parameters:"
	@echo "  LLM_API_KEY                  # API key (required, masked in output)"
	@echo "  LLM_PROVIDER                 # openai | openrouter (default: openrouter)"
	@echo "  LLM_MODEL                    # Model name (default: openai/gpt-4o-mini)"
	@echo "  LLM_BASE_URL                 # API URL (default: https://openrouter.ai/api/v1)"
	@echo "  QUERY_FILE                   # Path to query file (default: query.txt)"
	@echo "  BATCH_OUTPUT                 # Output file for batch test (default: test_output.txt)"
	@echo "  SYSTEM_EXPERT_FILE           # System prompt for expert evaluation (default: system_expert.txt)"
	@echo "  EXPERT_MODEL                 # Model for expert evaluation (default: openai/gpt-oss-120b:free)"

.DEFAULT_GOAL := help
