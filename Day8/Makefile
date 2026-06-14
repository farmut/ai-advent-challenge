.PHONY: test clean help check-env build \
        run-integration run-integration-batch \
        run-history-basic run-history-accumulate run-history-disabled run-history-context \
        run-history-all \
        run-tokens run-tokens-cost run-tokens-session \
        run-context-overflow

BINARY_NAME := ai-adv-agent-day8
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

HISTORY_TEST_FILE ?= /tmp/test_chat_history_day8.json
OVERFLOW_MODEL    ?= openai/gpt-4o-mini

# Set DEBUG=1 to add --debug flag to any integration target (prints raw JSON payload).
DEBUG_FLAG = $(if $(DEBUG),--debug,)

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

run-integration: $(BINARY_NAME) check-env ## Run integration test with --show-cost (add DEBUG=1 for raw payload)
	@echo "=== Integration test ==="
	@if [ -n "$(QUERY)" ]; then \
		echo "Query (inline): $(QUERY)"; \
		./$(BINARY_NAME) --query "$(QUERY)" --show-cost $(DEBUG_FLAG); \
	else \
		echo "Query file: $(QUERY_FILE)"; \
		if [ ! -f "$(QUERY_FILE)" ]; then \
			echo "Error: query file '$(QUERY_FILE)' not found"; \
			echo "Run: make run-integration QUERY_FILE=path/to/query.txt"; \
			echo "  or: make run-integration QUERY=\"your query\""; \
			exit 1; \
		fi; \
		./$(BINARY_NAME) --query "$$(cat $(QUERY_FILE))" --show-cost $(DEBUG_FLAG); \
	fi

run-integration-batch: $(BINARY_NAME) check-env ## Batch integration: qwen models -> final expert evaluation (QUERY= or QUERY_FILE=)
	@if [ -z "$(QUERY)" ] && [ ! -f "$(QUERY_FILE)" ]; then \
		echo "Error: neither QUERY= nor query file '$(QUERY_FILE)' provided"; \
		echo "Run: make run-integration-batch QUERY=\"your query\""; \
		echo "  or: make run-integration-batch QUERY_FILE=path/to/query.txt"; \
		exit 1; \
	fi
	@if [ ! -f "$(SYSTEM_EXPERT_FILE)" ]; then \
		echo "Error: system expert file '$(SYSTEM_EXPERT_FILE)' not found"; \
		exit 1; \
	fi
	@rm -f $(BATCH_OUTPUT)
	@echo "=== Batch integration test ==="
	@if [ -n "$(QUERY)" ]; then \
		echo "Query (inline): $(QUERY)"; \
	else \
		echo "Query file:   $(QUERY_FILE)"; \
	fi
	@echo "Models:       $(QWEN_MODELS)"
	@echo "Expert model: $(EXPERT_MODEL)"
	@echo "Expert file:  $(SYSTEM_EXPERT_FILE)"
	@echo "Output file:  $(BATCH_OUTPUT)"
	@echo ""
	@if [ -n "$(QUERY)" ]; then \
		QUERY_CONTENT="$(QUERY)"; \
	else \
		QUERY_CONTENT="$$(cat $(QUERY_FILE))"; \
	fi; \
	for model in $(QWEN_MODELS); do \
		echo "--- [Model: $$model] ---"; \
		echo "=== Model: $$model ===" >> $(BATCH_OUTPUT); \
		ANS_LOG=$$(mktemp); \
		STATS_LOG=$$(mktemp); \
		LLM_MODEL="$$model" ./$(BINARY_NAME) --query "$$QUERY_CONTENT" --show-cost $(DEBUG_FLAG) \
			>"$$ANS_LOG" 2>"$$STATS_LOG"; \
		cat "$$STATS_LOG" >&2; \
		grep '^\[tokens\]\|^\[cost\]' "$$STATS_LOG" >> $(BATCH_OUTPUT); \
		cat "$$ANS_LOG" >> $(BATCH_OUTPUT); \
		rm -f "$$STATS_LOG" "$$ANS_LOG"; \
		printf '\n#####\n\n' >> $(BATCH_OUTPUT); \
		echo ""; \
	done
	@echo "--- [Final evaluation: $(EXPERT_MODEL)] ---"
	@SYSTEM_CONTENT="$$(cat $(SYSTEM_EXPERT_FILE))"; \
	BATCH_CONTENT="$$(cat $(BATCH_OUTPUT))"; \
	LLM_MODEL=$(EXPERT_MODEL) ./$(BINARY_NAME) --query "$$BATCH_CONTENT" --system "$$SYSTEM_CONTENT" --show-cost $(DEBUG_FLAG)
	@echo ""
	@echo "=========================================="
	@echo "Batch integration test completed"
	@echo "Results saved to: $(BATCH_OUTPUT)"
	@echo "=========================================="

