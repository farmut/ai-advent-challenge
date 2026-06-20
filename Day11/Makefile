.PHONY: test clean help check-env build \
        run-integration run-integration-batch \
        run-history-basic run-history-accumulate run-history-disabled run-history-context \
        run-history-all \
        run-tokens run-tokens-cost run-tokens-session \
        run-context-overflow \
        run-summary-test run-critic-test \
        run-sliding-window run-sticky-facts run-branching \
        run-lion-book-test \
        run-memory-demo run-memory-recall run-memory-layers run-memory

BINARY_NAME := ai-adv-agent-day11
GO := go
GOFLAGS :=
LDFLAGS := -s -w
SHELL := /bin/bash
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
SYSTEM_CRITIC_FILE ?= system_critic.txt
CRITIC_LIMIT       ?= 4

HISTORY_TEST_FILE  ?= /tmp/test_chat_history_day11.json
SUMMARY_TEST_FILE  ?= /tmp/summary_test_day11.json
OVERFLOW_MODEL     ?= openai/gpt-4o-mini
TOKENS_HIST_FILE   ?= /tmp/token_demo_day11.json

# Day11: 3-layer memory files
MEMORY_STM_FILE    ?= /tmp/memory_stm_day11.json
MEMORY_WM_FILE     ?= /tmp/memory_wm_day11.json
MEMORY_LTM_FILE    ?= /tmp/memory_ltm_day11.json
MEMORY_UPDATE      ?=
MEMORY_UPDATE_FLAG  = $(if $(MEMORY_UPDATE),--memory-update,)

WINDOW_SIZE        ?= 5
BRANCH_HIST_FILE   ?= /tmp/branch_demo_day11.json
FACTS_HIST_FILE    ?= /tmp/facts_demo_day11.json

# Lion Book test — story scenario across 3 strategies
LION_WINDOW_SIZE   ?= 5
LION_SW_FILE       ?= /tmp/lion_sw_result.txt
LION_SF_FILE       ?= /tmp/lion_sf_result.txt
LION_BR_FILE       ?= /tmp/lion_br_result.txt

# Integration test parameters — override on the command line as needed:
#   STRATEGY=sliding-window  SUMMARY=1  HISTORY_FILE=my.json  SYSTEM_FILE=system.txt
STRATEGY      ?=
SUMMARY       ?=
HISTORY_FILE  ?= chat_history.json
HISTORY_LIMIT ?= 10
SYSTEM_FILE   ?=

STRATEGY_FLAG = $(if $(STRATEGY),--strategy $(STRATEGY),)
WINDOW_FLAG   = $(if $(STRATEGY),--window-size $(WINDOW_SIZE),)
SUMMARY_FLAG  = $(if $(SUMMARY),--summary,)

# Set DEBUG=1 to add --debug flag to any integration target.
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

run-integration: $(BINARY_NAME) check-env ## Run integration (QUERY=, QUERY_FILE=, STRATEGY=, SUMMARY=1, HISTORY_FILE=, HISTORY_LIMIT=, SYSTEM_FILE=, WINDOW_SIZE=)
	@echo "=== Integration test ==="
	@[ -n "$(STRATEGY)"    ] && echo "  Strategy:     $(STRATEGY)  (window-size=$(WINDOW_SIZE))" || true
	@[ -n "$(SUMMARY)"     ] && echo "  Summary:      enabled" || true
	@[ -n "$(SYSTEM_FILE)" ] && echo "  System file:  $(SYSTEM_FILE)" || true
	@echo "  History file: $(HISTORY_FILE)  limit=$(HISTORY_LIMIT)"
	@if [ -n "$(QUERY)" ]; then \
		QC="$(QUERY)"; echo "  Query (inline): $$QC"; \
	else \
		if [ ! -f "$(QUERY_FILE)" ]; then \
			echo "Error: query file '$(QUERY_FILE)' not found"; \
			echo "Run: make run-integration QUERY_FILE=path/to/query.txt"; \
			echo "  or: make run-integration QUERY=\"your query\""; \
			exit 1; \
		fi; \
		QC="$$(cat "$(QUERY_FILE)")"; echo "  Query file:   $(QUERY_FILE)"; \
	fi; \
	if [ -n "$(SYSTEM_FILE)" ]; then \
		if [ ! -f "$(SYSTEM_FILE)" ]; then \
			echo "Error: SYSTEM_FILE '$(SYSTEM_FILE)' not found"; exit 1; \
		fi; \
		SYS="$$(cat "$(SYSTEM_FILE)")"; \
		./$(BINARY_NAME) --query "$$QC" \
			--history "$(HISTORY_FILE)" --history-limit $(HISTORY_LIMIT) \
			$(STRATEGY_FLAG) $(WINDOW_FLAG) $(SUMMARY_FLAG) \
			--system "$$SYS" --show-cost $(DEBUG_FLAG); \
	else \
		./$(BINARY_NAME) --query "$$QC" \
			--history "$(HISTORY_FILE)" --history-limit $(HISTORY_LIMIT) \
			$(STRATEGY_FLAG) $(WINDOW_FLAG) $(SUMMARY_FLAG) \
			--show-cost $(DEBUG_FLAG); \
	fi

run-integration-batch: $(BINARY_NAME) check-env ## Batch integration: qwen models -> expert evaluation
	@if [ -z "$(QUERY)" ] && [ ! -f "$(QUERY_FILE)" ]; then \
		echo "Error: neither QUERY= nor query file '$(QUERY_FILE)' provided"; exit 1; \
	fi
	@if [ ! -f "$(SYSTEM_EXPERT_FILE)" ]; then \
		echo "Error: system expert file '$(SYSTEM_EXPERT_FILE)' not found"; exit 1; \
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
	@for model in $(QWEN_MODELS); do \
		echo "--- [Model: $$model] ---"; \
		echo "=== Model: $$model ===" >> $(BATCH_OUTPUT); \
		ANS_LOG=$$(mktemp); STATS_LOG=$$(mktemp); \
		if [ -n "$(QUERY)" ]; then QC="$(QUERY)"; else QC="$$(cat $(QUERY_FILE))"; fi; \
		LLM_MODEL="$$model" ./$(BINARY_NAME) --query "$$QC" --show-cost $(DEBUG_FLAG) >"$$ANS_LOG" 2>"$$STATS_LOG"; \
		cat "$$STATS_LOG" >&2; \
		grep '^\[tokens\]\|^\[cost\]' "$$STATS_LOG" >> $(BATCH_OUTPUT); \
		cat "$$ANS_LOG" >> $(BATCH_OUTPUT); \
		rm -f "$$STATS_LOG" "$$ANS_LOG"; \
		printf '\n#####\n\n' >> $(BATCH_OUTPUT); \
	done
	@echo "--- [Final evaluation: $(EXPERT_MODEL)] ---"
	@SYSTEM_CONTENT="$$(cat $(SYSTEM_EXPERT_FILE))"; \
	LLM_MODEL=$(EXPERT_MODEL) ./$(BINARY_NAME) --query "$$(cat $(BATCH_OUTPUT))" --system "$$SYSTEM_CONTENT" --show-cost $(DEBUG_FLAG)
	@echo ""
	@echo "=========================================="
	@echo "Batch integration test completed. Results: $(BATCH_OUTPUT)"
	@echo "=========================================="

# ---------------------------------------------------------------------------
# History integration tests
# ---------------------------------------------------------------------------

run-history-basic: $(BINARY_NAME) check-env ## [History] Single call: file created with 2 messages
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json" "$${HIST_FILE%.json}.summary.txt"; \
	echo "--- [History] Test 1: file created on first call ---"; \
	./$(BINARY_NAME) --query "Say the single word OK" --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))" 2>/dev/null || echo 0); \
	ROLES=$$(python3 -c "import json; print(' '.join(m['role'] for m in json.load(open('$$HIST_FILE'))))" 2>/dev/null || echo "?"); \
	if [ "$$COUNT" = "2" ] && [ "$$ROLES" = "user assistant" ]; then \
		echo "  ✓ PASS: file created with 2 messages — roles: [$$ROLES]"; \
	else \
		echo "  ✗ FAIL: expected 2 messages [user assistant], got $$COUNT [$$ROLES]"; exit 1; \
	fi

