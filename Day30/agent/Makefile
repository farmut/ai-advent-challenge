.PHONY: test clean help check-env build \
        run-interactive run-interactive-manual \
        run-interactive-test run-interactive-test-happy run-interactive-test-pause-resume \
        run-integration run-integration-batch \
        run-history-basic run-history-accumulate run-history-disabled run-history-context \
        run-history-all \
        run-tokens run-tokens-cost run-tokens-session \
        run-context-overflow \
        run-summary-test run-critic-test \
        run-sliding-window run-sticky-facts run-branching \
        run-lion-book-test \
        run-memory-demo run-memory-recall run-memory-layers run-memory \
        run-profile-init run-profile-demo run-profile-set run-profile-list run-profile-delete \
        run-profile-go-dev run-profile-ai-critic run-profile-data-sci run-profile-all \
        run-profile-only-go-dev run-profile-only-ai-critic run-profile-only-data-sci run-profile-only-all \
        run-invariants-init run-invariants-show \
        run-mcp-add-list-remove run-mcp-tools-filesystem run-mcp-slash-test \
        run-mcp-test run-mcp-all \
        run-mcp-session run-mcp-session-persist \
        run-rag run-rag-interactive run-rag-eval run-rag-grounded run-rag-compare run-rag-rerank-compare \
        run-rag-local run-rag-interactive-local run-rag-eval-local run-rag-grounded-local \
        run-rag-compare-local run-rag-rerank-compare-local run-rag-tests-local \
        run-telegram-opt run-telegram-opt-local

DAY     := $(shell basename $(CURDIR) | tr '[:upper:]' '[:lower:]')
DAY_NUM := $(shell basename $(CURDIR) | tr -d 'A-Za-z')

BINARY_NAME := ai-adv-agent-$(DAY)
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

HISTORY_TEST_FILE  ?= /tmp/test_chat_history_$(DAY).json
SUMMARY_TEST_FILE  ?= /tmp/summary_test_$(DAY).json
OVERFLOW_MODEL     ?= openai/gpt-4o-mini
TOKENS_HIST_FILE   ?= /tmp/token_demo_$(DAY).json

# Day11/12: 3-layer memory files
MEMORY_STM_FILE    ?= /tmp/memory_stm_$(DAY).json
MEMORY_WM_FILE     ?= /tmp/memory_wm_$(DAY).json
MEMORY_LTM_FILE    ?= /tmp/memory_ltm_$(DAY).json

# Day12: user profile (Markdown format — human-editable)
PROFILE_FILE       ?= /tmp/user_profile_$(DAY).md
MEMORY_UPDATE      ?=
MEMORY_UPDATE_FLAG  = $(if $(MEMORY_UPDATE),--memory-update,)

# Day14: invariants file (Markdown — absolute constraints the agent must never violate)
# Set INVARIANTS_FILE=/path/to/file.md before running interactive targets to activate invariants.
# If not set, the agent auto-derives the path from --history (e.g. agent_session.invariants.md).
INVARIANTS_FILE   ?=
INVARIANTS_FLAG    = $(if $(INVARIANTS_FILE),--invariants $(INVARIANTS_FILE),)

# Day16: MCP server config
MCP_CONFIG_FILE    ?= /tmp/mcp_config_$(DAY).yaml
MCP_HIST_FILE      ?= /tmp/mcp_test_$(DAY).json

# Day16: MCP manual session (make run-mcp-session)
MCP_SESSION_HIST   ?= /tmp/mcp_session_$(DAY).json
MCP_SESSION_CFG    ?= /tmp/mcp_session_$(DAY).mcp.yaml
# Optional: set to a profile .md file to inject persona into the session
MCP_SESSION_PROF   ?=
# Optional: directory exposed to the filesystem MCP server (default: current dir)
MCP_FS_ROOT        ?= $(CURDIR)

# Multi-profile integration tests: per-persona file sets (Day10/Day11 scenarios)
PROFILE_GO_DEV_FILE    ?= /tmp/profile_go_dev_$(DAY).md
PROFILE_AI_CRITIC_FILE ?= /tmp/profile_ai_critic_$(DAY).md
PROFILE_DATA_SCI_FILE  ?= /tmp/profile_dsci_$(DAY).md
GODEV_STM_FILE         ?= /tmp/godev_stm_$(DAY).json
GODEV_WM_FILE          ?= /tmp/godev_wm_$(DAY).json
GODEV_LTM_FILE         ?= /tmp/godev_ltm_$(DAY).json
CRITIC_STM_FILE        ?= /tmp/critic_stm_$(DAY).json
CRITIC_WM_FILE         ?= /tmp/critic_wm_$(DAY).json
CRITIC_LTM_FILE        ?= /tmp/critic_ltm_$(DAY).json
DSCI_STM_FILE          ?= /tmp/dsci_stm_$(DAY).json
DSCI_WM_FILE           ?= /tmp/dsci_wm_$(DAY).json
DSCI_LTM_FILE          ?= /tmp/dsci_ltm_$(DAY).json

# Profile-only integration tests: only profile layer, no WM/LTM/--memory-update
PONLY_GO_DEV_FILE      ?= /tmp/ponly_go_dev_$(DAY).md
PONLY_AI_CRITIC_FILE   ?= /tmp/ponly_ai_critic_$(DAY).md
PONLY_DATA_SCI_FILE    ?= /tmp/ponly_dsci_$(DAY).md
PONLY_GO_DEV_STM       ?= /tmp/ponly_godev_stm_$(DAY).json
PONLY_CRITIC_STM       ?= /tmp/ponly_critic_stm_$(DAY).json
PONLY_DSCI_STM         ?= /tmp/ponly_dsci_stm_$(DAY).json

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

# Interactive 4-phase agent (planning → execution → validation → done)
AGENT_HISTORY_FILE       ?= /tmp/agent_session_$(DAY).json
AGENT_PROFILE_FILE       ?= /tmp/agent_profile_$(DAY).md
INTERACTIVE_HIST_FILE    ?= /tmp/interactive_test_$(DAY).json
INTERACTIVE_TASK_FILE    := $(INTERACTIVE_HIST_FILE:%.json=%.task.json)
INTERACTIVE_PROFILE_FILE ?= /tmp/interactive_profile_$(DAY).md

run-interactive: $(BINARY_NAME) check-env ## [Interactive] Start the 4-phase task agent (AGENT_HISTORY_FILE=, AGENT_PROFILE_FILE=, INVARIANTS_FILE=, MEMORY_UPDATE=1)
	@PROF="$(AGENT_PROFILE_FILE)"; \
	if [ ! -f "$$PROF" ]; then \
		echo "Error: profile not found: $$PROF"; \
		echo "  Create it interactively: make run-profile-init PROFILE_FILE=$$PROF"; \
		echo "  Or specify another:      make run-interactive AGENT_PROFILE_FILE=/path/to/profile.md"; \
		exit 1; \
	fi; \
	echo "=========================================="; \
	echo "  Interactive Agent  (Day $(DAY_NUM))"; \
	echo "  History: $(AGENT_HISTORY_FILE)"; \
	echo "  Profile: $$PROF"; \
	if [ -n "$(INVARIANTS_FILE)" ]; then echo "  Invariants: $(INVARIANTS_FILE)"; fi; \
	echo "=========================================="; \
	echo "--- Active profile ---"; cat "$$PROF"; echo ""; \
	if [ -n "$(INVARIANTS_FILE)" ] && [ -f "$(INVARIANTS_FILE)" ]; then \
		echo "--- Active invariants ---"; cat "$(INVARIANTS_FILE)"; echo ""; \
	fi; \
	./$(BINARY_NAME) \
		--interactive \
		--history "$(AGENT_HISTORY_FILE)" \
		--profile "$$PROF" \
		--history-limit 50 \
		--show-cost \
		$(INVARIANTS_FLAG) \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)

MANUAL_HIST_FILE ?= /tmp/manual_session_$(DAY).json
MANUAL_TASK_FILE := $(MANUAL_HIST_FILE:%.json=%.task.json)

run-interactive-manual: $(BINARY_NAME) check-env ## [Interactive] Fresh session for manual testing — clears history/task on each run (INTERACTIVE_PROFILE_FILE=)
	@HIST="$(MANUAL_HIST_FILE)"; \
	TASK="$(MANUAL_TASK_FILE)"; \
	PROF="$(INTERACTIVE_PROFILE_FILE)"; \
	if [ ! -f "$$PROF" ]; then \
		echo "Error: profile not found: $$PROF"; \
		echo "  Create it interactively: make run-profile-init PROFILE_FILE=$$PROF"; \
		echo "  Or specify another:      make run-interactive-manual INTERACTIVE_PROFILE_FILE=/path/to/profile.md"; \
		exit 1; \
	fi; \
	rm -f "$$HIST" "$$TASK" "$${HIST%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Manual Interactive Session  (Day $(DAY_NUM))"; \
	echo "=========================================="; \
	echo "  Session file: $$HIST  (fresh each run)"; \
	echo "  Profile:      $$PROF"; \
	echo "  Model:        $(LLM_MODEL)"; \
	echo "  Provider:     $(LLM_PROVIDER)"; \
	echo ""; \
	echo "--- Active profile ---"; cat "$$PROF"; echo ""; \
	echo "  FSM stages: planning → execution → validation → done"; \
	echo "  At any stage prompt type:"; \
	echo "    y / yes  — approve / accept"; \
	echo "    pause    — suspend and save state"; \
	echo "    resume   — continue a paused task"; \
	echo "    exit     — quit"; \
	echo "  Any other text is treated as feedback / rejection."; \
	if [ -n "$(INVARIANTS_FILE)" ]; then echo "  Invariants: $(INVARIANTS_FILE)"; fi; \
	echo "=========================================="; echo ""; \
	if [ -n "$(INVARIANTS_FILE)" ] && [ -f "$(INVARIANTS_FILE)" ]; then \
		echo "--- Active invariants ---"; cat "$(INVARIANTS_FILE)"; echo ""; \
	fi; \
	./$(BINARY_NAME) \
		--interactive \
		--history "$$HIST" \
		--profile "$$PROF" \
		--history-limit 50 \
		--show-cost \
		$(INVARIANTS_FLAG) \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)

# ---------------------------------------------------------------------------
# Day14: Interactive agent integration tests
# ---------------------------------------------------------------------------
# Input sequences are piped via printf — no interactive terminal needed.
# Tests check both FSM lifecycle (phase labels in output) and side-effects
# (task state file created / cleared, history accumulated).
#
# Override test files: INTERACTIVE_HIST_FILE=/tmp/my.json