# ---------------------------------------------------------------------------
# History integration tests
# ---------------------------------------------------------------------------

run-history-basic: $(BINARY_NAME) check-env ## [History] Single call: file created with 2 messages (user + assistant)
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	rm -f "$$HIST_FILE" "$$HIST_FILE.stats.json" "$(HISTORY_TEST_FILE:.json=.stats.json)"; \
	echo "--- [History] Test 1: file created on first call ---"; \
	./$(BINARY_NAME) --query "Say the single word OK" --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	if [ ! -f "$$HIST_FILE" ]; then \
		echo "  ✗ FAIL: history file was not created"; exit 1; \
	fi; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))"); \
	ROLES=$$(python3 -c "import json; print(' '.join(m['role'] for m in json.load(open('$$HIST_FILE'))))"); \
	if [ "$$COUNT" != "2" ] || [ "$$ROLES" != "user assistant" ]; then \
		echo "  ✗ FAIL: expected 2 messages [user assistant], got $$COUNT [$$ROLES]"; exit 1; \
	fi; \
	echo "  ✓ PASS: file created with 2 messages — roles: [$$ROLES]"

run-history-accumulate: $(BINARY_NAME) check-env ## [History] 3 calls accumulate 6 messages in file
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	rm -f "$$HIST_FILE" "$(HISTORY_TEST_FILE:.json=.stats.json)"; \
	echo "--- [History] Test 2: history accumulates across calls ---"; \
	./$(BINARY_NAME) --query "First question: say ONE"   --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Second question: say TWO"  --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Third question: say THREE" --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))"); \
	ROLES=$$(python3 -c "import json; print(' '.join(m['role'] for m in json.load(open('$$HIST_FILE'))))"); \
	if [ "$$COUNT" != "6" ]; then \
		echo "  ✗ FAIL: expected 6 messages after 3 calls, got $$COUNT"; \
		echo "  roles: [$$ROLES]"; exit 1; \
	fi; \
	echo "  ✓ PASS: 6 messages after 3 calls — roles: [$$ROLES]"

run-history-disabled: $(BINARY_NAME) check-env ## [History] --history "" does not create a file
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	rm -f "$$HIST_FILE" "$(HISTORY_TEST_FILE:.json=.stats.json)"; \
	echo "--- [History] Test 3: history disabled with --history \"\" ---"; \
	./$(BINARY_NAME) --query "Say OK" --history "" --show-cost $(DEBUG_FLAG) > /dev/null; \
	if [ -f "$$HIST_FILE" ]; then \
		echo "  ✗ FAIL: history file was created despite --history being empty"; exit 1; \
	fi; \
	echo "  ✓ PASS: no history file created when --history is empty"

run-history-context: $(BINARY_NAME) check-env ## [History] Model recalls prior context from history file
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	RESP_FILE=$$(mktemp); \
	rm -f "$$HIST_FILE" "$(HISTORY_TEST_FILE:.json=.stats.json)"; \
	echo "--- [History] Test 4: model sees prior context ---"; \
	./$(BINARY_NAME) \
		--query "Remember the number 42. Reply with the single word: OK." \
		--history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) \
		--query "What number did I ask you to remember?" \
		--format-hint "Reply with the number only, no other text." \
		--history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > "$$RESP_FILE"; \
	RESPONSE=$$(cat "$$RESP_FILE"); \
	rm -f "$$RESP_FILE"; \
	echo "  Model response: $$RESPONSE"; \
	if echo "$$RESPONSE" | grep -q "42"; then \
		echo "  ✓ PASS: model recalled the number 42 from history"; \
	else \
		echo "  ✗ FAIL: model did not recall 42 from history"; exit 1; \
	fi