run-history-accumulate: $(BINARY_NAME) check-env ## [History] 3 calls accumulate messages
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json" "$${HIST_FILE%.json}.summary.txt"; \
	echo "--- [History] Test 2: history accumulates across calls ---"; \
	./$(BINARY_NAME) --query "First question: say ONE"   --history "$$HIST_FILE" --history-limit 10 --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Second question: say TWO"  --history "$$HIST_FILE" --history-limit 10 --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Third question: say THREE" --history "$$HIST_FILE" --history-limit 10 --show-cost $(DEBUG_FLAG) > /dev/null; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))" 2>/dev/null || echo 0); \
	if [ "$$COUNT" = "6" ]; then \
		echo "  ✓ PASS: 6 messages after 3 calls"; \
	else \
		echo "  ✗ FAIL: expected 6 messages, got $$COUNT"; exit 1; \
	fi

run-history-disabled: $(BINARY_NAME) check-env ## [History] --history "" does not create a file
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	rm -f "$$HIST_FILE"; \
	echo "--- [History] Test 3: history disabled with --history \"\" ---"; \
	./$(BINARY_NAME) --query "Say OK" --history "" --show-cost $(DEBUG_FLAG) > /dev/null; \
	if [ -f "$$HIST_FILE" ]; then \
		echo "  ✗ FAIL: history file was created despite --history being empty"; exit 1; \
	fi; \
	echo "  ✓ PASS: no history file created when --history is empty"

run-history-context: $(BINARY_NAME) check-env ## [History] Model recalls prior context
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	RESP_FILE=$$(mktemp); \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json" "$${HIST_FILE%.json}.summary.txt"; \
	echo "--- [History] Test 4: model sees prior context ---"; \
	./$(BINARY_NAME) --query "Remember the number 42. Reply with: OK." \
		--history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "What number did I ask you to remember?" \
		--format-hint "Reply with the number only, no other text." \
		--history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > "$$RESP_FILE"; \
	RESPONSE=$$(cat "$$RESP_FILE"); rm -f "$$RESP_FILE"; \
	echo "  Model response: $$RESPONSE"; \
	if echo "$$RESPONSE" | grep -q "42"; then \
		echo "  ✓ PASS: model recalled 42 from history"; \
	else \
		echo "  ✗ FAIL: model did not recall 42"; exit 1; \
	fi

run-history-all: $(BINARY_NAME) check-env ## [History] Run all 4 history integration tests
	@echo "=========================================="; echo "  History Integration Tests (Day11)"; echo "=========================================="; echo ""
	@HIST_FILE="$(HISTORY_TEST_FILE)"; FAILED=0; \
	\
	echo "--- [1/4] File created on first call ---"; \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json"; \
	./$(BINARY_NAME) --query "Say the single word OK" --history "$$HIST_FILE" --show-cost $(DEBUG_FLAG) > /dev/null; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))" 2>/dev/null); \
	ROLES=$$(python3 -c "import json; print(' '.join(m['role'] for m in json.load(open('$$HIST_FILE'))))" 2>/dev/null); \
	if [ "$$COUNT" = "2" ] && [ "$$ROLES" = "user assistant" ]; then \
		echo "  ✓ PASS"; else echo "  ✗ FAIL: $$COUNT [$$ROLES]"; FAILED=$$((FAILED+1)); fi; \
	\
	echo ""; echo "--- [2/4] History accumulates ---"; \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json"; \
	./$(BINARY_NAME) --query "Say ONE"   --history "$$HIST_FILE" --history-limit 10 $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Say TWO"   --history "$$HIST_FILE" --history-limit 10 $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "Say THREE" --history "$$HIST_FILE" --history-limit 10 $(DEBUG_FLAG) > /dev/null; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))" 2>/dev/null); \
	if [ "$$COUNT" = "6" ]; then echo "  ✓ PASS"; else echo "  ✗ FAIL: $$COUNT messages"; FAILED=$$((FAILED+1)); fi; \
	\
	echo ""; echo "--- [3/4] History disabled ---"; \
	rm -f "$$HIST_FILE"; \
	./$(BINARY_NAME) --query "Say OK" --history "" $(DEBUG_FLAG) > /dev/null; \
	if [ ! -f "$$HIST_FILE" ]; then echo "  ✓ PASS"; else echo "  ✗ FAIL"; FAILED=$$((FAILED+1)); fi; \
	\
	echo ""; echo "--- [4/4] Model recalls prior context ---"; \
	RESP_FILE=$$(mktemp); \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json"; \
	./$(BINARY_NAME) --query "Remember the number 42. Reply: OK." --history "$$HIST_FILE" $(DEBUG_FLAG) > /dev/null; \
	./$(BINARY_NAME) --query "What number?" --format-hint "Number only." --history "$$HIST_FILE" $(DEBUG_FLAG) > "$$RESP_FILE"; \
	RESPONSE=$$(cat "$$RESP_FILE"); rm -f "$$RESP_FILE"; \
	if echo "$$RESPONSE" | grep -q "42"; then echo "  ✓ PASS: recalled 42"; \
	else echo "  ✗ FAIL: got $$RESPONSE"; FAILED=$$((FAILED+1)); fi; \
	\
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then echo "  Result: 4/4 PASSED"; \
	else echo "  Result: $$FAILED/4 FAILED"; exit 1; fi; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Summary test (optional --summary flag)
# ---------------------------------------------------------------------------

run-summary-test: $(BINARY_NAME) check-env ## [Summary] --summary flag triggers auto-summarization
	@HIST_FILE="$(SUMMARY_TEST_FILE)"; \
	SUM_FILE="$${HIST_FILE%.json}.summary.txt"; \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json" "$$SUM_FILE"; \
	echo "=========================================="; \
	echo "  Summary Auto-Trim Test (Day11)"; \
	echo "  --summary enabled, history-limit: 4"; \
	echo "=========================================="; echo ""; \
	echo "--- Turn 1 ---"; \
	./$(BINARY_NAME) --query "Remember: secret code is ALPHA-7. Reply: OK." \
		--history "$$HIST_FILE" --history-limit 4 --summary $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 2 ---"; \
	./$(BINARY_NAME) --query "Remember: mission location is Berlin. Reply: OK." \
		--history "$$HIST_FILE" --history-limit 4 --summary $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 3: pushes over limit, triggers summary ---"; \
	./$(BINARY_NAME) --query "Remember: contact name is Viktor. Reply: OK." \
		--history "$$HIST_FILE" --history-limit 4 --summary $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	if [ -f "$$SUM_FILE" ]; then \
		echo "  ✓ Summary file created"; \
		echo "  --- Summary ---"; cat "$$SUM_FILE"; echo "  ---------------"; \
	else \
		echo "  ✗ FAIL: summary file NOT created"; exit 1; \
	fi; \
	HIST_COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))" 2>/dev/null || echo 0); \
	echo "  History kept: $$HIST_COUNT (limit: 4)"; \
	if [ "$$HIST_COUNT" -le 4 ]; then echo "  ✓ History correctly trimmed"; \
	else echo "  ✗ FAIL: $$HIST_COUNT > 4"; exit 1; fi; \
	echo ""; echo "--- Turn 4: recall fact from summarized turn 1 ---"; \
	RESP_FILE=$$(mktemp); \
	./$(BINARY_NAME) --query "What was the secret code?" \
		--format-hint "Reply with the code only." \
		--history "$$HIST_FILE" --history-limit 4 --summary $(DEBUG_FLAG) > "$$RESP_FILE"; \
	RESPONSE=$$(cat "$$RESP_FILE"); rm -f "$$RESP_FILE"; \
	echo "  Model response: $$RESPONSE"; \
	if echo "$$RESPONSE" | grep -qi "ALPHA"; then \
		echo "  ✓ PASS: model recalled secret code from summary"; \
	else \
		echo "  ✗ FAIL: model did not recall secret code (response: $$RESPONSE)"; exit 1; \
	fi; \
	echo ""; echo "=========================================="; \
	echo "  Result: PASS"; echo "=========================================="

# ---------------------------------------------------------------------------
# Critic test
# ---------------------------------------------------------------------------