run-interactive-test-happy: $(BINARY_NAME) check-env ## [Interactive] Test 1: full planning→execution→validation→done (with profile)
	@HIST="$(INTERACTIVE_HIST_FILE)"; \
	TASK="$(INTERACTIVE_TASK_FILE)"; \
	PROF="$(INTERACTIVE_PROFILE_FILE)"; \
	rm -f "$$HIST" "$$TASK" "$${HIST%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Interactive Test 1: happy path"; \
	echo "  (planning → execution → validation → done)"; \
	echo "  Profile: $$PROF"; \
	echo "=========================================="; echo ""; \
	echo "--- Setting up test profile ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "TestUser"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=english" \
		--profile-set "style=concise" \
		--profile-set "format=plain"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-list; \
	echo ""; \
	OUT_FILE=$$(mktemp); \
	printf 'Write only the word TESTOK, nothing else.\n/yes\n/yes\n/yes\nexit\n' | \
		./$(BINARY_NAME) --interactive \
			--history "$$HIST" --history-limit 50 \
			--profile "$$PROF" \
			--show-cost $(DEBUG_FLAG) \
		>"$$OUT_FILE" 2>&1; \
	echo "--- Agent output ---"; cat "$$OUT_FILE"; echo ""; echo "--------------------"; echo ""; \
	FAILED=0; \
	echo "  Checks:"; \
	for PHASE in PLANNING EXECUTION VALIDATION DONE; do \
		if grep -qF "[$$PHASE]" "$$OUT_FILE"; then \
			echo "  ✓ phase $$PHASE present in output"; \
		else \
			echo "  ✗ FAIL: phase $$PHASE not found in output"; FAILED=$$((FAILED+1)); \
		fi; \
	done; \
	if [ -f "$$PROF" ]; then \
		echo "  ✓ profile file exists: $$PROF"; \
	else \
		echo "  ✗ FAIL: profile file missing after test"; FAILED=$$((FAILED+1)); \
	fi; \
	if grep -q '\[memory\] profile: loaded' "$$OUT_FILE"; then \
		echo "  ✓ profile layer: loaded at startup"; \
	else \
		echo "  ✗ FAIL: profile not reported as loaded at startup (expected '[memory] profile: loaded ...')"; FAILED=$$((FAILED+1)); \
	fi; \
	if grep -q '\[memory\] WM:' "$$OUT_FILE"; then \
		echo "  ✓ WM layer: startup status reported"; \
	else \
		echo "  ✗ FAIL: WM layer status not reported at startup"; FAILED=$$((FAILED+1)); \
	fi; \
	if grep -q '\[memory\] LTM:' "$$OUT_FILE"; then \
		echo "  ✓ LTM layer: startup status reported"; \
	else \
		echo "  ✗ FAIL: LTM layer status not reported at startup"; FAILED=$$((FAILED+1)); \
	fi; \
	if [ ! -f "$$TASK" ]; then \
		echo "  ✓ task state file cleared after done"; \
	else \
		echo "  ✗ FAIL: task state file still exists after done"; FAILED=$$((FAILED+1)); \
	fi; \
	HIST_COUNT=$$(python3 -c "import json; print(len(json.load(open('$$HIST'))))" 2>/dev/null || echo 0); \
	if [ "$$HIST_COUNT" -ge 4 ]; then \
		echo "  ✓ history has $$HIST_COUNT messages (≥4 expected: planning + execution × 2)"; \
	else \
		echo "  ✗ FAIL: expected ≥4 messages in history, got $$HIST_COUNT"; FAILED=$$((FAILED+1)); \
	fi; \
	rm -f "$$OUT_FILE"; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then \
		echo "  Result: PASS"; \
	else \
		echo "  Result: $$FAILED check(s) FAILED"; exit 1; \
	fi; \
	echo "=========================================="

run-interactive-test-pause-resume: $(BINARY_NAME) check-env ## [Interactive] Test 2: pause saves state, resume continues to done (with profile)
	@HIST="$(INTERACTIVE_HIST_FILE)"; \
	TASK="$(INTERACTIVE_TASK_FILE)"; \
	PROF="$(INTERACTIVE_PROFILE_FILE)"; \
	rm -f "$$HIST" "$$TASK" "$${HIST%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Interactive Test 2: pause and resume"; \
	echo "  Phase A — start task, pause at plan approval"; \
	echo "  Phase B — restart agent, resume, complete task"; \
	echo "  Profile: $$PROF"; \
	echo "=========================================="; echo ""; \
	echo "--- Setting up test profile ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "TestUser"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=english" \
		--profile-set "style=concise" \
		--profile-set "format=plain"; \
	echo ""; \
	\
	echo "--- Phase A: start task and pause ---"; \
	OUT_A=$$(mktemp); \
	printf 'Write only the word TESTOK, nothing else.\npause\n' | \
		./$(BINARY_NAME) --interactive \
			--history "$$HIST" --history-limit 50 \
			--profile "$$PROF" \
			--show-cost $(DEBUG_FLAG) \
		>"$$OUT_A" 2>&1; \
	echo "Output A:"; cat "$$OUT_A"; echo ""; \
	FAILED=0; \
	if grep -qF '[paused]' "$$OUT_A"; then \
		echo "  ✓ pause confirmed in output"; \
	else \
		echo "  ✗ FAIL: [paused] not found in output"; FAILED=$$((FAILED+1)); \
	fi; \
	if [ -f "$$TASK" ]; then \
		echo "  ✓ task state file created on disk"; \
	else \
		echo "  ✗ FAIL: task state file not found after pause"; FAILED=$$((FAILED+1)); \
	fi; \
	TASK_PHASE=$$(python3 -c "import json; print(json.load(open('$$TASK'))['phase'])" 2>/dev/null || echo "?"); \
	if [ "$$TASK_PHASE" = "planning" ]; then \
		echo "  ✓ paused at phase: $$TASK_PHASE"; \
	else \
		echo "  ✗ FAIL: expected phase=planning, got $$TASK_PHASE"; FAILED=$$((FAILED+1)); \
	fi; \
	TASK_ITER=$$(python3 -c "import json; print(json.load(open('$$TASK'))['iteration'])" 2>/dev/null || echo "?"); \
	echo "  [info] paused iteration: $$TASK_ITER"; \
	\
	echo ""; echo "--- Phase B: restart agent and resume ---"; \
	OUT_B=$$(mktemp); \
	printf '/yes\n/yes\n/yes\n/yes\nexit\n' | \
		./$(BINARY_NAME) --interactive \
			--history "$$HIST" --history-limit 50 \
			--profile "$$PROF" \
			--show-cost $(DEBUG_FLAG) \
		>"$$OUT_B" 2>&1; \
	echo "Output B:"; cat "$$OUT_B"; echo ""; \
	if grep -qF 'PAUSED TASK FOUND' "$$OUT_B"; then \
		echo "  ✓ paused task detected on startup"; \
	else \
		echo "  ✗ FAIL: paused task banner not shown"; FAILED=$$((FAILED+1)); \
	fi; \
	if grep -qF '[resuming]' "$$OUT_B"; then \
		echo "  ✓ resume confirmed in output"; \
	else \
		echo "  ✗ FAIL: [resuming] not found in output"; FAILED=$$((FAILED+1)); \
	fi; \
	if grep -qF '[DONE]' "$$OUT_B"; then \
		echo "  ✓ task completed after resume"; \
	else \
		echo "  ✗ FAIL: DONE phase not reached after resume"; FAILED=$$((FAILED+1)); \
	fi; \
	if [ ! -f "$$TASK" ]; then \
		echo "  ✓ task state file cleared after done"; \
	else \
		echo "  ✗ FAIL: task state file not cleared after done"; FAILED=$$((FAILED+1)); \
	fi; \
	rm -f "$$OUT_A" "$$OUT_B"; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then \
		echo "  Result: PASS"; \
	else \
		echo "  Result: $$FAILED check(s) FAILED"; exit 1; \
	fi; \
	echo "=========================================="

run-interactive-test: $(BINARY_NAME) check-env ## [Interactive] Run all interactive integration tests
	@echo "=========================================="; \
	echo "  Interactive Agent Integration Tests"; \
	echo "  Day $(DAY_NUM) — Task State Machine"; \
	echo "=========================================="
	@$(MAKE) run-interactive-test-happy
	@echo ""
	@$(MAKE) run-interactive-test-pause-resume
	@echo ""; echo "=========================================="; \
	echo "  All interactive tests PASSED"; \
	echo "=========================================="

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

# ---------------------------------------------------------------------------
# Day12: User profile demos
# ---------------------------------------------------------------------------

run-profile-init: $(BINARY_NAME) ## [Profile] Interactive profile initialization (reads from stdin)
	@./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-init

run-profile-set: $(BINARY_NAME) ## [Profile] Set profile name + example preferences (PROFILE_FILE=)
	@echo "=========================================="; \
	echo "  Setting up user profile"; \
	echo "  Profile file: $(PROFILE_FILE)"; \
	echo "=========================================="; echo ""; \
	./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-name "Developer"; \
	./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-set "language=russian" --profile-set "style=concise"; \
	./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-set "format=markdown" --profile-set "constraints=no external libraries"; \
	echo ""; echo "--- Profile after setup ---"; \
	./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-list

run-profile-list: $(BINARY_NAME) ## [Profile] Print current profile (PROFILE_FILE=)
	@./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-list

run-profile-delete: $(BINARY_NAME) ## [Profile] Delete a preference (KEY=, PROFILE_FILE=)
	@if [ -z "$(KEY)" ]; then \
		echo "Usage: make run-profile-delete KEY=<preference-key> [PROFILE_FILE=...]"; \
		exit 1; \
	fi; \
	./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-delete "$(KEY)"; \
	./$(BINARY_NAME) --profile "$(PROFILE_FILE)" --profile-list

