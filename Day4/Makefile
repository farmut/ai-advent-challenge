.PHONY: test clean help \
        run-integration \
        run-temperature \
        run-all-integration \
        check-env build

BINARY_NAME := ai-adv-agent-day4
GO := go
GOFLAGS :=
LDFLAGS := -s -w
SOURCES := $(shell find . -name "*.go" -not -name "*_test.go")

export LLM_PROVIDER ?= openrouter
export LLM_MODEL ?= openai/gpt-4o-mini
export LLM_BASE_URL ?= https://openrouter.ai/api/v1
export LLM_API_KEY

QUERY_FILE ?= query.txt
LLM_TEMPERATURE ?=
TEMPERATURE_RESULT_FILE ?= temperature_result.txt

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

run-integration: $(BINARY_NAME) check-env ## Run with query from QUERY_FILE and LLM_TEMPERATURE
	@echo "=== Integration test ==="
	@echo "Query file:  $(QUERY_FILE)"
	@echo "Temperature: $$(if [ -n "$(LLM_TEMPERATURE)" ]; then echo "$(LLM_TEMPERATURE)"; else echo "<default>"; fi)"
	@if [ ! -f "$(QUERY_FILE)" ]; then \
		echo "Error: query file '$(QUERY_FILE)' not found"; \
		echo "Run: make run-integration QUERY_FILE=path/to/query.txt"; \
		exit 1; \
	fi
	@if [ -n "$(LLM_TEMPERATURE)" ]; then \
		echo "$$ ./$(BINARY_NAME) --query \"\$$(cat $(QUERY_FILE))\" --temperature $(LLM_TEMPERATURE)"; \
		./$(BINARY_NAME) --query "$$(cat $(QUERY_FILE))" --temperature $(LLM_TEMPERATURE); \
	else \
		echo "$$ ./$(BINARY_NAME) --query \"\$$(cat $(QUERY_FILE))\""; \
		./$(BINARY_NAME) --query "$$(cat $(QUERY_FILE))"; \
	fi

run-temperature: $(BINARY_NAME) check-env ## Run with temperatures 0, 0.7, 1.2 (results to temperature_result.txt)
	@echo "=== Temperature test ==="
	@echo "Query file:  $(QUERY_FILE)"
	@echo "Result file: $(TEMPERATURE_RESULT_FILE)"
	@if [ ! -f "$(QUERY_FILE)" ]; then \
		echo "Error: query file '$(QUERY_FILE)' not found"; \
		echo "Run: make run-temperature QUERY_FILE=path/to/query.txt"; \
		exit 1; \
	fi
	@if [ ! -f "system_revisor.txt" ]; then \
		echo "Error: system file 'system_revisor.txt' not found"; \
		exit 1; \
	fi
	@QUERY_CONTENT="$$(cat $(QUERY_FILE))"; \
	rm -f $(TEMPERATURE_RESULT_FILE); \
	echo "=== Temperature: 0 ===" >> $(TEMPERATURE_RESULT_FILE); \
	./$(BINARY_NAME) --query "$$QUERY_CONTENT" --temperature 0 >> $(TEMPERATURE_RESULT_FILE); \
	printf '\n#####\n\n' >> $(TEMPERATURE_RESULT_FILE); \
	echo "=== Temperature: 0.7 ===" >> $(TEMPERATURE_RESULT_FILE); \
	./$(BINARY_NAME) --query "$$QUERY_CONTENT" --temperature 0.7 >> $(TEMPERATURE_RESULT_FILE); \
	printf '\n#####\n\n' >> $(TEMPERATURE_RESULT_FILE); \
	echo "=== Temperature: 1.2 ===" >> $(TEMPERATURE_RESULT_FILE); \
	./$(BINARY_NAME) --query "$$QUERY_CONTENT" --temperature 1.2 >> $(TEMPERATURE_RESULT_FILE); \
	echo ""; \
	echo "=========================================="; \
	echo "Temperature test completed"; \
	echo "Results saved to: $(TEMPERATURE_RESULT_FILE)"; \
	echo "=========================================="; \
	echo ""; \
	echo "--- Final integration evaluation ---"; \
	REVISOR_SYSTEM="$$(cat system_revisor.txt)"; \
	FINAL_QUERY="$$(printf '%s\n\n%s' "$$(cat $(TEMPERATURE_RESULT_FILE))" "Оцени эти рассказы как эксперт")"; \
	./$(BINARY_NAME) --query "$$FINAL_QUERY" --system "$$REVISOR_SYSTEM"