run-critic-test: $(BINARY_NAME) check-env ## [Critic] AI writer-critic compares full vs. summarized history
	@if [ ! -f "chat_history.json" ]; then \
		echo "Error: chat_history.json not found"; exit 1; \
	fi
	@if [ ! -f "$(SYSTEM_CRITIC_FILE)" ]; then \
		echo "Error: $(SYSTEM_CRITIC_FILE) not found"; exit 1; \
	fi
	@HIST_A=$$(mktemp /tmp/critic_full_XXXXXX.json); \
	HIST_B=$$(mktemp /tmp/critic_slim_XXXXXX.json); \
	RESP_A=$$(mktemp); RESP_B=$$(mktemp); \
	STDERR_A=$$(mktemp); STDERR_B=$$(mktemp); STDERR_WU=$$(mktemp); \
	CRITIC_PROMPT_FILE=$$(mktemp); \
	SUM_B="$${HIST_B%.json}.summary.txt"; \
	cp chat_history.json "$$HIST_A"; cp chat_history.json "$$HIST_B"; \
	ORIG_COUNT=$$(python3 -c "import json; print(len(json.load(open('chat_history.json'))))" 2>/dev/null || echo "?"); \
	WARMUP_Q="Какой самый известный театр Москвы, связанный с именем Высоцкого? Ответь одним предложением."; \
	COMPARE_Q="Перечисли все рестораны, парки, музеи и галереи Москвы, которые мы упоминали в разговоре, с конкретными деталями о каждом."; \
	echo "=========================================="; \
	echo "  Critic Test: Full vs. Summarized history"; \
	echo "  Source history: chat_history.json ($$ORIG_COUNT messages)"; \
	echo "  history-limit (path B): $(CRITIC_LIMIT)"; \
	echo "=========================================="; echo ""; \
	echo "--- [Path A] Полная история ---"; \
	./$(BINARY_NAME) --query "$$WARMUP_Q" --history "$$HIST_A" --history-limit 0 \
		--show-cost $(DEBUG_FLAG) >/dev/null 2>"$$STDERR_WU"; cat "$$STDERR_WU" >&2; \
	./$(BINARY_NAME) --query "$$COMPARE_Q" --history "$$HIST_A" --history-limit 0 \
		--show-cost $(DEBUG_FLAG) >"$$RESP_A" 2>"$$STDERR_A"; cat "$$STDERR_A" >&2; \
	TOKENS_A=$$(grep '^\[tokens\]' "$$STDERR_A" | head -1); \
	CONTEXT_A=$$(grep '^\[context\]' "$$STDERR_A" | head -1); \
	COST_A=$$(grep '^\[cost\]' "$$STDERR_A" | head -1); \
	HIST_A_COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_A'))))" 2>/dev/null || echo "?"); \
	echo ""; echo "~~~ Ответ A ($$HIST_A_COUNT сообщений) ~~~"; cat "$$RESP_A"; echo ""; \
	echo "--- [Path B] Summary + усечённая история ---"; \
	./$(BINARY_NAME) --query "$$WARMUP_Q" --history "$$HIST_B" --history-limit $(CRITIC_LIMIT) \
		--summary --show-cost $(DEBUG_FLAG) >/dev/null 2>"$$STDERR_WU"; cat "$$STDERR_WU" >&2; \
	echo ""; \
	if [ -f "$$SUM_B" ]; then \
		echo "  [summary → $$SUM_B]:"; echo "  ──────────────────"; cat "$$SUM_B"; echo "  ──────────────────"; \
	fi; echo ""; \
	./$(BINARY_NAME) --query "$$COMPARE_Q" --history "$$HIST_B" --history-limit $(CRITIC_LIMIT) \
		--summary --show-cost $(DEBUG_FLAG) >"$$RESP_B" 2>"$$STDERR_B"; cat "$$STDERR_B" >&2; \
	TOKENS_B=$$(grep '^\[tokens\]' "$$STDERR_B" | head -1); \
	CONTEXT_B=$$(grep '^\[context\]' "$$STDERR_B" | head -1); \
	COST_B=$$(grep '^\[cost\]' "$$STDERR_B" | head -1); \
	HIST_B_COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_B'))))" 2>/dev/null || echo "?"); \
	echo ""; echo "~~~ Ответ B ($$HIST_B_COUNT сообщений + summary) ~~~"; cat "$$RESP_B"; echo ""; \
	echo "=========================================="; echo "  Оценка AI-критика"; echo "=========================================="; echo ""; \
	{ printf 'ВОПРОС:\n%s\n\n' "$$COMPARE_Q"; \
	  printf '=== ОТВЕТ A (полная история: %s сообщений) ===\n\n' "$$HIST_A_COUNT"; cat "$$RESP_A"; \
	  printf '\n--- Статистика A ---\n%s\n%s\n%s\n\n' "$$TOKENS_A" "$$CONTEXT_A" "$$COST_A"; \
	  printf '=== ОТВЕТ B (history-limit=$(CRITIC_LIMIT): %s сообщений + summary) ===\n\n' "$$HIST_B_COUNT"; cat "$$RESP_B"; \
	  printf '\n--- Статистика B ---\n%s\n%s\n%s\n' "$$TOKENS_B" "$$CONTEXT_B" "$$COST_B"; \
	} > "$$CRITIC_PROMPT_FILE"; \
	SYSTEM_CRITIC=$$(cat "$(SYSTEM_CRITIC_FILE)"); \
	./$(BINARY_NAME) --query "$$(cat $$CRITIC_PROMPT_FILE)" --system "$$SYSTEM_CRITIC" --history "" \
		--format-hint "Используй заголовки: ## 1. Полнота, ## 2. Качество, ## 3. Токены. В конце ## Общий вывод." \
		$(DEBUG_FLAG); \
	echo ""; echo "=========================================="; echo "  Тест завершён"; echo "=========================================="; \
	rm -f "$$HIST_A" "$$HIST_B" "$$RESP_A" "$$RESP_B" "$$STDERR_A" "$$STDERR_B" "$$STDERR_WU" "$$CRITIC_PROMPT_FILE" \
		"$${HIST_A%.json}.stats.json" "$${HIST_A%.json}.summary.txt" \
		"$${HIST_B%.json}.stats.json" "$$SUM_B"

# ---------------------------------------------------------------------------
# Strategy: Sliding Window
# ---------------------------------------------------------------------------

run-sliding-window: $(BINARY_NAME) check-env ## [Strategy] Sliding window: keep only last WINDOW_SIZE messages
	@HIST_FILE="$(HISTORY_TEST_FILE)"; \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Sliding Window Demo (window-size=$(WINDOW_SIZE))"; \
	echo "=========================================="; echo ""; \
	echo "--- Turn 1: establish fact A ---"; \
	./$(BINARY_NAME) --query "Remember fact A: the secret word is BANANA. Reply: OK." \
		--history "$$HIST_FILE" --strategy sliding-window --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 2 ---"; \
	./$(BINARY_NAME) --query "Say: message 2." \
		--history "$$HIST_FILE" --strategy sliding-window --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 3 ---"; \
	./$(BINARY_NAME) --query "Say: message 3." \
		--history "$$HIST_FILE" --strategy sliding-window --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 4 ---"; \
	./$(BINARY_NAME) --query "Say: message 4." \
		--history "$$HIST_FILE" --strategy sliding-window --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST_FILE'))))" 2>/dev/null || echo "?"); \
	echo ""; echo "  Messages in history file after 4 turns: $$COUNT (window-size=$(WINDOW_SIZE))"; \
	if [ "$$COUNT" -le "$(WINDOW_SIZE)" ]; then \
		echo "  ✓ History correctly bounded to window size"; \
	else \
		echo "  ✗ FAIL: $$COUNT messages exceeds window-size=$(WINDOW_SIZE)"; exit 1; \
	fi; \
	echo ""; \
	echo "--- Turn 5: ask about fact A (may be forgotten!) ---"; \
	RESP_FILE=$$(mktemp); \
	./$(BINARY_NAME) --query "What was the secret word I mentioned in my first message?" \
		--format-hint "Reply in one sentence." \
		--history "$$HIST_FILE" --strategy sliding-window --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > "$$RESP_FILE"; \
	echo "  Response: $$(cat $$RESP_FILE)"; \
	rm -f "$$RESP_FILE"; \
	echo "  (If the model doesn't recall BANANA, that is expected — fact A was pushed out of the window)"; \
	echo ""; echo "=========================================="; \
	echo "  Sliding Window Demo Complete"; echo "=========================================="

# ---------------------------------------------------------------------------
# Strategy: Sticky Facts
# ---------------------------------------------------------------------------