run-profile-demo: $(BINARY_NAME) check-env ## [Profile] Full demo: set profile, send query, verify profile appears in context
	@PROF="$(PROFILE_FILE)"; STM=$$(mktemp /tmp/profile_stm_XXXXXX.json); \
	echo "=========================================="; \
	echo "  User Profile Demo (Day12)"; \
	echo "  Profile: $$PROF"; \
	echo "=========================================="; echo ""; \
	echo "--- Step 1: configure profile ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "Andrey"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=russian" \
		--profile-set "style=concise" \
		--profile-set "format=markdown" \
		--profile-set "expertise=Go developer"; \
	echo ""; echo "--- Profile ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-list; \
	echo ""; \
	echo "--- Step 2: send query with profile injected ---"; \
	./$(BINARY_NAME) \
		--query "Что такое интерфейс в Go? Один абзац." \
		--history "$$STM" \
		--profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	echo ""; \
	echo "--- Step 3: follow-up (profile + history context) ---"; \
	./$(BINARY_NAME) \
		--query "Приведи пример реализации интерфейса." \
		--history "$$STM" \
		--profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	echo ""; \
	rm -f "$$STM" "$${STM%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Profile Demo Complete"; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Day12: Multi-profile integration tests (3 personas + combined runner)
#
# Сценарии взяты из реальных данных Day10/Day11:
#   go-dev   — Day11 run-memory-demo: Andrey строит Telegram-бот на Go stdlib
#   ai-critic — Day10/Day11 system_critic.txt: Maria оценивает ответы LLM
#   data-sci  — Alex ведёт ML-эксперимент (бинарная классификация)
#
# Каждый тест активирует все 4 слоя: Profile + STM (Layer 1) + WM (Layer 2) + LTM (Layer 3)
# ---------------------------------------------------------------------------

run-profile-go-dev: $(BINARY_NAME) check-env ## [Profile-IT] Go-dev (Andrey): Telegram-bot + stdlib + все слои памяти (Day11 сценарий)
	@PROF="$(PROFILE_GO_DEV_FILE)"; \
	STM="$(GODEV_STM_FILE)"; WM="$(GODEV_WM_FILE)"; LTM="$(GODEV_LTM_FILE)"; \
	rm -f "$$PROF" "$$STM" "$${STM%.json}.stats.json" "$$WM" "$$LTM"; \
	echo "=========================================="; \
	echo "  Profile Integration Test: Go Developer"; \
	echo "  Persona: Andrey — Golang dev, Telegram bot"; \
	echo "  Layers: Profile + STM + WM (--memory-update) + LTM (--memory-update)"; \
	echo "  Profile: $$PROF"; \
	echo "  STM:     $$STM"; \
	echo "  WM:      $$WM"; \
	echo "  LTM:     $$LTM"; \
	echo "=========================================="; echo ""; \
	\
	echo "--- Step 1: создать профиль ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "Andrey"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=russian" \
		--profile-set "style=concise" \
		--profile-set "format=code-first" \
		--profile-set "expertise=golang-developer" \
		--profile-set "constraints=standard-library-only"; \
	echo ""; ./$(BINARY_NAME) --profile "$$PROF" --profile-list; echo ""; \
	\
	echo "--- Turn 1: задача проекта ---"; \
	./$(BINARY_NAME) \
		--query "Мне необходимо разработать telegram-bot. Бот должен повторять отправлённое ему сообщение 10 раз с чередованием регистра первой буквы сообщения." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 2: новое требование ---"; \
	./$(BINARY_NAME) \
		--query "Нужно ещё выделять повторяемый текст разными цветами." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 3: требование — статистика ---"; \
	./$(BINARY_NAME) \
		--query "Необходимо вести статистику: сколько пользователей написало и сколько слов было повторено. Статистика — в отдельном файле." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 4 (свежий STM): recall из WM + LTM + Profile ---"; \
	FRESH_STM=$$(mktemp /tmp/godev_recall_XXXXXX.json); \
	./$(BINARY_NAME) \
		--query "Что ты знаешь обо мне и моём проекте? Перечисли ключевые требования и ограничения." \
		--history "$$FRESH_STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$FRESH_STM" "$${FRESH_STM%.json}.stats.json"; \
	echo ""; \
	\
	echo "--- WM contents ---"; \
	python3 -c "import json; d=json.load(open('$$WM')); [print(f'  {k}: {v}') for k,v in d.get('facts',{}).items()]" 2>/dev/null || echo "  (не найден)"; \
	echo ""; \
	echo "--- LTM contents ---"; \
	python3 -c "import json; d=json.load(open('$$LTM')); [print(f'  {k}: {v}') for k,v in d.get('entries',{}).items()]" 2>/dev/null || echo "  (не найден)"; \
	echo ""; \
	\
	FAILED=0; \
	[ -f "$$PROF" ] && echo "  ✓ Profile создан" || { echo "  ✗ FAIL: Profile не создан"; FAILED=1; }; \
	[ -f "$$WM"   ] && echo "  ✓ WM создан"      || { echo "  ✗ FAIL: WM не создан";      FAILED=1; }; \
	[ -f "$$LTM"  ] && echo "  ✓ LTM создан"     || { echo "  ✗ FAIL: LTM не создан";     FAILED=1; }; \
	WM_FACTS=$$(python3 -c "import json; d=json.load(open('$$WM')); print(len(d.get('facts',{})))" 2>/dev/null || echo 0); \
	if [ "$$WM_FACTS" -gt "0" ]; then echo "  ✓ WM содержит $$WM_FACTS фактов"; \
	else echo "  ✗ FAIL: WM пустой после 3 ходов с --memory-update"; FAILED=1; fi; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then echo "  Result: PASS — Profile + STM + WM + LTM работают в связке"; \
	else echo "  Result: FAIL"; exit 1; fi; \
	echo "=========================================="

run-profile-ai-critic: $(BINARY_NAME) check-env ## [Profile-IT] AI-critic (Maria): оценка ответов LLM, все слои памяти (Day10 сценарий)
	@PROF="$(PROFILE_AI_CRITIC_FILE)"; \
	STM="$(CRITIC_STM_FILE)"; WM="$(CRITIC_WM_FILE)"; LTM="$(CRITIC_LTM_FILE)"; \
	rm -f "$$PROF" "$$STM" "$${STM%.json}.stats.json" "$$WM" "$$LTM"; \
	echo "=========================================="; \
	echo "  Profile Integration Test: AI Critic"; \
	echo "  Persona: Maria — AI critic, structured response evaluation"; \
	echo "  Source: Day10/Day11 system_critic.txt evaluation criteria"; \
	echo "  Layers: Profile + STM + WM (--memory-update) + LTM (--memory-update)"; \
	echo "  Profile: $$PROF"; \
	echo "  STM:     $$STM"; \
	echo "  WM:      $$WM"; \
	echo "  LTM:     $$LTM"; \
	echo "=========================================="; echo ""; \
	\
	echo "--- Step 1: создать профиль AI-критика ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "Maria"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=russian" \
		--profile-set "format=structured-markdown" \
		--profile-set "style=analytical" \
		--profile-set "expertise=ai-critic" \
		--profile-set "output=verdicts-with-quotes"; \
	echo ""; ./$(BINARY_NAME) --profile "$$PROF" --profile-list; echo ""; \
	\
	echo "--- Turn 1: установить роль критика в WM и LTM ---"; \
	./$(BINARY_NAME) \
		--query "Моя задача — оценивать ответы языковых моделей по трём критериям из Day10: полнота, качество изложения, эффективность токенов. Подтверди понимание." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 2: первая оценка (сценарий из Day10 system_critic.txt) ---"; \
	RESP_FILE=$$(mktemp); \
	./$(BINARY_NAME) \
		--query "Оцени два ответа на один вопрос. Ответ A: 'Кот сидел на подоконнике и смотрел в окно на улицу.' Ответ B: 'Животное находилось у оконного проёма.' По критериям: полнота, качество, эффективность токенов." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > "$$RESP_FILE"; \
	cat "$$RESP_FILE"; echo ""; \
	\
	echo "--- Turn 3 (свежий STM): recall из WM + LTM + Profile ---"; \
	FRESH_STM=$$(mktemp /tmp/critic_recall_XXXXXX.json); \
	./$(BINARY_NAME) \
		--query "Кто ты? Опиши свою роль, методологию и по каким критериям ты оцениваешь ответы." \
		--history "$$FRESH_STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$FRESH_STM" "$${FRESH_STM%.json}.stats.json"; \
	echo ""; \
	\
	echo "--- WM contents (задача критика) ---"; \
	python3 -c "import json; d=json.load(open('$$WM')); [print(f'  {k}: {v}') for k,v in d.get('facts',{}).items()]" 2>/dev/null || echo "  (не найден)"; \
	echo ""; \
	\
	FAILED=0; \
	RESP_CONTENT=$$(cat "$$RESP_FILE"); rm -f "$$RESP_FILE"; \
	if echo "$$RESP_CONTENT" | grep -qi "полнота\|## 1"; then \
		echo "  ✓ Ответ содержит структурированную оценку с заголовками"; \
	else \
		echo "  ✗ FAIL: структурированные заголовки не найдены в оценке"; FAILED=1; \
	fi; \
	[ -f "$$PROF" ] && echo "  ✓ Profile создан" || { echo "  ✗ FAIL: Profile не создан"; FAILED=1; }; \
	[ -f "$$WM"   ] && echo "  ✓ WM создан"      || { echo "  ✗ FAIL: WM не создан";      FAILED=1; }; \
	[ -f "$$LTM"  ] && echo "  ✓ LTM создан"     || { echo "  ✗ FAIL: LTM не создан";     FAILED=1; }; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then echo "  Result: PASS — Profile + STM + WM + LTM работают в связке"; \
	else echo "  Result: FAIL"; exit 1; fi; \
	echo "=========================================="

run-profile-data-sci: $(BINARY_NAME) check-env ## [Profile-IT] Data-sci (Alex): ML-эксперимент, все слои памяти
	@PROF="$(PROFILE_DATA_SCI_FILE)"; \
	STM="$(DSCI_STM_FILE)"; WM="$(DSCI_WM_FILE)"; LTM="$(DSCI_LTM_FILE)"; \
	rm -f "$$PROF" "$$STM" "$${STM%.json}.stats.json" "$$WM" "$$LTM"; \
	echo "=========================================="; \
	echo "  Profile Integration Test: Data Scientist"; \
	echo "  Persona: Alex — ML engineer, binary classification"; \
	echo "  Layers: Profile + STM + WM (--memory-update) + LTM (--memory-update)"; \
	echo "  Profile: $$PROF"; \
	echo "  STM:     $$STM"; \
	echo "  WM:      $$WM"; \
	echo "  LTM:     $$LTM"; \
	echo "=========================================="; echo ""; \
	\
	echo "--- Step 1: создать профиль data scientist ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "Alex"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=english" \
		--profile-set "format=bullet-points" \
		--profile-set "style=technical" \
		--profile-set "expertise=ml-engineer" \
		--profile-set "constraints=scikit-learn-only" \
		--profile-set "tools=Python/pandas/scikit-learn"; \
	echo ""; ./$(BINARY_NAME) --profile "$$PROF" --profile-list; echo ""; \
	\
	echo "--- Turn 1: описать задачу и текущее состояние ---"; \
	./$(BINARY_NAME) \
		--query "My task: binary classification on tabular data. Baseline LogisticRegression F1=0.72. Confirm you understand the task." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 2: первый шаг улучшения ---"; \
	./$(BINARY_NAME) \
		--query "What polynomial features should I generate? Give me the exact scikit-learn code snippet." \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 3: результат и следующий шаг ---"; \
	./$(BINARY_NAME) \
		--query "Polynomial features gave F1=0.74. Small gain. Compare the next two options: RFE feature selection vs class-weight balancing. Which should I try first?" \
		--history "$$STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--memory-update --show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 4 (свежий STM): recall из WM + LTM + Profile ---"; \
	FRESH_STM=$$(mktemp /tmp/dsci_recall_XXXXXX.json); \
	./$(BINARY_NAME) \
		--query "Summarize my ML experiment: what is my task, baseline score, what did I try, current best result, and what is the next planned step?" \
		--history "$$FRESH_STM" --memory-wm "$$WM" --memory-ltm "$$LTM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$FRESH_STM" "$${FRESH_STM%.json}.stats.json"; \
	echo ""; \
	\
	echo "--- WM contents ---"; \
	python3 -c "import json; d=json.load(open('$$WM')); [print(f'  {k}: {v}') for k,v in d.get('facts',{}).items()]" 2>/dev/null || echo "  (не найден)"; \
	echo ""; \
	echo "--- LTM contents ---"; \
	python3 -c "import json; d=json.load(open('$$LTM')); [print(f'  {k}: {v}') for k,v in d.get('entries',{}).items()]" 2>/dev/null || echo "  (не найден)"; \
	echo ""; \
	\
	FAILED=0; \
	[ -f "$$PROF" ] && echo "  ✓ Profile создан" || { echo "  ✗ FAIL: Profile не создан"; FAILED=1; }; \
	[ -f "$$WM"   ] && echo "  ✓ WM создан"      || { echo "  ✗ FAIL: WM не создан";      FAILED=1; }; \
	[ -f "$$LTM"  ] && echo "  ✓ LTM создан"     || { echo "  ✗ FAIL: LTM не создан";     FAILED=1; }; \
	WM_FACTS=$$(python3 -c "import json; d=json.load(open('$$WM')); print(len(d.get('facts',{})))" 2>/dev/null || echo 0); \
	if [ "$$WM_FACTS" -gt "0" ]; then echo "  ✓ WM содержит $$WM_FACTS фактов"; \
	else echo "  ✗ FAIL: WM пустой после 3 ходов с --memory-update"; FAILED=1; fi; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then echo "  Result: PASS — Profile + STM + WM + LTM работают в связке"; \
	else echo "  Result: FAIL"; exit 1; fi; \
	echo "=========================================="

run-profile-all: $(BINARY_NAME) check-env ## [Profile-IT] Запустить все 3 профильных интеграционных теста последовательно
	@echo "=========================================="; \
	echo "  Multi-Profile Integration Tests (Day12)"; \
	echo "  Персоны: Go-developer | AI-critic | Data-scientist"; \
	echo "  Каждый тест: Profile + STM + WM + LTM (все 4 слоя)"; \
	echo "=========================================="; echo ""; \
	FAILURES=""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [1/3] Go Developer — Andrey (Day11 сценарий)"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-profile-go-dev || FAILURES="$$FAILURES go-dev"; \
	echo ""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [2/3] AI Critic — Maria (Day10 сценарий)"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-profile-ai-critic || FAILURES="$$FAILURES ai-critic"; \
	echo ""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [3/3] Data Scientist — Alex"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-profile-data-sci || FAILURES="$$FAILURES data-sci"; \
	echo ""; \
	\
	echo "=========================================="; \
	if [ -z "$$FAILURES" ]; then \
		echo "  Итог: 3/3 PASSED — все профили работают в единой связке"; \
	else \
		echo "  Итог: FAILED —$$FAILURES"; exit 1; \
	fi; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Day12: Profile-only integration tests (только профиль, без WM/LTM)
# ---------------------------------------------------------------------------

run-profile-only-go-dev: $(BINARY_NAME) check-env ## [Profile-Only] Go-dev (Andrey): только профиль, без WM/LTM
	@PROF="$(PONLY_GO_DEV_FILE)"; STM="$(PONLY_GO_DEV_STM)"; \
	rm -f "$$PROF" "$$STM" "$${STM%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Profile-Only Test: Go Developer"; \
	echo "  Persona: Andrey — Golang dev, Telegram bot"; \
	echo "  Layers: Profile only (no WM, no LTM, no --memory-update)"; \
	echo "  Profile: $$PROF"; \
	echo "  STM:     $$STM"; \
	echo "=========================================="; echo ""; \
	\
	echo "--- Step 1: создать профиль ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "Andrey"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=russian" \
		--profile-set "style=concise" \
		--profile-set "format=code-first" \
		--profile-set "expertise=golang-developer" \
		--profile-set "constraints=standard-library-only"; \
	echo ""; ./$(BINARY_NAME) --profile "$$PROF" --profile-list; echo ""; \
	\
	echo "--- Turn 1: задача проекта ---"; \
	./$(BINARY_NAME) \
		--query "Мне необходимо разработать telegram-bot. Бот должен повторять отправлённое ему сообщение 10 раз с чередованием регистра первой буквы сообщения." \
		--history "$$STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 2: новое требование ---"; \
	./$(BINARY_NAME) \
		--query "Нужно ещё выделять повторяемый текст разными цветами." \
		--history "$$STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 3 (свежий STM): recall из профиля ---"; \
	FRESH_STM=$$(mktemp /tmp/ponly_godev_recall_XXXXXX.json); \
	./$(BINARY_NAME) \
		--query "Что ты знаешь обо мне? Опиши мой стиль работы и предпочтения." \
		--history "$$FRESH_STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$FRESH_STM" "$${FRESH_STM%.json}.stats.json"; \
	echo ""; \
	\
	FAILED=0; \
	[ -f "$$PROF" ] && echo "  ✓ Profile создан" || { echo "  ✗ FAIL: Profile не создан"; FAILED=1; }; \
	PROF_SIZE=$$(wc -c < "$$PROF" 2>/dev/null || echo 0); \
	if [ "$$PROF_SIZE" -gt "0" ]; then echo "  ✓ Profile непустой ($$PROF_SIZE байт)"; \
	else echo "  ✗ FAIL: Profile пустой"; FAILED=1; fi; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then echo "  Result: PASS — Profile работает без слоёв памяти"; \
	else echo "  Result: FAIL"; exit 1; fi; \
	echo "=========================================="

run-profile-only-ai-critic: $(BINARY_NAME) check-env ## [Profile-Only] AI-critic (Maria): только профиль, без WM/LTM
	@PROF="$(PONLY_AI_CRITIC_FILE)"; STM="$(PONLY_CRITIC_STM)"; \
	rm -f "$$PROF" "$$STM" "$${STM%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Profile-Only Test: AI Critic"; \
	echo "  Persona: Maria — AI critic, structured response evaluation"; \
	echo "  Layers: Profile only (no WM, no LTM, no --memory-update)"; \
	echo "  Profile: $$PROF"; \
	echo "  STM:     $$STM"; \
	echo "=========================================="; echo ""; \
	\
	echo "--- Step 1: создать профиль AI-критика ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "Maria"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=russian" \
		--profile-set "format=structured-markdown" \
		--profile-set "style=analytical" \
		--profile-set "output=verdicts-with-quotes"; \
	echo ""; ./$(BINARY_NAME) --profile "$$PROF" --profile-list; echo ""; \
	\
	echo "--- Turn 1: оценка двух ответов (сценарий из Day10 system_critic.txt) ---"; \
	RESP_FILE=$$(mktemp); \
	./$(BINARY_NAME) \
		--query "Оцени два ответа на один вопрос. Ответ A: 'Кот сидел на подоконнике и смотрел в окно на улицу.' Ответ B: 'Животное находилось у оконного проёма.' По критериям: полнота, качество, эффективность токенов." \
		--history "$$STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG) > "$$RESP_FILE"; \
	cat "$$RESP_FILE"; echo ""; \
	\
	echo "--- Turn 2 (свежий STM): recall роли из профиля ---"; \
	FRESH_STM=$$(mktemp /tmp/ponly_critic_recall_XXXXXX.json); \
	./$(BINARY_NAME) \
		--query "Кто ты? Опиши свою роль и методологию оценки ответов." \
		--history "$$FRESH_STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$FRESH_STM" "$${FRESH_STM%.json}.stats.json"; \
	echo ""; \
	\
	FAILED=0; \
	RESP_CONTENT=$$(cat "$$RESP_FILE"); rm -f "$$RESP_FILE"; \
	if echo "$$RESP_CONTENT" | grep -qi "полнота\|## 1"; then \
		echo "  ✓ Ответ содержит структурированную оценку с заголовками"; \
	else \
		echo "  ✗ FAIL: структурированные заголовки не найдены в оценке"; FAILED=1; \
	fi; \
	[ -f "$$PROF" ] && echo "  ✓ Profile создан" || { echo "  ✗ FAIL: Profile не создан"; FAILED=1; }; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then echo "  Result: PASS — Profile работает без слоёв памяти"; \
	else echo "  Result: FAIL"; exit 1; fi; \
	echo "=========================================="

run-profile-only-data-sci: $(BINARY_NAME) check-env ## [Profile-Only] Data-sci (Alex): только профиль, без WM/LTM
	@PROF="$(PONLY_DATA_SCI_FILE)"; STM="$(PONLY_DSCI_STM)"; \
	rm -f "$$PROF" "$$STM" "$${STM%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Profile-Only Test: Data Scientist"; \
	echo "  Persona: Alex — ML engineer, binary classification"; \
	echo "  Layers: Profile only (no WM, no LTM, no --memory-update)"; \
	echo "  Profile: $$PROF"; \
	echo "  STM:     $$STM"; \
	echo "=========================================="; echo ""; \
	\
	echo "--- Step 1: создать профиль data scientist ---"; \
	./$(BINARY_NAME) --profile "$$PROF" --profile-name "Alex"; \
	./$(BINARY_NAME) --profile "$$PROF" \
		--profile-set "language=english" \
		--profile-set "format=bullet-points" \
		--profile-set "style=technical" \
		--profile-set "expertise=ml-engineer" \
		--profile-set "constraints=scikit-learn-only" \
		--profile-set "tools=Python/pandas/scikit-learn"; \
	echo ""; ./$(BINARY_NAME) --profile "$$PROF" --profile-list; echo ""; \
	\
	echo "--- Turn 1: описать задачу ---"; \
	./$(BINARY_NAME) \
		--query "My task: binary classification on tabular data. Baseline LogisticRegression F1=0.72. Confirm you understand the task." \
		--history "$$STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 2: запрос кода ---"; \
	./$(BINARY_NAME) \
		--query "What polynomial features should I generate? Give me the exact scikit-learn code snippet." \
		--history "$$STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG) > /dev/null; \
	echo ""; \
	\
	echo "--- Turn 3 (свежий STM): recall из профиля ---"; \
	FRESH_STM=$$(mktemp /tmp/ponly_dsci_recall_XXXXXX.json); \
	./$(BINARY_NAME) \
		--query "Who am I and what are my technical preferences?" \
		--history "$$FRESH_STM" --profile "$$PROF" \
		--show-cost $(DEBUG_FLAG); \
	rm -f "$$FRESH_STM" "$${FRESH_STM%.json}.stats.json"; \
	echo ""; \
	\
	FAILED=0; \
	[ -f "$$PROF" ] && echo "  ✓ Profile создан" || { echo "  ✗ FAIL: Profile не создан"; FAILED=1; }; \
	PROF_SIZE=$$(wc -c < "$$PROF" 2>/dev/null || echo 0); \
	if [ "$$PROF_SIZE" -gt "0" ]; then echo "  ✓ Profile непустой ($$PROF_SIZE байт)"; \
	else echo "  ✗ FAIL: Profile пустой"; FAILED=1; fi; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then echo "  Result: PASS — Profile работает без слоёв памяти"; \
	else echo "  Result: FAIL"; exit 1; fi; \
	echo "=========================================="

run-profile-only-all: $(BINARY_NAME) check-env ## [Profile-Only] Запустить все 3 profile-only теста последовательно
	@echo "=========================================="; \
	echo "  Profile-Only Integration Tests (Day12)"; \
	echo "  Персоны: Go-developer | AI-critic | Data-scientist"; \
	echo "  Только Profile layer (без WM, LTM, --memory-update)"; \
	echo "=========================================="; echo ""; \
	FAILURES=""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [1/3] Go Developer — Andrey"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-profile-only-go-dev || FAILURES="$$FAILURES go-dev"; \
	echo ""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [2/3] AI Critic — Maria"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-profile-only-ai-critic || FAILURES="$$FAILURES ai-critic"; \
	echo ""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [3/3] Data Scientist — Alex"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-profile-only-data-sci || FAILURES="$$FAILURES data-sci"; \
	echo ""; \
	\
	echo "=========================================="; \
	if [ -z "$$FAILURES" ]; then \
		echo "  Итог: 3/3 PASSED — профили работают без слоёв памяти"; \
	else \
		echo "  Итог: FAILED —$$FAILURES"; exit 1; \
	fi; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Day14: Invariants management targets
# ---------------------------------------------------------------------------

run-invariants-init: ## [Invariants] Create a sample invariants file (INVARIANTS_FILE=)
	@if [ -z "$(INVARIANTS_FILE)" ]; then \
		echo "Error: set INVARIANTS_FILE=/path/to/invariants.md"; \
		echo "Example: make run-invariants-init INVARIANTS_FILE=/tmp/my_project.invariants.md"; \
		exit 1; \
	fi; \
	printf '# Project Invariants\n\nThese are absolute constraints the agent must never violate.\nEdit this file freely — it is plain Markdown.\n\n## Architecture\n- (describe architecture constraints, e.g. "Clean Architecture: domain layer must not import adapter layer")\n\n## Technology Stack\n- (describe stack constraints, e.g. "Go standard library only — no external HTTP clients")\n- (e.g. "PostgreSQL only — no other storage engines")\n\n## Business Rules\n- (describe business rules, e.g. "All monetary calculations must use integer cents, never float64")\n- (e.g. "Every public API endpoint must require authentication")\n\n## Other Constraints\n- (any other invariants)\n' \
		> "$(INVARIANTS_FILE)"; \
	echo "Sample invariants file created: $(INVARIANTS_FILE)"; \
	echo "Edit it to describe your actual constraints, then start a session with:"; \
	echo "  make run-interactive INVARIANTS_FILE=$(INVARIANTS_FILE) AGENT_PROFILE_FILE=..."

run-invariants-show: ## [Invariants] Print the active invariants file (INVARIANTS_FILE=)
	@if [ -z "$(INVARIANTS_FILE)" ]; then \
		echo "Error: set INVARIANTS_FILE=/path/to/invariants.md"; \
		exit 1; \
	fi; \
	if [ ! -f "$(INVARIANTS_FILE)" ]; then \
		echo "File not found: $(INVARIANTS_FILE)"; \
		echo "Create it with: make run-invariants-init INVARIANTS_FILE=$(INVARIANTS_FILE)"; \
		exit 1; \
	fi; \
	echo "=========================================="; \
	echo "  Invariants: $(INVARIANTS_FILE)"; \
	echo "=========================================="; \
	cat "$(INVARIANTS_FILE)"; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Day16: MCP integration tests
# ---------------------------------------------------------------------------

run-mcp-session: $(BINARY_NAME) check-env ## [MCP] Start interactive agent with MCP — fresh session, auto-registers filesystem server (MCP_FS_ROOT=, MCP_SESSION_PROF=)
	@HIST="$(MCP_SESSION_HIST)"; \
	CFG="$(MCP_SESSION_CFG)"; \
	PROF="$(MCP_SESSION_PROF)"; \
	FSROOT="$(MCP_FS_ROOT)"; \
	rm -f "$$HIST" "$${HIST%.json}.task.json" "$${HIST%.json}.stats.json"; \
	rm -f "$$CFG"; \
	echo "=========================================="; \
	echo "  MCP Interactive Session  (Day $(DAY_NUM))"; \
	echo "  Fresh session each run"; \
	echo "=========================================="; \
	echo "  History:    $$HIST"; \
	echo "  MCP config: $$CFG"; \
	echo "  Model:      $(LLM_MODEL)"; \
	echo "  Provider:   $(LLM_PROVIDER)"; \
	if [ -n "$$PROF" ]; then echo "  Profile:    $$PROF"; fi; \
	echo ""; \
	if command -v npx >/dev/null 2>&1; then \
		echo "  Auto-registering filesystem MCP server (root: $$FSROOT)..."; \
		./$(BINARY_NAME) --mcp-config "$$CFG" \
			--mcp-add --mcp-name filesystem --mcp-type stdio \
			--mcp-command npx \
			--mcp-args "-y @modelcontextprotocol/server-filesystem $$FSROOT" 2>&1; \
		echo ""; \
	else \
		echo "  ⚠ npx not found — filesystem MCP server not registered."; \
		echo "    Install Node.js to enable it, or add servers manually with /mcp-add."; \
		echo ""; \
	fi; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then \
		echo "--- Active profile ---"; cat "$$PROF"; echo ""; \
	fi; \
	echo "=========================================="; \
	echo "  MCP slash commands (type at any prompt):"; \
	echo ""; \
	echo "  /mcp-list                           list configured servers"; \
	echo "  /mcp-tools                          list tools from all servers"; \
	echo "  /mcp-tools <name>                   list tools from one server"; \
	echo "  /mcp-add stdio <name> <cmd> [args]  add stdio server"; \
	echo "  /mcp-add sse   <name> <url>         add SSE server"; \
	echo "  /mcp-remove <name>                  remove a server"; \
	echo ""; \
	echo "  FSM commands:"; \
	echo "  /yes  — approve  |  /no — reject  |  Enter — pause  |  /exit — quit"; \
	echo "=========================================="; echo ""; \
	PROF_FLAG=""; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then PROF_FLAG="--profile $$PROF"; fi; \
	./$(BINARY_NAME) \
		--interactive \
		--history "$$HIST" \
		--mcp-config "$$CFG" \
		--history-limit 50 \
		--show-cost \
		$$PROF_FLAG \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)

