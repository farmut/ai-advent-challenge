.PHONY: test clean help \
        run-longest-sequence \
        run-full-report \
        check-env build

BINARY_NAME := ai-adv-agent-day3
GO := go
GOFLAGS :=
LDFLAGS := -s -w
SOURCES := $(shell find . -name "*.go" -not -name "*_test.go")

export LLM_PROVIDER ?= openrouter
export LLM_MODEL ?= openai/gpt-4o-mini
export LLM_BASE_URL ?= https://openrouter.ai/api/v1
export LLM_API_KEY

TEST_QUERY_FILE ?= query.txt
SYSTEM_FILE ?= system.txt

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

run-longest-sequence: $(BINARY_NAME) check-env ## Solve longest consecutive sequence problem
	@echo "=== Longest Consecutive Sequence ==="
	@{ \
		if [ -n "$(QUERY)" ]; then \
			QUERY_VAL="$(QUERY)"; \
			query_src="inline query"; \
		elif [ -f "$(TEST_QUERY_FILE)" ]; then \
			QUERY_VAL="$$(cat $(TEST_QUERY_FILE))"; \
			query_src="query file: $(TEST_QUERY_FILE)"; \
		else \
			echo "Error: query file '$(TEST_QUERY_FILE)' not found"; \
			echo "Run: make run-longest-sequence TEST_QUERY_FILE=path/to/query.txt"; \
			echo "Or:  make run-longest-sequence QUERY=\"your query here\""; \
			exit 1; \
		fi; \
		if [ -n "$(SYSTEM)" ]; then \
			SYSTEM_VAL="$(SYSTEM)"; \
			system_src="inline system"; \
		elif [ -f "$(SYSTEM_FILE)" ]; then \
			SYSTEM_VAL="$$(cat $(SYSTEM_FILE))"; \
			system_src="system file: $(SYSTEM_FILE)"; \
		else \
			SYSTEM_VAL=""; \
			system_src="(no system role)"; \
		fi; \
		echo "Using $$query_src"; \
		echo "Using $$system_src"; \
		if [ -n "$$SYSTEM_VAL" ]; then \
			./$(BINARY_NAME) --query "$$QUERY_VAL" --system "$$SYSTEM_VAL"; \
		else \
			./$(BINARY_NAME) --query "$$QUERY_VAL"; \
		fi; \
	}