run-sticky-facts: $(BINARY_NAME) check-env ## [Strategy] Sticky facts: key-value memory survives window rotation
	@HIST_FILE="$(FACTS_HIST_FILE)"; \
	FACTS_FILE="$${HIST_FILE%.json}.facts.json"; \
	rm -f "$$HIST_FILE" "$${HIST_FILE%.json}.stats.json" "$$FACTS_FILE"; \
	echo "=========================================="; \
	echo "  Sticky Facts Demo (window-size=$(WINDOW_SIZE))"; \
	echo "=========================================="; echo ""; \
	echo "--- Turn 1: establish goal ---"; \
	./$(BINARY_NAME) --query "I am building a REST API in Go. My name is Alice. Reply: OK." \
		--history "$$HIST_FILE" --strategy sticky-facts --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 2: add constraint ---"; \
	./$(BINARY_NAME) --query "The API must use JWT for auth. No external frameworks. Reply: OK." \
		--history "$$HIST_FILE" --strategy sticky-facts --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 3 ---"; \
	./$(BINARY_NAME) --query "Say: message 3." \
		--history "$$HIST_FILE" --strategy sticky-facts --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	echo "--- Turn 4 ---"; \
	./$(BINARY_NAME) --query "Say: message 4." \
		--history "$$HIST_FILE" --strategy sticky-facts --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	if [ -f "$$FACTS_FILE" ]; then \
		echo "  ✓ Facts file created: $$FACTS_FILE"; \
		echo "  --- Facts store ---"; cat "$$FACTS_FILE"; echo "  -------------------"; \
	else \
		echo "  ✗ FAIL: facts file NOT created"; exit 1; \
	fi; \
	echo ""; \
	echo "--- Turn 5: recall from facts (not in recent history) ---"; \
	RESP_FILE=$$(mktemp); \
	./$(BINARY_NAME) --query "What is my name and what am I building?" \
		--format-hint "Reply in one sentence." \
		--history "$$HIST_FILE" --strategy sticky-facts --window-size $(WINDOW_SIZE) $(DEBUG_FLAG) > "$$RESP_FILE"; \
	echo "  Response: $$(cat $$RESP_FILE)"; \
	RESPONSE=$$(cat "$$RESP_FILE"); rm -f "$$RESP_FILE"; \
	if echo "$$RESPONSE" | grep -qi "alice"; then \
		echo "  ✓ PASS: model recalled name from facts store"; \
	else \
		echo "  ? Model may not have recalled Alice — check facts file contents above"; \
	fi; \
	echo ""; echo "=========================================="; \
	echo "  Sticky Facts Demo Complete"; echo "=========================================="

# ---------------------------------------------------------------------------
# Strategy: Branching
# ---------------------------------------------------------------------------

run-branching: $(BINARY_NAME) check-env ## [Branching] Full workflow: checkpoint -> 2 branches -> switch
	@HIST_FILE="$(BRANCH_HIST_FILE)"; \
	BS_FILE="$${HIST_FILE%.json}.branch-state.json"; \
	rm -f "$$HIST_FILE" "$$BS_FILE" \
		"$${HIST_FILE%.json}.branch-plan-a.json" "$${HIST_FILE%.json}.branch-plan-b.json" \
		"$${HIST_FILE%.json}.stats.json" \
		"$${HIST_FILE%.json}.branch-plan-a.stats.json" \
		"$${HIST_FILE%.json}.branch-plan-b.stats.json"; \
	echo "=========================================="; \
	echo "  Branching Demo"; \
	echo "=========================================="; echo ""; \
	\
	echo "--- [main] Turn 1: shared context ---"; \
	./$(BINARY_NAME) --query "We are designing a CLI tool. It processes text files. Reply: OK." \
		--history "$$HIST_FILE" --strategy branching $(DEBUG_FLAG) > /dev/null; \
	echo "--- [main] Turn 2 ---"; \
	./$(BINARY_NAME) --query "The tool should support piping from stdin. Reply: OK." \
		--history "$$HIST_FILE" --strategy branching $(DEBUG_FLAG) > /dev/null; \
	\
	echo ""; echo "--- Save checkpoint 'cp1' ---"; \
	./$(BINARY_NAME) --history "$$HIST_FILE" --strategy branching --checkpoint cp1 2>&1; \
	\
	echo ""; echo "--- Create branch 'plan-a' from cp1 ---"; \
	./$(BINARY_NAME) --history "$$HIST_FILE" --strategy branching --branch-new plan-a --from-checkpoint cp1 2>&1; \
	\
	echo ""; echo "--- [plan-a] Continue: explore approach A ---"; \
	./$(BINARY_NAME) --query "For plan A: use a streaming JSON format for output. What are the pros?" \
		--format-hint "Reply in 2-3 sentences." \
		--history "$$HIST_FILE" --strategy branching $(DEBUG_FLAG); \
	\
	echo ""; echo "--- Switch back to 'main', create branch 'plan-b' from cp1 ---"; \
	./$(BINARY_NAME) --history "$$HIST_FILE" --strategy branching --branch-new plan-b --from-checkpoint cp1 2>&1; \
	\
	echo ""; echo "--- [plan-b] Continue: explore approach B ---"; \
	./$(BINARY_NAME) --query "For plan B: use a simple CSV format for output. What are the pros?" \
		--format-hint "Reply in 2-3 sentences." \
		--history "$$HIST_FILE" --strategy branching $(DEBUG_FLAG); \
	\
	echo ""; echo "--- Branch list ---"; \
	./$(BINARY_NAME) --history "$$HIST_FILE" --strategy branching --branch-list; \
	\
	echo ""; echo "--- Switch back to plan-a and verify context ---"; \
	./$(BINARY_NAME) --history "$$HIST_FILE" --strategy branching --branch-switch plan-a 2>&1; \
	RESP_FILE=$$(mktemp); \
	./$(BINARY_NAME) --query "What output format did we just discuss for this branch?" \
		--format-hint "One sentence." \
		--history "$$HIST_FILE" --strategy branching $(DEBUG_FLAG) > "$$RESP_FILE"; \
	echo "  plan-a response: $$(cat $$RESP_FILE)"; rm -f "$$RESP_FILE"; \
	\
	echo ""; echo "--- Switch to plan-b and verify independent context ---"; \
	./$(BINARY_NAME) --history "$$HIST_FILE" --strategy branching --branch-switch plan-b 2>&1; \
	RESP_FILE=$$(mktemp); \
	./$(BINARY_NAME) --query "What output format did we just discuss for this branch?" \
		--format-hint "One sentence." \
		--history "$$HIST_FILE" --strategy branching $(DEBUG_FLAG) > "$$RESP_FILE"; \
	echo "  plan-b response: $$(cat $$RESP_FILE)"; rm -f "$$RESP_FILE"; \
	\
	echo ""; echo "=========================================="; \
	echo "  Branching Demo Complete"; \
	echo "  plan-a and plan-b have independent conversation histories."; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Token / cost demos
# ---------------------------------------------------------------------------

run-tokens: $(BINARY_NAME) check-env ## Show token breakdown for a single call
	@echo "=== Token breakdown demo ==="
	@if [ -n "$(QUERY)" ]; then \
		./$(BINARY_NAME) --query "$(QUERY)" --history "$(TOKENS_HIST_FILE)" --show-tokens; \
	else \
		./$(BINARY_NAME) --query "Explain what a token is in the context of LLMs in one sentence." \
			--history "$(TOKENS_HIST_FILE)" --show-tokens; \
	fi

run-tokens-cost: $(BINARY_NAME) check-env ## Show token breakdown + estimated cost
	@echo "=== Token + cost demo ==="
	@if [ -n "$(QUERY)" ]; then \
		./$(BINARY_NAME) --query "$(QUERY)" --history "$(TOKENS_HIST_FILE)" --show-cost; \
	else \
		./$(BINARY_NAME) --query "What is the capital of France? One word." \
			--history "$(TOKENS_HIST_FILE)" --show-cost; \
	fi

run-tokens-session: $(BINARY_NAME) check-env ## Run 3 turns and show cumulative session stats
	@echo "=== Session token accumulation demo (3 calls) ==="
	@rm -f "$(TOKENS_HIST_FILE)" "$(TOKENS_HIST_FILE:.json=.stats.json)"
	@echo "--- Turn 1 ---"
	@./$(BINARY_NAME) --query "Remember the number 7. Reply: OK." \
		--history "$(TOKENS_HIST_FILE)" --show-cost > /dev/null
	@echo "--- Turn 2 ---"
	@./$(BINARY_NAME) --query "Multiply the number you remembered by 6. Result only." \
		--history "$(TOKENS_HIST_FILE)" --show-cost > /dev/null
	@echo "--- Turn 3 ---"
	@./$(BINARY_NAME) --query "What was the original number?" \
		--history "$(TOKENS_HIST_FILE)" --show-cost > /dev/null
	@echo ""
	@echo "Session stats file: $(TOKENS_HIST_FILE:.json=.stats.json)"
	@if [ -f "$(TOKENS_HIST_FILE:.json=.stats.json)" ]; then cat "$(TOKENS_HIST_FILE:.json=.stats.json)"; fi