run-mcp-session-persist: $(BINARY_NAME) check-env ## [MCP] Start interactive agent with MCP — keeps history across runs (MCP_FS_ROOT=, MCP_SESSION_PROF=)
	@HIST="$(MCP_SESSION_HIST)"; \
	CFG="$(MCP_SESSION_CFG)"; \
	PROF="$(MCP_SESSION_PROF)"; \
	FSROOT="$(MCP_FS_ROOT)"; \
	echo "=========================================="; \
	echo "  MCP Interactive Session  (Day $(DAY_NUM))"; \
	echo "  Persistent — history kept across runs"; \
	echo "=========================================="; \
	echo "  History:    $$HIST"; \
	echo "  MCP config: $$CFG"; \
	echo "  Model:      $(LLM_MODEL)"; \
	echo "  Provider:   $(LLM_PROVIDER)"; \
	if [ -n "$$PROF" ]; then echo "  Profile:    $$PROF"; fi; \
	echo ""; \
	if command -v npx >/dev/null 2>&1 && [ ! -f "$$CFG" ]; then \
		echo "  First run: auto-registering filesystem MCP server (root: $$FSROOT)..."; \
		./$(BINARY_NAME) --mcp-config "$$CFG" \
			--mcp-add --mcp-name filesystem --mcp-type stdio \
			--mcp-command npx \
			--mcp-args "-y @modelcontextprotocol/server-filesystem $$FSROOT" 2>&1; \
		echo ""; \
	elif [ -f "$$CFG" ]; then \
		echo "  MCP config already exists — showing registered servers:"; \
		./$(BINARY_NAME) --mcp-config "$$CFG" --mcp-list 2>/dev/null; \
		echo ""; \
	else \
		echo "  ⚠ npx not found — no filesystem server registered."; \
		echo "    Add servers manually with /mcp-add inside the session."; \
		echo ""; \
	fi; \
	echo "  MCP slash commands: /mcp-list  /mcp-tools [name]  /mcp-add  /mcp-remove"; \
	echo "  FSM commands: /yes  /no  Enter=pause  /exit"; \
	echo "=========================================="; echo ""; \
	PROF_FLAG=""; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then PROF_FLAG="--profile $$PROF"; fi; \
	./$(BINARY_NAME) \
		--interactive \
		--history "$$HIST" \
		--mcp-config "$$CFG" \
		--history-limit 50 \
		--show-cost \
		$$PROF_FLAG \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)