run-history-all: $(BINARY_NAME) check-env ## [History] Run all 4 history integration tests with summary
	@echo "=========================================="
	@echo "  History Integration Tests (Day8)"
	@echo "=========================================="
	@echo ""
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	STATS_FILE="$(HISTORY_TEST_FILE:.json=.stats.json)"; \
	FAILED=0; \
	\
	echo "--- [1/4] File created on first call ---"; \
	rm -f "$$HIST_FILE" "$$STATS_FILE"; \
	./$(BINARY_NAME) --query "Say the single word OK" --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	if [ ! -f "$$HIST_FILE" ]; then \
		echo "  ✗ FAIL: history file was not created"; FAILED=$$((FAILED+1)); \
	else \
		COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))"); \
		ROLES=$$(python3 -c "import json; print(' '.join(m['role'] for m in json.load(open('$$HIST_FILE'))))"); \
		if [ "$$COUNT" = "2" ] && [ "$$ROLES" = "user assistant" ]; then \
			echo "  ✓ PASS: file created with 2 messages — roles: [$$ROLES]"; \
		else \
			echo "  ✗ FAIL: expected 2 messages [user assistant], got $$COUNT [$$ROLES]"; FAILED=$$((FAILED+1)); \
		fi; \
	fi; \
	\
	echo ""; \
	echo "--- [2/4] History accumulates across calls ---"; \
	rm -f "$$HIST_FILE" "$$STATS_FILE"; \
	./$(BINARY_NAME) --query "First question: say ONE"   --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Second question: say TWO"  --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Third question: say THREE" --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))"); \
	if [ "$$COUNT" = "6" ]; then \
		echo "  ✓ PASS: 6 messages after 3 calls"; \
	else \
		echo "  ✗ FAIL: expected 6 messages, got $$COUNT"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	echo ""; \
	echo "--- [3/4] History disabled with --history \"\" ---"; \
	rm -f "$$HIST_FILE" "$$STATS_FILE"; \
	./$(BINARY_NAME) --query "Say OK" --history "" --show-cost $(DEBUG_FLAG) > /dev/null; \
	if [ ! -f "$$HIST_FILE" ]; then \
		echo "  ✓ PASS: no file created when --history is empty"; \
	else \
		echo "  ✗ FAIL: history file was unexpectedly created"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	echo ""; \
	echo "--- [4/4] Model recalls prior context from history ---"; \
	RESP_FILE=$$(mktemp); \
	rm -f "$$HIST_FILE" "$$STATS_FILE"; \
	./$(BINARY_NAME) \
		--query "Remember the number 42. Reply with the single word: OK." \
		--history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) \
		--query "What number did I ask you to remember?" \
		--format-hint "Reply with the number only, no other text." \
		--history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > "$$RESP_FILE"; \
	RESPONSE=$$(cat "$$RESP_FILE"); \
	rm -f "$$RESP_FILE"; \
	echo "  Model response: $$RESPONSE"; \
	if echo "$$RESPONSE" | grep -q "42"; then \
		echo "  ✓ PASS: model recalled the number 42 from history"; \
	else \
		echo "  ✗ FAIL: model did not recall 42 from history"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	echo ""; \
	echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then \
		echo "  Result: 4/4 PASSED"; \
	else \
		echo "  Result: $$FAILED/4 FAILED"; \
		exit 1; \
	fi; \
	echo "=========================================="

TOKENS_HIST_FILE ?= /tmp/token_demo_day8.json

run-tokens: $(BINARY_NAME) check-env ## Show token breakdown for a single call (--show-tokens)
	@echo "=== Token breakdown demo (single call) ==="
	@if [ -n "$(QUERY)" ]; then \
		./$(BINARY_NAME) --query "$(QUERY)" --history "$(TOKENS_HIST_FILE)" --show-tokens; \
	else \
		./$(BINARY_NAME) --query "Explain what a token is in the context of LLMs in one sentence." \
			--history "$(TOKENS_HIST_FILE)" --show-tokens; \
	fi

run-tokens-cost: $(BINARY_NAME) check-env ## Show token breakdown + estimated cost (--show-cost)
	@echo "=== Token + cost demo (single call) ==="
	@if [ -n "$(QUERY)" ]; then \
		./$(BINARY_NAME) --query "$(QUERY)" --history "$(TOKENS_HIST_FILE)" --show-cost; \
	else \
		./$(BINARY_NAME) --query "What is the capital of France? One word." \
			--history "$(TOKENS_HIST_FILE)" --show-cost; \
	fi