run-context-overflow: $(BINARY_NAME) check-env ## [Context] Overflow model context using chat_history.json
	@echo "=========================================="
	@echo "  Context Overflow Test"
	@echo "  Model:  $(OVERFLOW_MODEL)"
	@echo "=========================================="
	@OVERFLOW_FILE=$$(mktemp /tmp/overflow_XXXXXX.json); \
	python3 -c "\
import json, sys; \
CTX = {'gpt-4o': 128000, 'gpt-4o-mini': 128000, 'gpt-4-turbo': 128000, 'gpt-3.5-turbo': 16385, \
       'openai/gpt-4o': 128000, 'openai/gpt-4o-mini': 128000, 'qwen/qwen3.5-9b': 32768}; \
m = '$(OVERFLOW_MODEL)'; w = CTX.get(m, CTX.get(m.split(':')[0], 0)); \
_ = sys.stderr.write('ERROR: unknown context window for \"' + m + '\"\n') if not w else None; \
w = w or sys.exit(1); \
h = json.load(open('chat_history.json')); \
c = sum(len(msg['content']) for msg in h); t = c // 4; r = (w // t) + 2; b = h * r; \
b = b if b[-1]['role'] == 'assistant' else b[:-1]; \
sys.stderr.write('Overflow: {} messages, ~{} tokens\n'.format(len(b), t * r)); \
print(json.dumps(b)); \
" > "$$OVERFLOW_FILE"; \
	RESULT=$$(LLM_MODEL="$(OVERFLOW_MODEL)" \
		./$(BINARY_NAME) --query "Продолжи историю." \
		--format-hint "Продолжи историю одним абзацем." \
		--history "$$OVERFLOW_FILE" --show-cost $(DEBUG_FLAG) 2>&1); \
	echo "$$RESULT"; rm -f "$$OVERFLOW_FILE"; \
	echo ""; echo "--- Verification ---"; \
	if echo "$$RESULT" | grep -q "EXCEEDS\|CRITICAL"; then \
		echo "  ✓ pre-call: context overflow detected"; \
	else \
		echo "  ✗ CRITICAL/EXCEEDS warning not shown"; \
	fi; echo "=========================================="

# ---------------------------------------------------------------------------
# Lion Book: 10-turn story scenario across all 3 strategies + AI critic
# ---------------------------------------------------------------------------
# Calls: 10 (sliding-window) + 20 (sticky-facts: 10 main + 10 facts) + 10 (branching) + 1 (critic) = ~41 LLM calls
# Override: LION_WINDOW_SIZE=3  LION_SW_FILE=...  DEBUG=1

