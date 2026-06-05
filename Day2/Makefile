.PHONY: test clean help \
        run-markdown run-json run-custom-format \
        run-max-tokens run-stop-sequence run-combined \
        run-debug run-all-integration \
        check-env build

BINARY_NAME := ai-adv-agent-day2
GO := go
GOFLAGS :=
LDFLAGS := -s -w
SOURCES := $(shell find . -name "*.go" -not -name "*_test.go")

export LLM_PROVIDER ?= openrouter
export LLM_MODEL ?= openai/gpt-4o-mini
export LLM_BASE_URL ?= https://openrouter.ai/api/v1
export LLM_API_KEY

TEST_QUERY ?= "Напиши краткое описание достопримечательностей Казани"

API_KEY_MASKED = $(shell if [ -n "$(LLM_API_KEY)" ]; then echo "$(LLM_API_KEY)" | awk '{l=length($$0); if(l>8) print substr($$0,1,4)"****"substr($$0,l-3); else if(l>4) print substr($$0,1,2)"****"; else print "****"}'; else echo "<not set>"; fi)

check-env: ## Verify environment variables
	@if [ -z "$(LLM_API_KEY)" ]; then \
		echo "Error: LLM_API_KEY is not set"; \
		echo "Run: export LLM_API_KEY=your-api-key"; \
		exit 1; \
	fi
	@echo "✓ LLM_API_KEY=$(API_KEY_MASKED)"
	@echo "✓ LLM_PROVIDER=$(LLM_PROVIDER)"
	@echo "✓ LLM_MODEL=$(LLM_MODEL)"

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

run-markdown: $(BINARY_NAME) check-env ## Format markdown
	@echo "=== Testing --format markdown ==="
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format markdown"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format markdown

run-json: $(BINARY_NAME) check-env ## Format json
	@echo "=== Testing --format json ==="
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format json"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format json

run-custom-format: $(BINARY_NAME) check-env ## Format via --format-hint
	@echo "=== Testing --format-hint ==="
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format-hint \"Format as a numbered list\""
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format-hint "Format as a numbered list"

run-max-tokens: $(BINARY_NAME) check-env ## --max-tokens 50 + debug
	@echo "=== Testing --max-tokens 50 ==="
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --max-tokens 50 --debug"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --max-tokens 50 --debug

run-stop-sequence: $(BINARY_NAME) check-env ## Stop sequences
	@echo "=== Testing --stop sequences ==="
	@echo "$$ ./$(BINARY_NAME) --query \"Перечисли 10 стран Европы\" --stop \"5.\" --stop \"6.\""
	@./$(BINARY_NAME) --query "Перечисли 10 стран Европы" --stop "5." --stop "6."

run-combined: $(BINARY_NAME) check-env ## All flags combined
	@echo "=== Testing combined flags ==="
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format-hint \"Format as a structured report with sections\" --max-tokens 200 --stop \"END\" --debug"
	@./$(BINARY_NAME) \
		--query $(TEST_QUERY) \
		--format-hint "Format as a structured report with sections" \
		--max-tokens 200 \
		--stop "END" \
		--debug

run-debug: $(BINARY_NAME) check-env ## Debug mode
	@echo "=== Testing --debug mode ==="
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --debug"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --debug