run-mcp-add-list-remove: $(BINARY_NAME) ## [MCP] Test 1: add / list / remove — YAML config round-trip (no API key)
	@CFG="$(MCP_CONFIG_FILE)"; \
	rm -f "$$CFG"; \
	echo "=========================================="; \
	echo "  MCP Test 1: add / list / remove"; \
	echo "  Config: $$CFG"; \
	echo "=========================================="; echo ""; \
	FAILED=0; \
	\
	echo "--- [1] add stdio server ---"; \
	./$(BINARY_NAME) --mcp-config "$$CFG" \
		--mcp-add --mcp-name filesystem --mcp-type stdio \
		--mcp-command npx --mcp-args "-y @modelcontextprotocol/server-filesystem /tmp" 2>&1; \
	if [ -f "$$CFG" ]; then \
		echo "  ✓ YAML config created"; \
	else \
		echo "  ✗ FAIL: YAML config not created"; FAILED=$$((FAILED+1)); \
	fi; \
	if grep -q "filesystem" "$$CFG" 2>/dev/null; then \
		echo "  ✓ server 'filesystem' present in YAML"; \
	else \
		echo "  ✗ FAIL: server 'filesystem' missing from YAML"; FAILED=$$((FAILED+1)); \
	fi; \
	if grep -q "stdio" "$$CFG" 2>/dev/null; then \
		echo "  ✓ type 'stdio' recorded in YAML"; \
	else \
		echo "  ✗ FAIL: type 'stdio' missing from YAML"; FAILED=$$((FAILED+1)); \
	fi; \
	echo ""; \
	\
	echo "--- [2] add SSE server ---"; \
	./$(BINARY_NAME) --mcp-config "$$CFG" \
		--mcp-add --mcp-name remote-srv --mcp-type sse \
		--mcp-url "http://localhost:9999/sse" 2>&1; \
	if grep -q "remote-srv" "$$CFG" 2>/dev/null; then \
		echo "  ✓ server 'remote-srv' present in YAML"; \
	else \
		echo "  ✗ FAIL: server 'remote-srv' missing from YAML"; FAILED=$$((FAILED+1)); \
	fi; \
	echo ""; \
	\
	echo "--- [3] list: expect 2 servers ---"; \
	LIST_OUT=$$(./$(BINARY_NAME) --mcp-config "$$CFG" --mcp-list 2>/dev/null); \
	echo "$$LIST_OUT"; \
	COUNT=$$(echo "$$LIST_OUT" | grep -c "Name:" || true); \
	if [ "$$COUNT" = "2" ]; then \
		echo "  ✓ listed 2 servers"; \
	else \
		echo "  ✗ FAIL: expected 2 servers, got $$COUNT"; FAILED=$$((FAILED+1)); \
	fi; \
	echo ""; \
	\
	echo "--- [4] duplicate add: must fail ---"; \
	./$(BINARY_NAME) --mcp-config "$$CFG" \
		--mcp-add --mcp-name filesystem --mcp-type stdio \
		--mcp-command npx 2>&1 | grep -q "already exists" \
	&& echo "  ✓ duplicate add rejected" \
	|| { echo "  ✗ FAIL: duplicate add not rejected"; FAILED=$$((FAILED+1)); }; \
	echo ""; \
	\
	echo "--- [5] remove stdio server ---"; \
	./$(BINARY_NAME) --mcp-config "$$CFG" --mcp-remove filesystem 2>&1; \
	if ! grep -q "filesystem" "$$CFG" 2>/dev/null; then \
		echo "  ✓ 'filesystem' removed from YAML"; \
	else \
		echo "  ✗ FAIL: 'filesystem' still present in YAML after remove"; FAILED=$$((FAILED+1)); \
	fi; \
	echo ""; \
	\
	echo "--- [6] list: expect 1 server (SSE only) ---"; \
	LIST_OUT=$$(./$(BINARY_NAME) --mcp-config "$$CFG" --mcp-list 2>/dev/null); \
	echo "$$LIST_OUT"; \
	if echo "$$LIST_OUT" | grep -q "remote-srv"; then \
		echo "  ✓ 'remote-srv' still listed"; \
	else \
		echo "  ✗ FAIL: 'remote-srv' missing after removing 'filesystem'"; FAILED=$$((FAILED+1)); \
	fi; \
	echo ""; \
	\
	echo "--- [7] remove non-existent server: must fail ---"; \
	./$(BINARY_NAME) --mcp-config "$$CFG" --mcp-remove no-such-server 2>&1 | grep -q "not found" \
	&& echo "  ✓ remove non-existent server returns error" \
	|| { echo "  ✗ FAIL: expected 'not found' error"; FAILED=$$((FAILED+1)); }; \
	echo ""; \
	\
	rm -f "$$CFG"; \
	echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then \
		echo "  Result: PASS (7/7 checks)"; \
	else \
		echo "  Result: $$FAILED check(s) FAILED"; exit 1; \
	fi; \
	echo "=========================================="

run-mcp-tools-filesystem: $(BINARY_NAME) ## [MCP] Test 2: list tools from filesystem MCP server (needs npx)
	@CFG="$(MCP_CONFIG_FILE)"; \
	rm -f "$$CFG"; \
	echo "=========================================="; \
	echo "  MCP Test 2: tool discovery via filesystem server"; \
	echo "  Config: $$CFG"; \
	echo "=========================================="; echo ""; \
	\
	if ! command -v npx >/dev/null 2>&1; then \
		echo "  ⚠ SKIP: npx not found — install Node.js to run this test"; \
		exit 0; \
	fi; \
	echo "  npx found: $$(npx --version 2>/dev/null || echo unknown)"; \
	echo ""; \
	FAILED=0; \
	\
	echo "--- [1] register filesystem MCP server ---"; \
	./$(BINARY_NAME) --mcp-config "$$CFG" \
		--mcp-add --mcp-name fstest --mcp-type stdio \
		--mcp-command npx --mcp-args "-y @modelcontextprotocol/server-filesystem /tmp" 2>&1; \
	echo ""; \
	\
	echo "--- [2] list tools (connects to subprocess, no LLM call) ---"; \
	TOOLS_OUT=$$(./$(BINARY_NAME) --mcp-config "$$CFG" --mcp-tools fstest 2>/dev/null); \
	echo "$$TOOLS_OUT"; \
	echo ""; \
	\
	TOOL_COUNT=$$(echo "$$TOOLS_OUT" | grep -cE '^\s+[0-9]+\.' || true); \
	if [ "$$TOOL_COUNT" -ge 5 ]; then \
		echo "  ✓ $$TOOL_COUNT tools discovered (≥5 expected)"; \
	else \
		echo "  ✗ FAIL: expected ≥5 tools, got $$TOOL_COUNT"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	for REQUIRED_TOOL in read_file list_directory write_file; do \
		if echo "$$TOOLS_OUT" | grep -q "$$REQUIRED_TOOL"; then \
			echo "  ✓ tool '$$REQUIRED_TOOL' present"; \
		else \
			echo "  ✗ FAIL: expected tool '$$REQUIRED_TOOL' not found"; FAILED=$$((FAILED+1)); \
		fi; \
	done; \
	echo ""; \
	\
	echo "--- [3] list tools — unknown server: must fail ---"; \
	./$(BINARY_NAME) --mcp-config "$$CFG" --mcp-tools no-such-server 2>&1 | grep -qi "not found" \
	&& echo "  ✓ unknown server returns error" \
	|| { echo "  ✗ FAIL: expected 'not found' error for unknown server"; FAILED=$$((FAILED+1)); }; \
	echo ""; \
	\
	rm -f "$$CFG"; \
	echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then \
		echo "  Result: PASS"; \
	else \
		echo "  Result: $$FAILED check(s) FAILED"; exit 1; \
	fi; \
	echo "=========================================="