run-lion-book-test: $(BINARY_NAME) check-env ## [Lion Book] 10-turn story × 3 strategies + AI-critic comparison
	@declare -a QUERIES=( \
		"Напишем детскую книгу о львенке и его путешествии по саванне. Напиши мне первую главу. Глава должна быть не больше 100 слов" \
		"Напиши мне вторую главу." \
		"Напиши мне третью главу." \
		"Напиши мне четвертую главу." \
		"Напиши мне пятую главу." \
		"Напиши мне шестую главу" \
		"Напиши мне седьмую главу." \
		"Напиши мне восьмую главу." \
		"Напиши мне девятую главу." \
		"Расскажи мне что было во второй главе книги" \
	); \
	BIN="./$(BINARY_NAME)"; \
	WIN=$(LION_WINDOW_SIZE); \
	SW_HIST=$$(mktemp /tmp/lion_sw_XXXXXX.json); \
	SF_HIST=$$(mktemp /tmp/lion_sf_XXXXXX.json); \
	BR_HIST=$$(mktemp /tmp/lion_br_XXXXXX.json); \
	CRITIC_TMP=$$(mktemp /tmp/lion_critic_XXXXXX.txt); \
	SW_OUT="$(LION_SW_FILE)"; \
	SF_OUT="$(LION_SF_FILE)"; \
	BR_OUT="$(LION_BR_FILE)"; \
	\
	echo "=========================================="; \
	echo "  Lion Book — Strategy Comparison Test"; \
	echo "  Turns: $${#QUERIES[@]}  |  window-size: $$WIN"; \
	echo "  Model: $(LLM_MODEL)"; \
	echo "  ~41 LLM calls total (incl. facts updates)"; \
	echo "=========================================="; \
	echo ""; \
	\
	run_scenario() { \
		local STRAT="$$1" HIST="$$2" OUT="$$3"; \
		rm -f "$$HIST" "$${HIST%.json}.stats.json" \
			"$${HIST%.json}.facts.json" "$${HIST%.json}.branch-state.json"; \
		printf "=== Стратегия: %s | window-size=%s | model=$(LLM_MODEL) ===\n\n" \
			"$$STRAT" "$$WIN" > "$$OUT"; \
		for i in "$${!QUERIES[@]}"; do \
			local TURN=$$((i+1)); \
			local Q="$${QUERIES[$$i]}"; \
			printf "  [%s] Ход %d/%d...\n" "$$STRAT" "$$TURN" "$${#QUERIES[@]}"; \
			local RESP_TMP=$$(mktemp); \
			local STAT_TMP=$$(mktemp); \
			"$$BIN" --query "$$Q" \
				--history "$$HIST" \
				--strategy "$$STRAT" --window-size "$$WIN" \
				--history-limit 0 \
				--format-hint "Пиши на русском языке." \
				--show-cost $(DEBUG_FLAG) \
				>"$$RESP_TMP" 2>"$$STAT_TMP"; \
			echo "--- Ход $$TURN ---" >> "$$OUT"; \
			echo "Вопрос: $$Q" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			cat "$$RESP_TMP" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			grep -E '^\[(tokens|context|cost|facts)\]' "$$STAT_TMP" >> "$$OUT" || true; \
			echo "" >> "$$OUT"; \
			rm -f "$$RESP_TMP" "$$STAT_TMP"; \
		done; \
		echo "=== Конец: $$STRAT ===" >> "$$OUT"; \
	}; \
	\
	run_branching_scenario() { \
		local HIST="$$1" OUT="$$2"; \
		local RESP_TMP STAT_TMP Q TURN; \
		rm -f "$$HIST" "$${HIST%.json}.branch-state.json" \
			"$${HIST%.json}.branch-dark-side.json" \
			"$${HIST%.json}.branch-happy-end.json"; \
		echo "=== Стратегия: branching | Ветвление диалога | model=$(LLM_MODEL) ===" > "$$OUT"; \
		echo "" >> "$$OUT"; \
		echo "====== ОБЩИЙ СТВОЛ: главы 1-3 (ветка main) ======" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		for TURN in 1 2 3; do \
			case "$$TURN" in \
				1) Q="Напишем детскую книгу о львёнке и его путешествии по саванне. Напиши первую главу. Не больше 100 слов.";; \
				2) Q="Напиши вторую главу. Не больше 100 слов.";; \
				3) Q="Напиши третью главу. Не больше 100 слов.";; \
			esac; \
			printf "  [branching/main] Ход %d/3 (ствол)...\n" "$$TURN"; \
			RESP_TMP=$$(mktemp); STAT_TMP=$$(mktemp); \
			"$$BIN" --query "$$Q" --history "$$HIST" \
				--strategy branching --history-limit 0 \
				--format-hint "Пиши на русском языке." \
				--show-cost $(DEBUG_FLAG) \
				>"$$RESP_TMP" 2>"$$STAT_TMP"; \
			echo "--- Ход $$TURN [main] ---" >> "$$OUT"; \
			echo "Вопрос: $$Q" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			cat "$$RESP_TMP" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			grep -E '^\[(tokens|context|cost)\]' "$$STAT_TMP" >> "$$OUT" || true; \
			echo "" >> "$$OUT"; \
			rm -f "$$RESP_TMP" "$$STAT_TMP"; \
			if [ "$$TURN" = "1" ]; then \
				printf "  [checkpoint] after-ch1...\n"; \
				echo "[checkpoint after-ch1 — точка сразу после первой главы]" >> "$$OUT"; \
				"$$BIN" --history "$$HIST" --strategy branching \
					--checkpoint "after-ch1" 2>>"$$OUT"; \
				echo "" >> "$$OUT"; \
			fi; \
		done; \
		printf "  [checkpoint] after-ch3...\n"; \
		echo "[checkpoint after-ch3 — точка после третьей главы; от неё создадим две ветки]" >> "$$OUT"; \
		"$$BIN" --history "$$HIST" --strategy branching \
			--checkpoint "after-ch3" 2>>"$$OUT"; \
		echo "" >> "$$OUT"; \
		printf "  [branch-new] happy-end от after-ch3...\n"; \
		echo "[branch-new: создаём happy-end от checkpoint after-ch3]" >> "$$OUT"; \
		"$$BIN" --history "$$HIST" --strategy branching \
			--branch-new "happy-end" --from-checkpoint "after-ch3" 2>>"$$OUT"; \
		printf "  [branch-new] dark-side от after-ch3...\n"; \
		echo "[branch-new: создаём dark-side от checkpoint after-ch3 → current]" >> "$$OUT"; \
		"$$BIN" --history "$$HIST" --strategy branching \
			--branch-new "dark-side" --from-checkpoint "after-ch3" 2>>"$$OUT"; \
		echo "" >> "$$OUT"; \
		echo "====== ВЕТКА dark-side: тёмный поворот (главы 4-5) ======" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		for TURN in 4 5; do \
			case "$$TURN" in \
				4) Q="Напиши четвёртую главу. Драматический поворот: львёнок заблудился в незнакомой части саванны и попал в беду. Не больше 100 слов.";; \
				5) Q="Напиши пятую главу. Ситуация ухудшается — над саванной собирается гроза, опасность нарастает. Не больше 100 слов.";; \
			esac; \
			printf "  [branching/dark-side] Ход %d...\n" "$$TURN"; \
			RESP_TMP=$$(mktemp); STAT_TMP=$$(mktemp); \
			"$$BIN" --query "$$Q" --history "$$HIST" \
				--strategy branching --history-limit 0 \
				--format-hint "Пиши на русском языке. Тон — напряжённый, тревожный." \
				--show-cost $(DEBUG_FLAG) \
				>"$$RESP_TMP" 2>"$$STAT_TMP"; \
			echo "--- Ход $$TURN [dark-side] ---" >> "$$OUT"; \
			echo "Вопрос: $$Q" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			cat "$$RESP_TMP" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			grep -E '^\[(tokens|context|cost)\]' "$$STAT_TMP" >> "$$OUT" || true; \
			echo "" >> "$$OUT"; \
			rm -f "$$RESP_TMP" "$$STAT_TMP"; \
		done; \
		printf "  [branch-switch] dark-side → happy-end\n"; \
		echo "[branch-switch: dark-side → happy-end]" >> "$$OUT"; \
		"$$BIN" --history "$$HIST" --strategy branching \
			--branch-switch "happy-end" 2>>"$$OUT"; \
		echo "" >> "$$OUT"; \
		echo "====== ВЕТКА happy-end: счастливое приключение (главы 4-5) ======" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		for TURN in 4 5; do \
			case "$$TURN" in \
				4) Q="Напиши четвёртую главу. Весёлое приключение: львёнок встречает новых друзей — черепаху и попугая. Не больше 100 слов.";; \
				5) Q="Напиши пятую главу. Герои вместе радостно исследуют саванну и помогают друг другу. Не больше 100 слов.";; \
			esac; \
			printf "  [branching/happy-end] Ход %d...\n" "$$TURN"; \
			RESP_TMP=$$(mktemp); STAT_TMP=$$(mktemp); \
			"$$BIN" --query "$$Q" --history "$$HIST" \
				--strategy branching --history-limit 0 \
				--format-hint "Пиши на русском языке. Тон — радостный, оптимистичный." \
				--show-cost $(DEBUG_FLAG) \
				>"$$RESP_TMP" 2>"$$STAT_TMP"; \
			echo "--- Ход $$TURN [happy-end] ---" >> "$$OUT"; \
			echo "Вопрос: $$Q" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			cat "$$RESP_TMP" >> "$$OUT"; \
			echo "" >> "$$OUT"; \
			grep -E '^\[(tokens|context|cost)\]' "$$STAT_TMP" >> "$$OUT" || true; \
			echo "" >> "$$OUT"; \
			rm -f "$$RESP_TMP" "$$STAT_TMP"; \
		done; \
		printf "  [branch-switch] happy-end → dark-side (финал)\n"; \
		echo "[branch-switch: happy-end → dark-side (финальная глава)]" >> "$$OUT"; \
		"$$BIN" --history "$$HIST" --strategy branching \
			--branch-switch "dark-side" 2>>"$$OUT"; \
		echo "" >> "$$OUT"; \
		echo "====== ВЕТКА dark-side: финал (глава 6) ======" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		Q="Напиши шестую (финальную) главу. Герой преодолевает бурю, находит дорогу домой и возвращается мудрее и сильнее. Не больше 100 слов."; \
		printf "  [branching/dark-side] Ход 6 (финал)...\n"; \
		RESP_TMP=$$(mktemp); STAT_TMP=$$(mktemp); \
		"$$BIN" --query "$$Q" --history "$$HIST" \
			--strategy branching --history-limit 0 \
			--format-hint "Пиши на русском языке. Тон — торжественный, катарсис." \
			--show-cost $(DEBUG_FLAG) \
			>"$$RESP_TMP" 2>"$$STAT_TMP"; \
		echo "--- Ход 6 [dark-side — финал] ---" >> "$$OUT"; \
		echo "Вопрос: $$Q" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		cat "$$RESP_TMP" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		grep -E '^\[(tokens|context|cost)\]' "$$STAT_TMP" >> "$$OUT" || true; \
		echo "" >> "$$OUT"; \
		rm -f "$$RESP_TMP" "$$STAT_TMP"; \
		printf "  [branch-switch] dark-side → happy-end (финал)\n"; \
		echo "[branch-switch: dark-side → happy-end (финальная глава)]" >> "$$OUT"; \
		"$$BIN" --history "$$HIST" --strategy branching \
			--branch-switch "happy-end" 2>>"$$OUT"; \
		echo "" >> "$$OUT"; \
		echo "====== ВЕТКА happy-end: финал (глава 6) ======" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		Q="Напиши шестую (финальную) главу. Все герои воссоединяются и устраивают праздник под звёздами саванны. Не больше 100 слов."; \
		printf "  [branching/happy-end] Ход 6 (финал)...\n"; \
		RESP_TMP=$$(mktemp); STAT_TMP=$$(mktemp); \
		"$$BIN" --query "$$Q" --history "$$HIST" \
			--strategy branching --history-limit 0 \
			--format-hint "Пиши на русском языке. Тон — праздничный, тёплый." \
			--show-cost $(DEBUG_FLAG) \
			>"$$RESP_TMP" 2>"$$STAT_TMP"; \
		echo "--- Ход 6 [happy-end — финал] ---" >> "$$OUT"; \
		echo "Вопрос: $$Q" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		cat "$$RESP_TMP" >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		grep -E '^\[(tokens|context|cost)\]' "$$STAT_TMP" >> "$$OUT" || true; \
		echo "" >> "$$OUT"; \
		rm -f "$$RESP_TMP" "$$STAT_TMP"; \
		echo "====== Состояние веток (branch-list) ======" >> "$$OUT"; \
		"$$BIN" --history "$$HIST" --strategy branching --branch-list >> "$$OUT"; \
		echo "" >> "$$OUT"; \
		echo "=== Конец: branching ===" >> "$$OUT"; \
	}; \
	\
	echo "--- [1/3] sliding-window (window-size=$$WIN) ---"; \
	run_scenario "sliding-window" "$$SW_HIST" "$$SW_OUT"; \
	echo "  ✓ Результат → $$SW_OUT"; \
	echo ""; \
	\
	echo "--- [2/3] sticky-facts (window-size=$$WIN, с обновлением фактов) ---"; \
	run_scenario "sticky-facts" "$$SF_HIST" "$$SF_OUT"; \
	echo "  ✓ Результат → $$SF_OUT"; \
	echo ""; \
	\
	echo "--- [3/3] branching (2 checkpoint + 2 ветки: dark-side / happy-end) ---"; \
	run_branching_scenario "$$BR_HIST" "$$BR_OUT"; \
	echo "  ✓ Результат → $$BR_OUT"; \
	echo ""; \
	\
	echo "--- Формирование задания для AI-критика ---"; \
	{ \
		echo "Тебе предоставлены результаты работы AI-ассистента при трёх стратегиях управления контекстом."; \
		echo ""; \
		echo "Стратегии:"; \
		echo "  1. sliding-window (window=$$WIN) — хранит последние N сообщений, старые удаляются."; \
		echo "     Сценарий: детская книга о львёнке, 10 диалоговых ходов (9 глав + вопрос о 2-й главе)."; \
		echo "  2. sticky-facts (window=$$WIN) — ключевые факты в KV-хранилище + последние N сообщений."; \
		echo "     Сценарий: тот же 10-ходовой сценарий с книгой."; \
		echo "  3. branching — независимые ветки диалога с checkpoints."; \
		echo "     Сценарий: главы 1-3 на ветке main (с 2 checkpoints: after-ch1, after-ch3),"; \
		echo "     затем 2 ветки от after-ch3 с разными сюжетными линиями:"; \
		echo "       - dark-side: тёмный/напряжённый тон (главы 4-5-6)"; \
		echo "       - happy-end: радостный/оптимистичный тон (главы 4-5-6)"; \
		echo "     С переключениями: dark→happy→dark→happy в процессе."; \
		echo ""; \
		echo "Сравни стратегии по четырём критериям:"; \
		echo "  1. Качество ответа — связность, стиль, детальность глав"; \
		echo "  2. Стабильность контекста — помнит ли стратегия важные детали"; \
		echo "     (для sw/sf — вопрос о 2-й главе на ходу 10;"; \
		echo "      для branching — независимость веток, разница между dark-side и happy-end)"; \
		echo "  3. Расход токенов — сравни строки [tokens] и [cost]"; \
		echo "  4. Удобство для пользователя — прозрачность, предсказуемость, гибкость"; \
		echo ""; \
		echo "############### sliding-window ###############"; \
		echo ""; \
		cat "$$SW_OUT"; \
		echo ""; \
		echo "############### sticky-facts #################"; \
		echo ""; \
		cat "$$SF_OUT"; \
		echo ""; \
		echo "############### branching (2 ветки) ##########"; \
		echo ""; \
		cat "$$BR_OUT"; \
	} > "$$CRITIC_TMP"; \
	\
	echo "--- [Критик] AI-анализ стратегий (openai/gpt-oss-120b:free) ---"; \
	echo ""; \
	CRITIC_SYS="Ты — опытный AI-исследователь и литературный редактор. Твоя задача — объективно сравнить три стратегии управления контекстом в диалоговом AI-ассистенте на основе реального многоходового сценария. Будь конкретен: цитируй различия в ответах, сравнивай цифры токенов. Каждый критерий завершай рейтингом трёх стратегий (1 = лучшая)."; \
	LLM_MODEL="openai/gpt-oss-120b:free" "$$BIN" \
		--query "$$(cat $$CRITIC_TMP)" \
		--system "$$CRITIC_SYS" \
		--history "" \
		--format-hint "Структурируй анализ с Markdown-заголовками: ## 1. Качество ответа, ## 2. Стабильность, ## 3. Расход токенов, ## 4. Удобство для пользователя, ## Итоговый вывод. Пиши на русском языке. Будь конкретен и лаконичен." \
		$(DEBUG_FLAG); \
	\
	rm -f "$$SW_HIST" "$$SF_HIST" "$$BR_HIST" \
		"$${SW_HIST%.json}.stats.json" \
		"$${SF_HIST%.json}.stats.json" \
		"$${BR_HIST%.json}.stats.json" \
		"$${SF_HIST%.json}.facts.json" \
		"$${BR_HIST%.json}.branch-state.json" \
		"$${BR_HIST%.json}.branch-dark-side.json" \
		"$${BR_HIST%.json}.branch-happy-end.json" \
		"$$CRITIC_TMP"; \
	echo ""; \
	echo "=========================================="; \
	echo "  Тест завершён. Файлы с результатами:"; \
	echo "  SW : $$SW_OUT"; \
	echo "  SF : $$SF_OUT"; \
	echo "  BR : $$BR_OUT"; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Day11: 3-layer memory demos