run-tokens-session: $(BINARY_NAME) check-env ## Run 3 turns and show cumulative session token/cost stats
	@echo "=== Session token accumulation demo (3 calls) ==="
	@rm -f "$(TOKENS_HIST_FILE)" "$(basename $(TOKENS_HIST_FILE)).stats$(suffix $(TOKENS_HIST_FILE))"
	@echo "--- Turn 1 ---"
	@./$(BINARY_NAME) --query "Remember the number 7. Reply with: OK." \
		--history "$(TOKENS_HIST_FILE)" --show-cost > /dev/null
	@echo "--- Turn 2 ---"
	@./$(BINARY_NAME) --query "Multiply the number you remembered by 6. Reply with the result only." \
		--history "$(TOKENS_HIST_FILE)" --show-cost > /dev/null
	@echo "--- Turn 3 ---"
	@./$(BINARY_NAME) --query "What was the original number you remembered?" \
		--history "$(TOKENS_HIST_FILE)" --show-cost > /dev/null
	@echo ""
	@echo "Session stats file: $(TOKENS_HIST_FILE:.json=.stats.json)"
	@if [ -f "$(TOKENS_HIST_FILE:.json=.stats.json)" ]; then cat "$(TOKENS_HIST_FILE:.json=.stats.json)"; fi

run-context-overflow: $(BINARY_NAME) check-env ## [Context] Overflow model context using chat_history.json (OVERFLOW_MODEL=...)
	@echo "=========================================="
	@echo "  Context Overflow Test"
	@echo "  Model:  $(OVERFLOW_MODEL)"
	@echo "  Source: chat_history.json (repeated until overflow)"
	@echo "=========================================="
	@OVERFLOW_FILE=$$(mktemp /tmp/overflow_XXXXXX.json); \
	python3 -c "\
import json, sys; \
CTX = {'gpt-4o': 128000, 'gpt-4o-mini': 128000, 'gpt-4-turbo': 128000, 'gpt-3.5-turbo': 16385, 'o1': 200000, 'o1-mini': 128000, 'openai/gpt-4o': 128000, 'openai/gpt-4o-mini': 128000, 'openai/gpt-4-turbo': 128000, 'openai/gpt-3.5-turbo': 16385, 'openai/o1': 200000, 'openai/o1-mini': 128000, 'anthropic/claude-opus-4': 200000, 'anthropic/claude-sonnet-4': 200000, 'anthropic/claude-3-5-sonnet': 200000, 'anthropic/claude-3-haiku': 200000, 'anthropic/claude-3-opus': 200000, 'qwen/qwen3.5-9b': 32768, 'qwen/qwen3.5-122b-a10b': 32768, 'qwen/qwen3.5-397b-a17b': 32768}; \
m = '$(OVERFLOW_MODEL)'; \
w = CTX.get(m, CTX.get(m.split(':')[0], 0)); \
_ = sys.stderr.write('ERROR: unknown context window for \"' + m + '\". Add it to CTX or use a known model.\n') if not w else None; \
w = w or sys.exit(1); \
h = json.load(open('chat_history.json')); \
c = sum(len(msg['content']) for msg in h); \
t = c // 4; \
r = (w // t) + 2; \
b = h * r; \
b = b if b[-1]['role'] == 'assistant' else b[:-1]; \
sys.stderr.write('Model:     {}  (context window: {:,} tokens)\n'.format(m, w)); \
sys.stderr.write('Original:  {} messages, ~{:,} chars, ~{:,} tokens\n'.format(len(h), c, t)); \
sys.stderr.write('Overflow:  {} messages, ~{:,} tokens  ({}x repeated)\n\n'.format(len(b), t * r, r)); \
print(json.dumps(b)); \
" > "$$OVERFLOW_FILE"; \
	RESULT=$$(LLM_MODEL="$(OVERFLOW_MODEL)" \
		./$(BINARY_NAME) \
		--query "Продолжи историю." \
		--format-hint "Продолжи историю одним абзацем." \
		--history "$$OVERFLOW_FILE" \
		--show-cost $(DEBUG_FLAG) 2>&1); \
	echo "$$RESULT"; \
	rm -f "$$OVERFLOW_FILE"; \
	echo ""; \
	echo "--- Verification ---"; \
	FAIL=0; \
	if echo "$$RESULT" | grep -q "EXCEEDS\|CRITICAL"; then \
		echo "  ✓ pre-call: context overflow detected before API call"; \
	else \
		echo "  ✗ pre-call: CRITICAL/EXCEEDS warning not shown"; \
		FAIL=$$((FAIL+1)); \
	fi; \
	if echo "$$RESULT" | grep -qi "context.*length\|too many token\|maximum.*token\|context_length\|Error.*LLM"; then \
		echo "  ✓ API correctly rejected the oversized request"; \
	else \
		echo "  ? API response unexpected — check output above"; \
	fi; \
	echo ""; \
	if [ "$$FAIL" = "0" ]; then \
		echo "  Result: PASS — context overflow correctly detected and reported"; \
	else \
		echo "  Result: $$FAIL check(s) failed"; exit 1; \
	fi; \
	echo "=========================================="

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -f $(BATCH_OUTPUT)
	rm -f $(HISTORY_TEST_FILE) $(HISTORY_TEST_FILE:.json=.stats.json)
	rm -f $(TOKENS_HIST_FILE) $(TOKENS_HIST_FILE:.json=.stats.json)
	$(GO) clean -testcache