run-mcp-slash-test: $(BINARY_NAME) check-env ## [MCP] Test 3: /mcp-* slash commands in interactive agent (API key required)
	@CFG="$(MCP_CONFIG_FILE)"; \
	HIST="$(MCP_HIST_FILE)"; \
	rm -f "$$CFG" "$$HIST" "$${HIST%.json}.stats.json" "$${HIST%.json}.task.json"; \
	echo "=========================================="; \
	echo "  MCP Test 3: /mcp-* slash commands in interactive mode"; \
	echo "  Config: $$CFG"; \
	echo "  History: $$HIST"; \
	echo "=========================================="; echo ""; \
	FAILED=0; \
	\
	OUT=$$(mktemp); \
	printf '/mcp-list\n/mcp-add stdio myfs npx -y @modelcontextprotocol/server-filesystem /tmp\n/mcp-list\n/mcp-add sse remote http://localhost:9999/sse\n/mcp-list\n/mcp-remove myfs\n/mcp-list\n/mcp-remove remote\n/mcp-list\n/exit\n' | \
		./$(BINARY_NAME) --interactive \
			--history "$$HIST" \
			--mcp-config "$$CFG" \
			>"$$OUT" 2>&1; \
	echo "--- Agent output ---"; cat "$$OUT"; echo ""; echo "--------------------"; echo ""; \
	\
	if grep -q "No MCP servers configured" "$$OUT"; then \
		echo "  ✓ /mcp-list shows empty state when no servers"; \
	else \
		echo "  ✗ FAIL: expected empty-state message from /mcp-list"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	if grep -q "Server \"myfs\" added" "$$OUT"; then \
		echo "  ✓ /mcp-add stdio: server 'myfs' added"; \
	else \
		echo "  ✗ FAIL: 'myfs' add confirmation not found"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	if grep -q "Server \"remote\" added" "$$OUT"; then \
		echo "  ✓ /mcp-add sse: server 'remote' added"; \
	else \
		echo "  ✗ FAIL: 'remote' add confirmation not found"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	if grep -q "myfs" "$$OUT" && grep -q "remote" "$$OUT"; then \
		echo "  ✓ /mcp-list after add: both servers listed"; \
	else \
		echo "  ✗ FAIL: expected both 'myfs' and 'remote' in list"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	if grep -q "Server \"myfs\" removed" "$$OUT"; then \
		echo "  ✓ /mcp-remove 'myfs' confirmed"; \
	else \
		echo "  ✗ FAIL: 'myfs' remove confirmation not found"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	if ! grep -qF '[PLANNING]' "$$OUT"; then \
		echo "  ✓ no task was started (FSM not triggered by MCP commands)"; \
	else \
		echo "  ✗ FAIL: FSM was unexpectedly triggered"; FAILED=$$((FAILED+1)); \
	fi; \
	\
	if [ -f "$$CFG" ]; then \
		FINAL_SERVERS=$$(grep -c "name:" "$$CFG" 2>/dev/null || echo 0); \
		if [ "$$FINAL_SERVERS" = "0" ]; then \
			echo "  ✓ YAML config empty after all servers removed"; \
		else \
			echo "  ✗ FAIL: expected empty YAML, found $$FINAL_SERVERS entries"; FAILED=$$((FAILED+1)); \
		fi; \
	else \
		echo "  ✓ YAML config not created (no servers remain)"; \
	fi; \
	\
	rm -f "$$OUT" "$$CFG" "$$HIST" "$${HIST%.json}.stats.json" "$${HIST%.json}.task.json"; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then \
		echo "  Result: PASS (6/6 checks)"; \
	else \
		echo "  Result: $$FAILED check(s) FAILED"; exit 1; \
	fi; \
	echo "=========================================="

run-mcp-test: $(BINARY_NAME) ## [MCP] Run MCP config tests 1+2 (no API key needed)
	@echo "=========================================="; \
	echo "  MCP Integration Tests (Day16)"; \
	echo "  Test 1: config YAML round-trip (add/list/remove)"; \
	echo "  Test 2: tool discovery via filesystem MCP server"; \
	echo "=========================================="; echo ""; \
	FAILURES=""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [1/2] Config management: add / list / remove"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-mcp-add-list-remove || FAILURES="$$FAILURES add-list-remove"; \
	echo ""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [2/2] Tool discovery: filesystem MCP server"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-mcp-tools-filesystem || FAILURES="$$FAILURES tools-filesystem"; \
	echo ""; \
	\
	echo "=========================================="; \
	if [ -z "$$FAILURES" ]; then \
		echo "  Итог: 2/2 PASSED"; \
	else \
		echo "  Итог: FAILED —$$FAILURES"; exit 1; \
	fi; \
	echo "=========================================="

run-mcp-all: $(BINARY_NAME) check-env ## [MCP] Run all MCP tests including interactive slash commands (API key required)
	@echo "=========================================="; \
	echo "  MCP All Tests (Day16)"; \
	echo "  Test 1: config YAML round-trip"; \
	echo "  Test 2: tool discovery (needs npx)"; \
	echo "  Test 3: /mcp-* slash commands (needs API key)"; \
	echo "=========================================="; echo ""; \
	FAILURES=""; \
	\
	$(MAKE) --no-print-directory run-mcp-test || FAILURES="$$FAILURES config-tests"; \
	echo ""; \
	\
	echo "══════════════════════════════════════════"; \
	echo "  [3/3] Slash commands in interactive mode"; \
	echo "══════════════════════════════════════════"; \
	$(MAKE) --no-print-directory run-mcp-slash-test || FAILURES="$$FAILURES slash-test"; \
	echo ""; \
	\
	echo "=========================================="; \
	if [ -z "$$FAILURES" ]; then \
		echo "  Итог: 3/3 PASSED"; \
	else \
		echo "  Итог: FAILED —$$FAILURES"; exit 1; \
	fi; \
	echo "=========================================="

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
	rm -f $(PROFILE_FILE)
	rm -f $(PROFILE_GO_DEV_FILE) $(PROFILE_AI_CRITIC_FILE) $(PROFILE_DATA_SCI_FILE)
	rm -f $(GODEV_STM_FILE) $(GODEV_STM_FILE:.json=.stats.json) $(GODEV_WM_FILE) $(GODEV_LTM_FILE)
	rm -f $(CRITIC_STM_FILE) $(CRITIC_STM_FILE:.json=.stats.json) $(CRITIC_WM_FILE) $(CRITIC_LTM_FILE)
	rm -f $(DSCI_STM_FILE) $(DSCI_STM_FILE:.json=.stats.json) $(DSCI_WM_FILE) $(DSCI_LTM_FILE)
	rm -f $(PONLY_GO_DEV_FILE) $(PONLY_AI_CRITIC_FILE) $(PONLY_DATA_SCI_FILE)
	rm -f $(PONLY_GO_DEV_STM) $(PONLY_GO_DEV_STM:.json=.stats.json)
	rm -f $(PONLY_CRITIC_STM) $(PONLY_CRITIC_STM:.json=.stats.json)
	rm -f $(PONLY_DSCI_STM) $(PONLY_DSCI_STM:.json=.stats.json)
	rm -f $(MCP_CONFIG_FILE) $(MCP_HIST_FILE) \
		$(MCP_HIST_FILE:.json=.stats.json) \
		$(MCP_HIST_FILE:.json=.task.json) \
		$(MCP_HIST_FILE:.json=.mcp.yaml)
	rm -f $(MCP_SESSION_HIST) $(MCP_SESSION_CFG) \
		$(MCP_SESSION_HIST:.json=.stats.json) \
		$(MCP_SESSION_HIST:.json=.task.json)
	$(GO) clean -testcache

install-deps: ## Install and tidy dependencies
	$(GO) mod download
	$(GO) mod tidy

# ---------------------------------------------------------------------------
# RAG — question → retrieve relevant chunks → combine with question → LLM
# Requires an index built by the `rag` component (../rag: `make run-index`)
# and a reachable embeddings endpoint (default: local Ollama nomic-embed-text).
# ---------------------------------------------------------------------------
RAG_DB          ?= ../rag/rag.db
RAG_QUESTION    ?= Какие бывают этапы выполнения очистки таблиц?
# Two-stage retrieval knobs: retrieve RAG_TOP_K chunks, optionally rerank them
# (RAG_RERANK=1) with a DEDICATED model (RAG_RERANK_MODEL, its own optional
# provider/URL/key), drop anything below RAG_THRESHOLD, keep RAG_TOP_K_FINAL.
RAG_TOP_K          ?= 10
RAG_RERANK         ?= 0
RAG_RERANK_MODEL   ?=
RAG_RERANK_MODE    ?= auto
RAG_RERANK_PROVIDER ?=
RAG_RERANK_URL     ?=
RAG_RERANK_KEY     ?=
RAG_THRESHOLD      ?= 0
RAG_TOP_K_FINAL    ?= 5
# Embeddings endpoint. Must use the SAME model the index was built with — the
# ../rag/rag.db index was built with nomic-embed-text-v2-moe (768 dims).
RAG_EMBED_URL   ?= http://127.0.0.1:1234
RAG_EMBED_MODEL ?= text-embedding-nomic-embed-text-v2-moe
RAG_EMBED_KEY   ?=
# Answer token budget for the --rag CLI targets. 0 = no limit (provider default).
# Reasoning models (e.g. local google/gemma-4-*) spend tokens thinking before they
# emit the answer, so a generous cap is needed or the reply comes back empty.
RAG_MAX_TOKENS  ?= 0
# Interactive RAG dialogue persistence: the chat history and the task memory
# (goal / clarified / constraints, saved to <history>.taskmem.json) are stored
# here so a session resumes. Set RAG_HISTORY_FILE="" to run stateless.
RAG_HISTORY_FILE ?= /tmp/rag_session_$(DAY).json
# go test -timeout for the LLM-driven RAG integration targets. Local reasoning
# models (e.g. google/gemma-4-*) run many slow answer+judge calls, so the local
# targets raise this well above the cloud default via LOCAL_TEST_TIMEOUT.
RAG_TEST_TIMEOUT ?= 900s
# Optional: run only the first N of the 10 control questions (empty = all 10).
# Handy with slow local models. Assertions scale to the reduced count.
RAG_MAX_QUESTIONS ?=

run-rag-eval: ## [RAG] Run the 10 control questions — retrieval check (add RUN_LLM=1 for the full pipeline)
	RAG_DB="$(RAG_DB)" \
	EMBED_URL="$(RAG_EMBED_URL)" \
	EMBED_MODEL="$(RAG_EMBED_MODEL)" \
	RAG_MAX_QUESTIONS="$(RAG_MAX_QUESTIONS)" \
	$(GO) test -tags integration -run TestRAGEval -v -timeout 300s .

# Grounded-answer test ALWAYS reranks with a dedicated model. These target-specific
# values are the day's tuned defaults; a command-line RAG_* still overrides them.
run-rag-grounded: RAG_RERANK_MODEL := cohere/rerank-4-fast
run-rag-grounded: RAG_TOP_K        := 20
run-rag-grounded: RAG_THRESHOLD     := 0.5
run-rag-grounded: RAG_TOP_K_FINAL   := 10
run-rag-grounded: check-env ## [RAG] 10 control questions → grounded answers (ALWAYS reranks); assert each has sources + quotes and the meaning matches (needs LLM + embeddings)
	RUN_LLM=1 \
	RAG_DB="$(RAG_DB)" \
	EMBED_URL="$(RAG_EMBED_URL)" \
	EMBED_MODEL="$(RAG_EMBED_MODEL)" \
	RAG_TOP_K="$(RAG_TOP_K)" \
	RAG_THRESHOLD="$(RAG_THRESHOLD)" \
	RAG_TOP_K_FINAL="$(RAG_TOP_K_FINAL)" \
	RAG_RERANK_MODEL="$(RAG_RERANK_MODEL)" \
	RAG_RERANK_MODE="$(RAG_RERANK_MODE)" \
	RAG_RERANK_PROVIDER="$(RAG_RERANK_PROVIDER)" \
	RAG_RERANK_URL="$(RAG_RERANK_URL)" \
	RAG_RERANK_KEY="$(RAG_RERANK_KEY)" \
	RAG_MAX_QUESTIONS="$(RAG_MAX_QUESTIONS)" \
	$(GO) test -tags integration -run TestRAGGroundedAnswers -v -timeout $(RAG_TEST_TIMEOUT) .