run-full-report: $(BINARY_NAME) check-env ## Generate 4 reports + final review (pipeline)
	@echo "=========================================="
	@echo "  Full Report Pipeline (6 steps)"
	@echo "=========================================="
	@set -e; \
	for f in query.txt query_by_steps.txt system_pre_promt.txt system_experts.txt system_final_reviewer.txt; do \
		if [ ! -f "$$f" ]; then \
			echo "Error: required file '$$f' is missing"; \
			exit 1; \
		fi; \
	done; \
	QUERY_TXT="$$(cat query.txt)"; \
	QUERY_BY_STEPS="$$(cat query_by_steps.txt)"; \
	SYSTEM_PRE="$$(cat system_pre_promt.txt)"; \
	SYSTEM_EXPERTS="$$(cat system_experts.txt)"; \
	SYSTEM_FINAL="$$(cat system_final_reviewer.txt)"; \
	echo ""; \
	echo "--- Step 1/6: common query (no system) -> report_common_query.txt"; \
	{ echo "Решение 1 — ответ без дополнительных инструкций"; \
	  ./$(BINARY_NAME) --query "$$QUERY_TXT"; } > report_common_query.txt; \
	echo "    ✓ report_common_query.txt"; \
	echo "--- Step 2/6: query_by_steps -> report_by_steps_query.txt"; \
	{ echo "Решение 2 — ответ разбитый по шагам"; \
	  ./$(BINARY_NAME) --query "$$QUERY_BY_STEPS"; } > report_by_steps_query.txt; \
	echo "    ✓ report_by_steps_query.txt"; \
	echo "--- Step 3/6: query.txt + system_pre_promt.txt -> llm_promt.txt"; \
	./$(BINARY_NAME) --query "$$QUERY_TXT" --system "$$SYSTEM_PRE" > llm_promt.txt; \
	echo "    ✓ llm_promt.txt"; \
	echo "--- Step 4/6: llm_promt.txt -> report_llm_promt.txt"; \
	LLM_PROMT="$$(cat llm_promt.txt)"; \
	{ echo "Решение 3 — ответ LLM promt"; \
	  ./$(BINARY_NAME) --query "$$LLM_PROMT"; } > report_llm_promt.txt; \
	echo "    ✓ report_llm_promt.txt"; \
	echo "--- Step 5/6: query.txt + system_experts.txt -> report_experts_query.txt"; \
	{ echo "Решение 4 — ответ 3х экспертов"; \
	  ./$(BINARY_NAME) --query "$$QUERY_TXT" --system "$$SYSTEM_EXPERTS"; } > report_experts_query.txt; \
	echo "    ✓ report_experts_query.txt"; \
	echo "--- Step 6/6: merge all reports + system_final_reviewer.txt -> final_report.txt"; \
	{ cat report_common_query.txt; \
	  printf '\n\n'; \
	  cat report_by_steps_query.txt; \
	  printf '\n\n'; \
	  cat report_llm_promt.txt; \
	  printf '\n\n'; \
	  cat report_experts_query.txt; } > .combined_reports.tmp; \
	COMBINED="$$(cat .combined_reports.tmp)"; \
	rm -f .combined_reports.tmp; \
	./$(BINARY_NAME) --query "$$COMBINED" --system "$$SYSTEM_FINAL" > final_report.txt; \
	echo "    ✓ final_report.txt"; \
	echo ""; \
	echo "=========================================="; \
	echo "  Pipeline finished. Files generated:"; \
	echo "    - report_common_query.txt"; \
	echo "    - report_by_steps_query.txt"; \
	echo "    - llm_promt.txt"; \
	echo "    - report_llm_promt.txt"; \
	echo "    - report_experts_query.txt"; \
	echo "    - final_report.txt"; \
	echo "=========================================="

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -f report_*.txt llm_promt.txt final_report.txt .combined_reports.tmp
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
	@echo "  make run-longest-sequence    # Solve longest consecutive sequence problem"
	@echo "  make run-full-report         # Generate 4 reports + final review (pipeline)"
	@echo ""
	@echo "Parameters:"
	@echo "  LLM_API_KEY                  # API key (required, masked in output)"
	@echo "  LLM_PROVIDER                 # openai | openrouter (default: openrouter)"
	@echo "  LLM_MODEL                    # Model name (default: openai/gpt-4o-mini)"
	@echo "  LLM_BASE_URL                 # API URL (default: https://openrouter.ai/api/v1)"
	@echo "  TEST_QUERY_FILE              # Query file path (default: query.txt)"
	@echo "  QUERY                        # Inline query string (overrides file)"
	@echo "  SYSTEM_FILE                  # System role file path (default: system.txt)"
	@echo "  SYSTEM                       # Inline system role string (overrides file)"
	@echo ""
	@echo "Full pipeline required files:"
	@echo "  query.txt, query_by_steps.txt, system_pre_promt.txt,"
	@echo "  system_experts.txt, system_final_reviewer.txt"
	@echo ""
	@echo "Examples:"
	@echo "  export LLM_API_KEY=sk-*** && make run-longest-sequence"
	@echo "  make run-longest-sequence TEST_QUERY_FILE=myquery.txt"
	@echo "  make run-longest-sequence QUERY=\"Найди длину последовательности\""
	@echo "  make run-longest-sequence SYSTEM_FILE=myrole.txt"
	@echo "  make run-longest-sequence SYSTEM=\"Ты опытный программист на Go\""
	@echo "  make run-longest-sequence QUERY=\"Explain closures\" SYSTEM=\"Act as a teacher\""
	@echo "  make run-full-report"

.DEFAULT_GOAL := help