install-deps: ## Install and tidy dependencies
	$(GO) mod download
	$(GO) mod tidy

help: ## Show this help
	@echo "Available targets:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-28s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Build & test:"
	@echo "  make build                       # Build the application"
	@echo "  make test                        # Run unit tests"
	@echo "  make vet                         # Static analysis"
	@echo "  make test-coverage               # Generate coverage.html"
	@echo ""
	@echo "Integration tests:"
	@echo "  make run-integration             # Run with --show-cost (tokens + price always shown)"
	@echo "  make run-integration QUERY=\"...\"  # Run with inline query"
	@echo "  make run-integration-batch       # qwen models -> expert evaluation (QUERY_FILE=)"
	@echo "  make run-integration-batch QUERY=\"...\"  # batch with inline query"
	@echo "  DEBUG=1 make run-integration     # Any target: also print raw JSON request payload"
	@echo ""
	@echo "History integration tests:"
	@echo "  make run-history-basic           # Single call: verify file + 2 messages"
	@echo "  make run-history-accumulate      # 3 calls: verify 6 messages accumulated"
	@echo "  make run-history-disabled        # --history \"\": verify no file created"
	@echo "  make run-history-context         # 2-turn chat: model recalls prior context"
	@echo "  make run-history-all             # Run all 4 history tests with summary"
	@echo ""
	@echo "Token / cost demos (Day8):"
	@echo "  make run-tokens                  # Single call with --show-tokens breakdown"
	@echo "  make run-tokens-cost             # Single call with --show-cost (tokens + price)"
	@echo "  make run-tokens-session          # 3-turn session — cumulative stats"
	@echo "  QUERY=\"...\" make run-tokens-cost  # Custom query"
	@echo ""
	@echo "Context overflow test (Day8):"
	@echo "  make run-context-overflow                            # Overflow openai/gpt-4o-mini (128K)"
	@echo "  OVERFLOW_MODEL=qwen/qwen3.5-9b make run-context-overflow  # Overflow any known model"
	@echo ""
	@echo "Parameters:"
	@echo "  LLM_API_KEY                      # API key (required, masked in output)"
	@echo "  LLM_PROVIDER                     # openai | openrouter (default: openrouter)"
	@echo "  LLM_MODEL                        # Model name (default: openai/gpt-4o-mini)"
	@echo "  LLM_BASE_URL                     # API URL (default: https://openrouter.ai/api/v1)"
	@echo "  QUERY                            # Inline query string (overrides QUERY_FILE)"
	@echo "  QUERY_FILE                       # Path to query file (default: query.txt)"
	@echo "  BATCH_OUTPUT                     # Output file for batch test (default: test_output.txt)"
	@echo "  SYSTEM_EXPERT_FILE               # System prompt for expert evaluation (default: system_expert.txt)"
	@echo "  EXPERT_MODEL                     # Model for expert evaluation (default: openai/gpt-oss-120b:free)"
	@echo "  HISTORY_TEST_FILE                # Temp file for history tests (default: /tmp/test_chat_history_day8.json)"
	@echo "  OVERFLOW_MODEL                   # Model for context overflow test (default: openai/gpt-4o-mini)"
	@echo "  TOKENS_HIST_FILE                 # Temp file for token demos (default: /tmp/token_demo_day8.json)"
	@echo ""
	@echo "Examples:"
	@echo "  export LLM_API_KEY=sk-*** && make run-history-all"
	@echo "  make run-history-context LLM_MODEL=openai/gpt-4o"
	@echo "  export LLM_API_KEY=sk-*** && make run-tokens-session"
	@echo "  QUERY=\"What is Go?\" make run-tokens-cost"

.DEFAULT_GOAL := help