run-rag-compare: check-env ## [RAG] Compare the 10 control questions WITH vs WITHOUT RAG (needs LLM + embeddings)
	RUN_LLM=1 \
	RAG_DB="$(RAG_DB)" \
	EMBED_URL="$(RAG_EMBED_URL)" \
	EMBED_MODEL="$(RAG_EMBED_MODEL)" \
	RAG_MAX_QUESTIONS="$(RAG_MAX_QUESTIONS)" \
	$(GO) test -tags integration -run TestRAGCompare -v -timeout $(RAG_TEST_TIMEOUT) .

run-rag-rerank-compare: check-env ## [RAG] Compare the 10 control questions WITH vs WITHOUT reranker (RAG_RERANK_MODEL=, RAG_TOP_K=, RAG_THRESHOLD=, RAG_TOP_K_FINAL=)
	RUN_LLM=1 \
	RAG_DB="$(RAG_DB)" \
	EMBED_URL="$(RAG_EMBED_URL)" \
	EMBED_MODEL="$(RAG_EMBED_MODEL)" \
	RAG_TOP_K="$(RAG_TOP_K)" \
	RAG_THRESHOLD="$(RAG_THRESHOLD)" \
	RAG_TOP_K_FINAL="$(RAG_TOP_K_FINAL)" \
	RAG_RERANK_MODEL="$(RAG_RERANK_MODEL)" \
	RAG_RERANK_MODE="$(RAG_RERANK_MODE)" \
	RAG_RERANK_PROVIDER="$(RAG_RERANK_PROVIDER)" \
	RAG_RERANK_URL="$(RAG_RERANK_URL)" \
	RAG_RERANK_KEY="$(RAG_RERANK_KEY)" \
	RAG_MAX_QUESTIONS="$(RAG_MAX_QUESTIONS)" \
	$(GO) test -tags integration -run TestRAGRerankCompare -v -timeout $(RAG_TEST_TIMEOUT) .

run-rag: $(BINARY_NAME) check-env ## [RAG] Answer RAG_QUESTION grounded on RAG_DB (RAG_TOP_K=, RAG_RERANK=1, RAG_THRESHOLD=, RAG_TOP_K_FINAL=)
	@echo "=========================================="
	@echo "  RAG query  (Day $(DAY_NUM))"
	@echo "  Index:     $(RAG_DB)"
	@echo "  Question:  $(RAG_QUESTION)"
	@echo "  Retrieve:  $(RAG_TOP_K) → rerank=$(RAG_RERANK) threshold=$(RAG_THRESHOLD) → keep $(RAG_TOP_K_FINAL)"
	@echo "=========================================="
	./$(BINARY_NAME) \
		--query "$(RAG_QUESTION)" \
		--rag \
		$(if $(filter-out 0,$(RAG_MAX_TOKENS)),--max-tokens $(RAG_MAX_TOKENS),) \
		--rag-db "$(RAG_DB)" \
		--rag-top-k $(RAG_TOP_K) \
		$(if $(filter-out 0,$(RAG_RERANK)),--rag-rerank,) \
		$(if $(RAG_RERANK_MODEL),--rag-rerank-model "$(RAG_RERANK_MODEL)",) \
		--rag-rerank-mode "$(RAG_RERANK_MODE)" \
		$(if $(RAG_RERANK_PROVIDER),--rag-rerank-provider "$(RAG_RERANK_PROVIDER)",) \
		$(if $(RAG_RERANK_URL),--rag-rerank-url "$(RAG_RERANK_URL)",) \
		$(if $(RAG_RERANK_KEY),--rag-rerank-key "$(RAG_RERANK_KEY)",) \
		--rag-threshold $(RAG_THRESHOLD) \
		--rag-top-k-final $(RAG_TOP_K_FINAL) \
		--rag-embed-url "$(RAG_EMBED_URL)" \
		--rag-embed-model "$(RAG_EMBED_MODEL)" \
		$(if $(RAG_EMBED_KEY),--rag-embed-key "$(RAG_EMBED_KEY)",) \
		--history ""

# Interactive agent+RAG session: type a question, get a grounded answer (answer +
# sources + quotes); /exit or /quit to leave. Reranks by default with a dedicated
# model (the day's tuned values); every RAG_* knob is command-line overridable.
run-rag-interactive: RAG_RERANK       := 1
run-rag-interactive: RAG_RERANK_MODEL := cohere/rerank-4-fast
run-rag-interactive: RAG_TOP_K        := 20
run-rag-interactive: RAG_THRESHOLD     := 0.5
run-rag-interactive: RAG_TOP_K_FINAL   := 10
run-rag-interactive: $(BINARY_NAME) check-env ## [RAG] Interactive agent+RAG session (type questions → grounded answers with sources+quotes)
	@echo "=========================================="
	@echo "  RAG interactive  (Day $(DAY_NUM))"
	@echo "  Index:     $(RAG_DB)"
	@echo "  Retrieve:  $(RAG_TOP_K) → rerank=$(RAG_RERANK) threshold=$(RAG_THRESHOLD) → keep $(RAG_TOP_K_FINAL)"
	@echo "  History:   $(RAG_HISTORY_FILE)"
	@echo "  Task mem:  saved next to history (.taskmem.json): goal / clarified / constraints"
	@echo "  Type your questions; /exit or /quit to leave, /memory to show task memory."
	@echo "=========================================="
	./$(BINARY_NAME) \
		--interactive \
		--rag \
		$(if $(filter-out 0,$(RAG_MAX_TOKENS)),--max-tokens $(RAG_MAX_TOKENS),) \
		--rag-db "$(RAG_DB)" \
		--rag-top-k $(RAG_TOP_K) \
		$(if $(filter-out 0,$(RAG_RERANK)),--rag-rerank,) \
		$(if $(RAG_RERANK_MODEL),--rag-rerank-model "$(RAG_RERANK_MODEL)",) \
		--rag-rerank-mode "$(RAG_RERANK_MODE)" \
		$(if $(RAG_RERANK_PROVIDER),--rag-rerank-provider "$(RAG_RERANK_PROVIDER)",) \
		$(if $(RAG_RERANK_URL),--rag-rerank-url "$(RAG_RERANK_URL)",) \
		$(if $(RAG_RERANK_KEY),--rag-rerank-key "$(RAG_RERANK_KEY)",) \
		--rag-threshold $(RAG_THRESHOLD) \
		--rag-top-k-final $(RAG_TOP_K_FINAL) \
		--rag-embed-url "$(RAG_EMBED_URL)" \
		--rag-embed-model "$(RAG_EMBED_MODEL)" \
		$(if $(RAG_EMBED_KEY),--rag-embed-key "$(RAG_EMBED_KEY)",) \
		--history "$(RAG_HISTORY_FILE)"

# ---------------------------------------------------------------------------
# Local-only RAG integration tests — answers AND reranking on local models.
#   Main LLM:   google/gemma-4-26b-a4b               @ LM Studio  http://127.0.0.1:1234/v1
#   Embeddings: text-embedding-nomic-embed-text-v2-moe @ LM Studio  http://127.0.0.1:1234  (index model)
#   Reranker:   MedAIBase/Qwen3-VL-Reranker:2b        @ Ollama     http://127.0.0.1:11434/v1
#
# Ollama serves the reranker as a completion model (no Cohere-style /rerank route),
# so the rerank stage runs in CHAT mode (RAG_RERANK_MODE=chat) — auto would pick
# 'api' because the model name contains "rerank" and 404 on Ollama. Local endpoints
# ignore the API key; a non-empty dummy satisfies check-env.
#
# These *-local targets reuse the existing RAG test targets, overriding only the
# endpoints on the recursive make command line (command-line vars beat the cloud
# defaults, including each target's tuned RAG_* values, without touching them).
# Override any LOCAL_* below to point at a different local host/model.
# ---------------------------------------------------------------------------
LOCAL_LLM_PROVIDER    ?= openai
LOCAL_LLM_BASE_URL    ?= http://127.0.0.1:1234/v1
LOCAL_LLM_MODEL       ?= google/gemma-4-26b-a4b
LOCAL_LLM_API_KEY     ?= local
LOCAL_EMBED_URL       ?= http://127.0.0.1:1234
LOCAL_EMBED_MODEL     ?= text-embedding-nomic-embed-text-v2-moe
LOCAL_RERANK_MODEL    ?= MedAIBase/Qwen3-VL-Reranker:2b
LOCAL_RERANK_URL      ?= http://127.0.0.1:11434/v1
LOCAL_RERANK_MODE     ?= chat
LOCAL_RERANK_PROVIDER ?= openai
LOCAL_RERANK_KEY      ?= local

# Day29: Telegram-bot optimization test. Generation runs ONLY on the local model
# (LLM_* / LOCAL_LLM_*). The quality comparison uses a public judge model on
# openrouter. Supply JUDGE_API_KEY (or export OPENROUTER_API_KEY) to enable it.
JUDGE_PROVIDER        ?= openrouter
JUDGE_MODEL           ?= deepseek/deepseek-v4-flash
JUDGE_BASE_URL        ?=
JUDGE_API_KEY         ?= $(OPENROUTER_API_KEY)
TELEGRAM_OPT_TIMEOUT  ?= 1800s
# google/gemma-4-26b-a4b is a reasoning model — it thinks before answering, so the
# CLI --rag targets need a generous budget or content comes back empty (the
# integration tests already pass 4000 internally).
LOCAL_MAX_TOKENS      ?= 4000
# The chat reranker packs the whole retrieval pool into ONE prompt. Ollama loads
# MedAIBase/Qwen3-VL-Reranker with a 4096-token context by default (the model
# itself supports far more), so a 20-chunk pool (~12k tokens) 400s with
# "exceeds the available context size". ~6 chunks (~500 tokens each) fit safely.
# Raise these if you start Ollama with a larger num_ctx for the reranker.
LOCAL_RAG_TOP_K       ?= 6
LOCAL_RAG_TOP_K_FINAL ?= 5
# The local reasoning model is slow and each grounded question runs an answer
# call plus an LLM-judge call (plus a retry on transient errors), so the default
# 15-min go-test timeout is not enough. 40 min gives comfortable headroom.
LOCAL_TEST_TIMEOUT    ?= 2400s
# Run a subset of the 10 control questions to keep local runs quick. Set to 10
# (or empty) for the full set. Assertions scale to the reduced count.
LOCAL_MAX_QUESTIONS   ?= 4

# Overrides forwarded to every recursive make below. Rerank vars are harmless on
# the non-rerank targets (eval/compare) — those targets simply ignore them.
LOCAL_OVERRIDES = \
	LLM_PROVIDER="$(LOCAL_LLM_PROVIDER)" \
	LLM_BASE_URL="$(LOCAL_LLM_BASE_URL)" \
	LLM_MODEL="$(LOCAL_LLM_MODEL)" \
	LLM_API_KEY="$(LOCAL_LLM_API_KEY)" \
	RAG_EMBED_URL="$(LOCAL_EMBED_URL)" \
	RAG_EMBED_MODEL="$(LOCAL_EMBED_MODEL)" \
	RAG_RERANK_MODEL="$(LOCAL_RERANK_MODEL)" \
	RAG_RERANK_MODE="$(LOCAL_RERANK_MODE)" \
	RAG_RERANK_PROVIDER="$(LOCAL_RERANK_PROVIDER)" \
	RAG_RERANK_URL="$(LOCAL_RERANK_URL)" \
	RAG_RERANK_KEY="$(LOCAL_RERANK_KEY)" \
	RAG_MAX_TOKENS="$(LOCAL_MAX_TOKENS)" \
	RAG_TOP_K="$(LOCAL_RAG_TOP_K)" \
	RAG_TOP_K_FINAL="$(LOCAL_RAG_TOP_K_FINAL)" \
	RAG_TEST_TIMEOUT="$(LOCAL_TEST_TIMEOUT)" \
	RAG_MAX_QUESTIONS="$(LOCAL_MAX_QUESTIONS)"