# ---------------------------------------------------------------------------

run-memory: $(BINARY_NAME) check-env ## [Memory] Single call with 3-layer memory, always writes WM+LTM (QUERY=, QUERY_FILE=, SYSTEM_FILE=)
	@echo "=== Memory integration test ==="
	@echo "  STM: $(MEMORY_STM_FILE)"
	@echo "  WM:  $(MEMORY_WM_FILE)"
	@echo "  LTM: $(MEMORY_LTM_FILE)"
	@[ -n "$(SYSTEM_FILE)" ] && echo "  system file: $(SYSTEM_FILE)" || true
	@if [ -n "$(QUERY)" ]; then \
		QC="$(QUERY)"; echo "  query (inline): $$QC"; \
	else \
		if [ ! -f "$(QUERY_FILE)" ]; then \
			echo "Error: query file '$(QUERY_FILE)' not found"; \
			echo "  Run: make run-memory QUERY=\"your query\""; \
			echo "    or: make run-memory QUERY_FILE=path/to/query.txt"; \
			exit 1; \
		fi; \
		QC="$$(cat "$(QUERY_FILE)")"; echo "  query file: $(QUERY_FILE)"; \
	fi; \
	if [ -n "$(SYSTEM_FILE)" ]; then \
		if [ ! -f "$(SYSTEM_FILE)" ]; then \
			echo "Error: SYSTEM_FILE '$(SYSTEM_FILE)' not found"; exit 1; \
		fi; \
		SYS="$$(cat "$(SYSTEM_FILE)")"; \
		./$(BINARY_NAME) --query "$$QC" \
			--history "$(MEMORY_STM_FILE)" \
			--memory-wm "$(MEMORY_WM_FILE)" \
			--memory-ltm "$(MEMORY_LTM_FILE)" \
			--memory-update \
			--system "$$SYS" \
			--show-cost $(DEBUG_FLAG); \
	else \
		./$(BINARY_NAME) --query "$$QC" \
			--history "$(MEMORY_STM_FILE)" \
			--memory-wm "$(MEMORY_WM_FILE)" \
			--memory-ltm "$(MEMORY_LTM_FILE)" \
			--memory-update \
			--show-cost $(DEBUG_FLAG); \
	fi

run-memory-demo: $(BINARY_NAME) check-env ## [Memory] 4 turns: build WM+LTM across turns
	@STM="$(MEMORY_STM_FILE)"; WM="$(MEMORY_WM_FILE)"; LTM="$(MEMORY_LTM_FILE)"; \
	rm -f "$$STM" "$$STM".stats.json "$$WM" "$$LTM"; \
	echo "=========================================="; \
	echo "  3-Layer Memory Demo (Day11)"; \
	echo "  STM: $$STM"; \
	echo "  WM:  $$WM"; \
	echo "  LTM: $$LTM"; \
	echo "=========================================="; echo ""; \
	echo "--- Turn 1: задача проекта ---"; \
	./$(BINARY_NAME) --query "Я Golang разработчик. Мне необходимо разработать telegram-bot. Бот должен повторять отправленое ему сообщение 10 раз с чередованием регистра первой буквы сообщения." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	echo "--- Turn 2: ограничение на библиотеки ---"; \
	./$(BINARY_NAME) --query "При разработке бота должено использоваться только официальная библиотека для работы с telegram. Все остальные фунции должны быть реализованы с помощью стандатной библиотке Golang которая идет в компленкте с языком" \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	echo "--- Turn 3: новое требование — статистика ---"; \
	./$(BINARY_NAME) --query "Необходимо вести статистику сколько пользователей написало и исколько слов был повторено. Статистика должна быть ввиде отдельного файла" \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	echo "--- Turn 4: recall из памяти (чистый STM, WM+LTM инжектируются) ---"; \
	HIST2=$$(mktemp /tmp/memory_fresh_XXXXXX.json); \
	./$(BINARY_NAME) --query "Что ты знаешь о моём проекте? Перечисли все ключевые требования и принятые решения." \
		--history "$$HIST2" --memory-wm "$$WM" --memory-ltm "$$LTM" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$HIST2"; \
	echo ""; \
	echo "--- WM contents ---"; \
	python3 -c "import json; d=json.load(open('$$WM')); [print(f'  {k}: {v}') for k,v in d.get('facts',{}).items()]" 2>/dev/null || cat "$$WM"; \
	echo ""; \
	echo "--- LTM contents ---"; \
	python3 -c "import json; d=json.load(open('$$LTM')); [print(f'  {k}: {v}') for k,v in d.get('entries',{}).items()]" 2>/dev/null || cat "$$LTM"; \
	echo ""; \
	echo "=========================================="

run-memory-recall: $(BINARY_NAME) check-env ## [Memory] Verify WM+LTM survive across sessions (no STM)
	@WM="$(MEMORY_WM_FILE)"; LTM="$(MEMORY_LTM_FILE)"; \
	if [ ! -f "$$WM" ] && [ ! -f "$$LTM" ]; then \
		echo "Error: run 'make run-memory-demo' first to build memory files"; exit 1; \
	fi; \
	echo "=========================================="; \
	echo "  Memory Recall Test (fresh STM, WM+LTM from previous session)"; \
	echo "=========================================="; \
	FRESH_STM=$$(mktemp /tmp/recall_stm_XXXXXX.json); \
	./$(BINARY_NAME) \
		--query "Without any conversation history, tell me: what is my name, what am I building, and what database did we choose?" \
		--format-hint "Be specific. List each fact on a separate line." \
		--history "$$FRESH_STM" --memory-wm "$$WM" --memory-ltm "$$LTM" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$FRESH_STM"; \
	echo ""; echo "=========================================="