run-all-integration: $(BINARY_NAME) check-env ## Run all 6 integration tests
	@echo "=========================================="
	@echo "Starting all integration tests"
	@echo "=========================================="
	@printf "LLM_API_KEY: %s\n" "$(API_KEY_MASKED)"
	@echo ""
	@echo "--- [1/6] Markdown format ---"
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format markdown"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format markdown || true
	@echo ""
	@echo "--- [2/6] JSON format ---"
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format json"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format json || true
	@echo ""
	@echo "--- [3/6] Custom format hint ---"
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format-hint \"Format as a numbered list\""
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format-hint "Format as a numbered list" || true
	@echo ""
	@echo "--- [4/6] Max tokens (50) ---"
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --max-tokens 50 --debug"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --max-tokens 50 --debug || true
	@echo ""
	@echo "--- [5/6] Stop sequences ---"
	@echo "$$ ./$(BINARY_NAME) --query \"Перечисли 10 стран Европы\" --stop \"5.\" --stop \"6.\""
	@./$(BINARY_NAME) --query "Перечисли 10 стран Европы" --stop "5." --stop "6." || true
	@echo ""
	@echo "--- [6/6] Combined flags ---"
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format-hint \"Format as a structured report\" --max-tokens 200 --stop \"END\" --debug"
	@./$(BINARY_NAME) \
		--query $(TEST_QUERY) \
		--format-hint "Format as a structured report" \
		--max-tokens 200 \
		--stop "END" \
		--debug || true
	@echo ""
	@echo "=========================================="
	@echo "All integration tests completed"
	@echo "=========================================="

run-custom: $(BINARY_NAME) check-env ## Run with TEST_QUERY override
	@echo "=== Testing custom configuration ==="
	@echo "$$ ./$(BINARY_NAME) --query \"$(TEST_QUERY)\" --debug"
	@./$(BINARY_NAME) --query "$(TEST_QUERY)" --debug

run-openai-markdown: $(BINARY_NAME) check-env ## OpenAI markdown test
	@echo "=== OpenAI: markdown format ==="
	$(eval override LLM_PROVIDER := openai)
	$(eval override LLM_MODEL := gpt-4o-mini)
	$(eval override LLM_BASE_URL := https://api.openai.com/v1)
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format markdown"
	@echo "  (with LLM_PROVIDER=openai LLM_MODEL=gpt-4o-mini)"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format markdown

run-openai-json: $(BINARY_NAME) check-env ## OpenAI json test
	@echo "=== OpenAI: json format ==="
	$(eval override LLM_PROVIDER := openai)
	$(eval override LLM_MODEL := gpt-4o-mini)
	$(eval override LLM_BASE_URL := https://api.openai.com/v1)
	@echo "$$ ./$(BINARY_NAME) --query $(TEST_QUERY) --format json"
	@echo "  (with LLM_PROVIDER=openai LLM_MODEL=gpt-4o-mini)"
	@./$(BINARY_NAME) --query $(TEST_QUERY) --format json

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
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
	@echo "  make test                    # Run 7 unit tests"
	@echo "  make vet                     # Static analysis"
	@echo "  make test-coverage           # Generate coverage.html"
	@echo ""
	@echo "Integration tests:"
	@echo "  make run-markdown            # Markdown format (default)"
	@echo "  make run-json                # JSON format"
	@echo "  make run-custom-format       # Custom --format-hint"
	@echo "  make run-max-tokens          # --max-tokens 50 + debug"
	@echo "  make run-stop-sequence       # --stop sequences"
	@echo "  make run-combined            # All flags combined"
	@echo "  make run-debug               # Debug mode"
	@echo "  make run-all-integration     # Run all 6 integration tests"
	@echo ""
	@echo "Parameters:"
	@echo "  LLM_API_KEY                  # API key (required, masked in output)"
	@echo "  LLM_PROVIDER                 # openai | openrouter (default: openrouter)"
	@echo "  LLM_MODEL                    # Model name (default: openai/gpt-4o-mini)"
	@echo "  LLM_BASE_URL                 # API URL (default: https://openrouter.ai/api/v1)"
	@echo "  TEST_QUERY                   # Override test query"
	@echo ""
	@echo "Examples:"
	@echo "  export LLM_API_KEY=sk-*** && make run-all-integration"
	@echo "  make run-json LLM_PROVIDER=openai LLM_MODEL=gpt-4o-mini"
	@echo "  make run-custom TEST_QUERY=\"What is Golang?\""

.DEFAULT_GOAL := help