run-rag-eval-local: ## [RAG][local] Retrieval check on the 10 control questions (local embeddings)
	@echo "[local] LLM=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  embed=$(LOCAL_EMBED_MODEL)@$(LOCAL_EMBED_URL)"
	@$(MAKE) run-rag-eval $(LOCAL_OVERRIDES)

run-rag-compare-local: ## [RAG][local] Compare WITH vs WITHOUT RAG on the local LLM (no rerank)
	@echo "[local] LLM=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  embed=$(LOCAL_EMBED_MODEL)@$(LOCAL_EMBED_URL)"
	@$(MAKE) run-rag-compare $(LOCAL_OVERRIDES)

run-rag-rerank-compare-local: ## [RAG][local] Compare WITH vs WITHOUT reranker — local LLM + local reranker (chat mode)
	@echo "[local] LLM=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  rerank=$(LOCAL_RERANK_MODEL)@$(LOCAL_RERANK_URL) (mode $(LOCAL_RERANK_MODE))"
	@$(MAKE) run-rag-rerank-compare $(LOCAL_OVERRIDES)

run-rag-grounded-local: ## [RAG][local] Grounded-answer test — local LLM + local reranker (chat mode)
	@echo "[local] LLM=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  rerank=$(LOCAL_RERANK_MODEL)@$(LOCAL_RERANK_URL) (mode $(LOCAL_RERANK_MODE))"
	@$(MAKE) run-rag-grounded $(LOCAL_OVERRIDES)

run-rag-local: ## [RAG][local] Answer RAG_QUESTION on the local LLM + local reranker (RAG_RERANK=1)
	@echo "[local] LLM=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  rerank=$(LOCAL_RERANK_MODEL)@$(LOCAL_RERANK_URL) (mode $(LOCAL_RERANK_MODE))"
	@$(MAKE) run-rag RAG_RERANK=1 $(LOCAL_OVERRIDES)

run-rag-interactive-local: ## [RAG][local] Interactive agent+RAG session — local LLM + local reranker (chat mode)
	@echo "[local] LLM=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  rerank=$(LOCAL_RERANK_MODEL)@$(LOCAL_RERANK_URL) (mode $(LOCAL_RERANK_MODE))"
	@$(MAKE) run-rag-interactive $(LOCAL_OVERRIDES)

# ---------------------------------------------------------------------------
# Day29: does optimization (params + profile + invariants) improve the answer?
# Runs the Telegram-bot task under 4 configs on the LOCAL model, then compares
# quality with the public openrouter judge. Report: telegram_opt_result.txt.
# ---------------------------------------------------------------------------
run-telegram-opt: check-env ## [Day29] 4-config Telegram-bot test on the current LLM_* model + JUDGE_* comparison
	@echo "[telegram-opt] gen=$(LLM_MODEL)@$(LLM_BASE_URL)  judge=$(JUDGE_MODEL)@$(JUDGE_PROVIDER)"
	@LLM_PROVIDER="$(LLM_PROVIDER)" LLM_BASE_URL="$(LLM_BASE_URL)" LLM_MODEL="$(LLM_MODEL)" LLM_API_KEY="$(LLM_API_KEY)" \
	JUDGE_PROVIDER="$(JUDGE_PROVIDER)" JUDGE_MODEL="$(JUDGE_MODEL)" JUDGE_BASE_URL="$(JUDGE_BASE_URL)" JUDGE_API_KEY="$(JUDGE_API_KEY)" \
	$(GO) test -tags integration -run TestTelegramBotOptimization -v -timeout $(TELEGRAM_OPT_TIMEOUT) .

run-telegram-opt-local: ## [Day29][local] Telegram-bot optimization test — local gemma generation + openrouter judge
	@echo "[local] gen=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  judge=$(JUDGE_MODEL)@$(JUDGE_PROVIDER)"
	@$(MAKE) run-telegram-opt \
		LLM_PROVIDER="$(LOCAL_LLM_PROVIDER)" LLM_BASE_URL="$(LOCAL_LLM_BASE_URL)" \
		LLM_MODEL="$(LOCAL_LLM_MODEL)" LLM_API_KEY="$(LOCAL_LLM_API_KEY)" \
		JUDGE_PROVIDER="$(JUDGE_PROVIDER)" JUDGE_MODEL="$(JUDGE_MODEL)" \
		JUDGE_BASE_URL="$(JUDGE_BASE_URL)" JUDGE_API_KEY="$(JUDGE_API_KEY)" \
		TELEGRAM_OPT_TIMEOUT="$(LOCAL_TEST_TIMEOUT)"

run-rag-tests-local: ## [RAG][local] Run all local RAG integration tests (eval → compare → rerank-compare → grounded)
	@echo "=========================================="
	@echo "  Local RAG integration suite (Day $(DAY_NUM))"
	@echo "=========================================="
	@$(MAKE) run-rag-eval-local
	@$(MAKE) run-rag-compare-local
	@$(MAKE) run-rag-rerank-compare-local
	@$(MAKE) run-rag-grounded-local
	@echo ""; echo "=== Local RAG suite complete ==="

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
	@echo "Day12 — User profile flags:"
	@echo "  --profile <path>            Profile file (.md, human-editable). Default: <history>.profile.md"
	@echo "  --profile-init              Interactive initialization dialog (no --query needed)"
	@echo "  --profile-name <name>       Set user name and exit"
	@echo "  --profile-set key=value     Set a preference (repeatable) and exit"
	@echo "  --profile-delete <key>      Delete a preference by key and exit"
	@echo "  --profile-list              Print current profile and exit"
	@echo ""
	@echo "Day12 profile demo targets:"
	@echo "  make run-profile-init       Interactive profile creation (reads stdin)"
	@echo "  make run-profile-set        Set name + example preferences non-interactively"
	@echo "  make run-profile-list       Print current profile"
	@echo "  make run-profile-delete     Delete a preference (KEY=<key>)"
	@echo "  make run-profile-demo       Full demo: set profile, query with profile injected"
	@echo ""
	@echo "Day12 multi-profile integration tests (все 4 слоя памяти):"
	@echo "  make run-profile-go-dev     Andrey — Golang/Telegram-bot (Day11 сценарий), все 4 слоя памяти"
	@echo "  make run-profile-ai-critic  Maria  — AI-критик (Day10 system_critic.txt), все 4 слоя памяти"
	@echo "  make run-profile-data-sci   Alex   — ML-эксперимент, все 4 слоя памяти"
	@echo "  make run-profile-all        Запустить все 3 теста последовательно (с итоговым PASS/FAIL)"
	@echo ""
	@echo "Day12 profile-only integration tests (только Profile layer, без WM/LTM):"
	@echo "  make run-profile-only-go-dev     Andrey — Golang/Telegram-bot, только профиль"
	@echo "  make run-profile-only-ai-critic  Maria  — AI-критик, только профиль"
	@echo "  make run-profile-only-data-sci   Alex   — ML-эксперимент, только профиль"
	@echo "  make run-profile-only-all        Запустить все 3 profile-only теста последовательно"
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
	@echo ""
	@echo "Day14 — Invariants (absolute constraints the agent must never violate):"
	@echo "  --invariants <path>         Invariants file (.md). Default: derived from --history"
	@echo "                              File is read-only by the agent — managed by the user."
	@echo "                              Active ONLY in --interactive mode (task state machine)."
	@echo "                              Invariants are embedded in PLANNING and EXECUTION prompts."
	@echo ""
	@echo "Invariant management targets:"
	@echo "  make run-invariants-init INVARIANTS_FILE=<path>   Create a sample invariants file"
	@echo "  make run-invariants-show INVARIANTS_FILE=<path>   Print the active invariants file"
	@echo ""
	@echo "Using invariants with interactive agent:"
	@echo "  make run-interactive INVARIANTS_FILE=/tmp/proj.invariants.md AGENT_PROFILE_FILE=..."
	@echo "  make run-interactive-manual INVARIANTS_FILE=/tmp/proj.invariants.md INTERACTIVE_PROFILE_FILE=..."
	@echo ""
	@echo "Invariants file format (plain Markdown, user-editable):"
	@echo "  # Project Invariants"
	@echo "  ## Architecture"
	@echo "  - Clean Architecture: no imports from adapter into domain"
	@echo "  ## Technology Stack"
	@echo "  - Go standard library only"
	@echo "  ## Business Rules"
	@echo "  - All monetary values must use integer cents"
	@echo ""
	@echo "Day16 — MCP (Model Context Protocol) flags:"
	@echo "  --mcp-config <path>       MCP config file (.yaml). Default: derived from --history"
	@echo "  --mcp-add                 Add a new MCP server (combined with flags below)"
	@echo "  --mcp-name <name>         Server name (used with --mcp-add)"
	@echo "  --mcp-type stdio|sse      Transport type (used with --mcp-add)"
	@echo "  --mcp-command <cmd>       Executable for stdio servers (used with --mcp-add)"
	@echo "  --mcp-args '<a1 a2 ...>'  Space-separated args for stdio server"
	@echo "  --mcp-url <url>           SSE endpoint URL (used with --mcp-add)"
	@echo "  --mcp-env KEY=VALUE       Extra env var for stdio server (repeatable)"
	@echo "  --mcp-remove <name>       Remove a server by name"
	@echo "  --mcp-list                List configured servers"
	@echo "  --mcp-tools <name>        List tools from named server"
	@echo "  --mcp-tools-all           List tools from all servers"
	@echo ""
	@echo "MCP slash commands (interactive mode — available at every prompt):"
	@echo "  /mcp-list                          List configured servers"
	@echo "  /mcp-tools [server-name]           List tools (all servers or one)"
	@echo "  /mcp-add stdio <name> <cmd> [args] Add stdio server"
	@echo "  /mcp-add sse   <name> <url>        Add SSE server"
	@echo "  /mcp-remove <name>                 Remove server"
	@echo ""
	@echo "MCP manual session targets (Day16):"
	@echo "  make run-mcp-session          Fresh session each run, auto-registers filesystem server"
	@echo "  make run-mcp-session-persist  Persistent session (history kept across runs)"
	@echo ""
	@echo "  Override variables:"
	@echo "    MCP_FS_ROOT=<dir>           Root dir exposed to filesystem MCP server (default: current dir)"
	@echo "    MCP_SESSION_PROF=<path.md>  Optional profile file to inject into the session"
	@echo "    MCP_SESSION_HIST=<path>     Override session history file"
	@echo "    MCP_SESSION_CFG=<path>      Override MCP config file"
	@echo ""
	@echo "  Examples:"
	@echo "    make run-mcp-session"
	@echo "    make run-mcp-session MCP_FS_ROOT=/home/user/projects"
	@echo "    make run-mcp-session MCP_SESSION_PROF=/tmp/my_profile.md"
	@echo "    make run-mcp-session-persist MCP_FS_ROOT=/tmp"
	@echo ""
	@echo "MCP integration test targets (Day16):"
	@echo "  make run-mcp-add-list-remove  Test 1: YAML config round-trip (no API key)"
	@echo "  make run-mcp-tools-filesystem Test 2: tool discovery via filesystem server (needs npx)"
	@echo "  make run-mcp-test             Run tests 1+2 (no API key needed)"
	@echo "  make run-mcp-slash-test       Test 3: /mcp-* slash commands (API key required)"
	@echo "  make run-mcp-all              Run all 3 MCP tests (API key required)"
	@echo ""
	@echo "MCP CLI examples:"
	@echo "  # Add filesystem server (reads /tmp)"
	@echo "  make BINARY_NAME=./$(BINARY_NAME) run-mcp-add-list-remove"
	@echo "  ./$(BINARY_NAME) --mcp-add --mcp-name fs --mcp-type stdio \\"
	@echo "      --mcp-command npx --mcp-args '-y @modelcontextprotocol/server-filesystem /tmp'"
	@echo "  ./$(BINARY_NAME) --mcp-list"
	@echo "  ./$(BINARY_NAME) --mcp-tools fs"
	@echo "  ./$(BINARY_NAME) --mcp-remove fs"
	@echo ""
	@echo "  # Add remote SSE server"
	@echo "  ./$(BINARY_NAME) --mcp-add --mcp-name remote --mcp-type sse --mcp-url http://host:8080/sse"

.DEFAULT_GOAL := help