run-memory-layers: $(BINARY_NAME) check-env ## [Memory] Show all 3 layers side by side after run-memory-demo
	@STM="$(MEMORY_STM_FILE)"; WM="$(MEMORY_WM_FILE)"; LTM="$(MEMORY_LTM_FILE)"; \
	echo "=========================================="; \
	echo "  Layer 1 — STM (short-term / dialogue)"; \
	echo "=========================================="; \
	python3 -c "import json; msgs=json.load(open('$$STM')); print(f'  {len(msgs)} messages'); [print(f'  [{m[\"role\"]}] {m[\"content\"][:80]}...') for m in msgs]" 2>/dev/null || echo "  (not found)"; \
	echo ""; \
	echo "=========================================="; \
	echo "  Layer 2 — WM (working memory / task facts)"; \
	echo "=========================================="; \
	python3 -c "import json; d=json.load(open('$$WM')); [print(f'  {k}: {v}') for k,v in d.get('facts',{}).items()]" 2>/dev/null || echo "  (not found)"; \
	echo ""; \
	echo "=========================================="; \
	echo "  Layer 3 — LTM (long-term memory / profile)"; \
	echo "=========================================="; \
	python3 -c "import json; d=json.load(open('$$LTM')); [print(f'  {k}: {v}') for k,v in d.get('entries',{}).items()]" 2>/dev/null || echo "  (not found)"; \
	echo ""

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -f $(BATCH_OUTPUT)
	rm -f $(HISTORY_TEST_FILE) $(HISTORY_TEST_FILE:.json=.stats.json) $(HISTORY_TEST_FILE:.json=.summary.txt)
	rm -f $(SUMMARY_TEST_FILE) $(SUMMARY_TEST_FILE:.json=.stats.json) $(SUMMARY_TEST_FILE:.json=.summary.txt)
	rm -f $(TOKENS_HIST_FILE) $(TOKENS_HIST_FILE:.json=.stats.json)
	rm -f $(BRANCH_HIST_FILE) $(BRANCH_HIST_FILE:.json=.stats.json) \
		$(BRANCH_HIST_FILE:.json=.branch-state.json) \
		$(BRANCH_HIST_FILE:.json=.branch-plan-a.json) \
		$(BRANCH_HIST_FILE:.json=.branch-plan-b.json)
	rm -f $(FACTS_HIST_FILE) $(FACTS_HIST_FILE:.json=.stats.json) $(FACTS_HIST_FILE:.json=.facts.json)
	rm -f $(LION_SW_FILE) $(LION_SF_FILE) $(LION_BR_FILE)
	rm -f $(MEMORY_STM_FILE) $(MEMORY_STM_FILE:.json=.stats.json) $(MEMORY_WM_FILE) $(MEMORY_LTM_FILE)
	$(GO) clean -testcache

install-deps: ## Install and tidy dependencies
	$(GO) mod download
	$(GO) mod tidy

help: ## Show this help
	@echo "Available targets:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-30s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Context strategies (--strategy flag):"
	@echo "  none           No strategy (default). --summary flag enables auto-summarization."
	@echo "  sliding-window Keep only last --window-size messages (no summary, just drop)."
	@echo "  sticky-facts   Key-value facts store survives window rotation. --window-size for recent msgs."
	@echo "  branching      Branch conversations from checkpoints. Use management flags below."
	@echo ""
	@echo "Branching management flags:"
	@echo "  --checkpoint <name>         Save current history as a checkpoint (no --query needed)"
	@echo "  --branch-new <name>         Create + switch to new branch (add --from-checkpoint <name>)"
	@echo "  --branch-switch <name>      Switch to existing branch"
	@echo "  --branch-list               List all branches and checkpoints"
	@echo ""
	@echo "Day11 — 3-layer memory flags:"
	@echo "  --memory-wm <path>          Working memory file (Layer 2: task facts). Default: <history>.wm.json"
	@echo "  --memory-ltm <path>         Long-term memory file (Layer 3: profile/knowledge). Default: <history>.ltm.json"
	@echo "  --memory-update             Auto-update WM and LTM after each call (2 extra LLM calls)"
	@echo ""
	@echo "Day10 flags:"
	@echo "  --summary                   Enable auto-summarization (disabled by default)"
	@echo "  --strategy <name>           Context management strategy (default: none)"
	@echo "  --window-size N             Window size for sliding-window / sticky-facts (default: 5)"
	@echo ""
	@echo "Strategy demo targets:"
	@echo "  make run-sliding-window     Demo: messages dropped after window"
	@echo "  make run-sticky-facts       Demo: facts survive window rotation"
	@echo "  make run-branching          Demo: full checkpoint → 2-branch workflow"
	@echo ""
	@echo "Parameters (run-integration):"
	@echo "  QUERY          Inline query string"
	@echo "  QUERY_FILE     Path to query file (default: query.txt)"
	@echo "  STRATEGY       Context strategy: sliding-window | sticky-facts | branching (default: none)"
	@echo "  WINDOW_SIZE    Window / facts size (default: 5)"
	@echo "  SUMMARY        Set to 1 to enable auto-summarization (e.g. SUMMARY=1)"
	@echo "  HISTORY_FILE   History file path (default: chat_history.json)"
	@echo "  HISTORY_LIMIT  Max messages before summary triggers (default: 10)"
	@echo "  SYSTEM_FILE    Path to system message file (optional)"
	@echo "  DEBUG          Set to 1 to print raw JSON request"
	@echo ""
	@echo "Parameters (env):"
	@echo "  LLM_API_KEY    API key (required)"
	@echo "  LLM_PROVIDER   openai | openrouter (default: openrouter)"
	@echo "  LLM_MODEL      Model name (default: openai/gpt-4o-mini)"
	@echo ""
	@echo "Examples:"
	@echo "  make run-integration QUERY=\"What is Go?\" STRATEGY=sliding-window WINDOW_SIZE=3"
	@echo "  make run-integration QUERY_FILE=query.txt STRATEGY=sticky-facts"
	@echo "  make run-integration QUERY=\"hello\" SUMMARY=1 HISTORY_LIMIT=4"
	@echo "  make run-integration QUERY=\"hello\" SYSTEM_FILE=system.txt STRATEGY=branching"
	@echo ""
	@echo "3-Layer memory targets (Day11):"
	@echo "  make run-memory             Single call with all 3 memory layers (custom query)"
	@echo "  make run-memory-demo        4-turn demo: build WM+LTM, then recall with fresh STM"
	@echo "  make run-memory-recall      Recall from WM+LTM only (no dialogue history)"
	@echo "  make run-memory-layers      Print all 3 layer files side by side"
	@echo ""
	@echo "run-memory parameters:"
	@echo "  QUERY          Inline query string (required if QUERY_FILE not set)"
	@echo "  QUERY_FILE     Path to query file (default: query.txt)"
	@echo "  SYSTEM_FILE    Path to system message file (optional)"
	@echo "  MEMORY_STM_FILE / MEMORY_WM_FILE / MEMORY_LTM_FILE — override memory file paths"
	@echo "  Note: WM and LTM are always written (--memory-update is always on for this target)"
	@echo ""
	@echo "Memory file variables:"
	@echo "  MEMORY_STM_FILE   STM (dialogue) file  (default: $(MEMORY_STM_FILE))"
	@echo "  MEMORY_WM_FILE    WM  (task facts) file (default: $(MEMORY_WM_FILE))"
	@echo "  MEMORY_LTM_FILE   LTM (profile)   file (default: $(MEMORY_LTM_FILE))"
	@echo ""
	@echo "Lion Book strategy comparison test:"
	@echo "  make run-lion-book-test                        # ~41 LLM calls, uses LION_WINDOW_SIZE=$(LION_WINDOW_SIZE)"
	@echo "  make run-lion-book-test LION_WINDOW_SIZE=3     # tighter window for starker contrast"
	@echo "  make run-lion-book-test DEBUG=1                # also print raw JSON payloads"
	@echo "  Results written to: $(LION_SW_FILE)  $(LION_SF_FILE)  $(LION_BR_FILE)"

.DEFAULT_GOAL := help