run-all-integration: $(BINARY_NAME) check-env ## Run all integration tests
	@echo "=========================================="
	@echo "Starting all integration tests"
	@echo "=========================================="
	@printf "LLM_API_KEY: %s\n" "$(API_KEY_MASKED)"
	@if [ ! -f "$(QUERY_FILE)" ]; then \
		echo "Error: query file '$(QUERY_FILE)' not found"; \
		echo "Run: make run-all-integration QUERY_FILE=path/to/query.txt"; \
		exit 1; \
	fi
	@QUERY_CONTENT="$$(cat $(QUERY_FILE))"; \
	echo ""; \
	echo "--- [1/4] Markdown format ---"; \
	echo "$$ ./$(BINARY_NAME) --query <$(QUERY_FILE)> --format markdown"; \
	./$(BINARY_NAME) --query "$$QUERY_CONTENT" --format markdown || true; \
	echo ""; \
	echo "--- [2/4] JSON format ---"; \
	echo "$$ ./$(BINARY_NAME) --query <$(QUERY_FILE)> --format json"; \
	./$(BINARY_NAME) --query "$$QUERY_CONTENT" --format json || true; \
	echo ""; \
	echo "--- [3/4] Max tokens (100) ---"; \
	echo "$$ ./$(BINARY_NAME) --query <$(QUERY_FILE)> --max-tokens 100 --debug"; \
	./$(BINARY_NAME) --query "$$QUERY_CONTENT" --max-tokens 100 --debug || true; \
	echo ""; \
	echo "--- [4/4] Stop sequences ---"; \
	echo "$$ ./$(BINARY_NAME) --query <$(QUERY_FILE)> --stop \"END\" --debug"; \
	./$(BINARY_NAME) --query "$$QUERY_CONTENT" --stop "END" --debug || true; \
	echo ""; \
	echo "=========================================="; \
	echo "All integration tests completed"; \
	echo "=========================================="

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -f $(TEMPERATURE_RESULT_FILE)
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
	@echo "Integration tests:"
	@echo "  make run-integration         # Run with query from file + temperature"
	@echo "  make run-temperature         # Test temperatures 0, 0.7, 1.2"
	@echo "  make run-all-integration     # Run all 4 integration tests"
	@echo ""
	@echo "Parameters:"
	@echo "  LLM_API_KEY                  # API key (required, masked in output)"
	@echo "  LLM_PROVIDER                 # openai | openrouter (default: openrouter)"
	@echo "  LLM_MODEL                    # Model name (default: openai/gpt-4o-mini)"
	@echo "  LLM_BASE_URL                 # API URL (default: https://openrouter.ai/api/v1)"
	@echo "  QUERY_FILE                   # Path to query file (default: query.txt)"
	@echo "  LLM_TEMPERATURE              # Sampling temperature 0.0-2.0 (optional, default: not set)"
	@echo "  TEMPERATURE_RESULT_FILE      # Output file for temperature tests (default: temperature_result.txt)"
	@echo ""
	@echo "Examples:"
	@echo "  export LLM_API_KEY=sk-*** && make run-integration"
	@echo "  make run-integration QUERY_FILE=myquery.txt LLM_TEMPERATURE=0.7"
	@echo "  make run-temperature"
	@echo "  make run-all-integration"

.DEFAULT_GOAL := help
