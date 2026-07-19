.PHONY: test test-coverage vet clean help check-env build install-deps test-fsguard-live \
        run-orchestrator run-orchestrator-interactive run-orchestrator-rag run-orchestrator-local \
        run-mcp-add-list-remove run-mcp-tools-filesystem run-mcp-test \
        run-rag run-rag-interactive run-rag-eval run-rag-grounded run-rag-compare run-rag-rerank-compare \
        run-rag-local run-rag-interactive-local run-rag-eval-local run-rag-grounded-local \
        run-rag-compare-local run-rag-rerank-compare-local run-rag-tests-local \
        run-telegram-opt run-telegram-opt-local \
        check-tooling workspace clean-workspace run-tooling-demo run-tooling-perm-test run-tooling-interactive

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

# Day16: MCP server config
MCP_CONFIG_FILE    ?= /tmp/mcp_config_$(DAY).yaml
MCP_HIST_FILE      ?= /tmp/mcp_test_$(DAY).json

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

# ---------------------------------------------------------------------------
# Day31: orchestrator mode — one YAML config drives memory/RAG/MCP, and the
# main process orchestrates sub-agents from the config roster.
# ---------------------------------------------------------------------------
AGENT_CONFIG   ?= agent.config.yaml
AGENT_TASK     ?= Проанализируй репозиторий и предложи 3 улучшения архитектуры.

run-orchestrator: $(BINARY_NAME) check-env ## [Day31] Run the orchestrator on AGENT_TASK (AGENT_CONFIG=path.yaml, default agent.config.yaml)
	./$(BINARY_NAME) --config "$(AGENT_CONFIG)" --query "$(AGENT_TASK)" $(DEBUG_FLAG)

run-orchestrator-interactive: $(BINARY_NAME) check-env ## [Day31] Interactive orchestrator TUI (AGENT_CONFIG=path.yaml, default agent.config.yaml)
	./$(BINARY_NAME) --config "$(AGENT_CONFIG)" --interactive $(DEBUG_FLAG)

# Enable RAG from the CLI on top of the config (flags override config). The
# researcher role (rag: true) then grounds its work on the index.
run-orchestrator-rag: $(BINARY_NAME) check-env ## [Day31] Orchestrator with RAG enabled via flags (RAG_DB=, RAG_TOP_K=, RAG_THRESHOLD=, RAG_TOP_K_FINAL=)
	./$(BINARY_NAME) --config "$(AGENT_CONFIG)" --query "$(AGENT_TASK)" \
		--rag --rag-db "$(RAG_DB)" \
		--rag-embed-url "$(RAG_EMBED_URL)" --rag-embed-model "$(RAG_EMBED_MODEL)" \
		--rag-top-k "$(RAG_TOP_K)" --rag-threshold "$(RAG_THRESHOLD)" --rag-top-k-final "$(RAG_TOP_K_FINAL)" \
		$(DEBUG_FLAG)

# Run the orchestrator against a local OpenAI-compatible endpoint (LM Studio /
# Ollama). Override LOCAL_LLM_BASE_URL / LLM_MODEL as needed.
run-orchestrator-local: $(BINARY_NAME) ## [Day31][local] Orchestrator on a local LLM endpoint (LOCAL_LLM_BASE_URL=, LLM_MODEL=)
	LLM_PROVIDER=openai LLM_API_KEY=local LLM_BASE_URL="$(LOCAL_LLM_BASE_URL)" \
		./$(BINARY_NAME) --config "$(AGENT_CONFIG)" --query "$(AGENT_TASK)" $(DEBUG_FLAG)

# ---------------------------------------------------------------------------
# Day34: file tooling + permission model.
#
# Two external MCP servers do the file work (see agent.config.yaml):
#   fs     — @modelcontextprotocol/server-filesystem@2025.8.21, launched via npx
#            (the package hardcodes serverInfo.version "0.2.0" in every release —
#             do not mistake that for the npm version; npm 0.2.0 does not start)
#   search — fff-mcp (brew install dmtrKovalenko/fff/fff-mcp)
# The read/write asymmetry (read all of Day34/, write only Day34/workspace/) is
# enforced client-side by internal/policy, not by the servers.
#
# NOTE: `timeout` does not exist on macOS — never use it in these targets.
# ---------------------------------------------------------------------------
WORKSPACE_DIR ?= ../workspace
# Target of the negative test: a file OUTSIDE the write root that must survive.
PERM_TEST_FILE ?= internal/app/toolbelt.go

# Interactive session for exercising the file tooling by hand. Unlike the generic
# run-orchestrator-interactive it depends on check-tooling and workspace, so a
# missing npx/fff-mcp fails loudly here instead of silently dropping the tools.
run-tooling-interactive: $(BINARY_NAME) check-env check-tooling workspace ## [Day34] Interactive session for the file tooling (/perms, /tools, /mcp)
	@echo "Файловые инструменты активны. Полезные команды:"
	@echo "  /perms  — корни чтения/записи и права каждой роли"
	@echo "  /tools  — полный пул MCP-инструментов ([write] помечены)"
	@echo "  /mcp    — подключённые серверы"
	@echo "Запись разрешена только в $(abspath $(WORKSPACE_DIR))"
	@echo "=========================================="
	./$(BINARY_NAME) --config "$(AGENT_CONFIG)" --interactive $(DEBUG_FLAG)

test-fsguard-live: check-tooling ## [Day34] Drive the REAL fs MCP server and assert the permission model holds (no LLM key needed)
	go test -tags livefsguard -count=1 -run TestLiveFSGuard -v ./internal/app/

check-tooling: ## [Day34] Verify the external file MCP servers are installed (npx, fff-mcp)
	@missing=""; \
	if command -v npx >/dev/null 2>&1; then \
		echo "  npx      : $$(command -v npx)"; \
	else \
		echo "  npx      : NOT FOUND — install Node.js (brew install node)"; missing="yes"; \
	fi; \
	if command -v fff-mcp >/dev/null 2>&1; then \
		echo "  fff-mcp  : $$(command -v fff-mcp)"; \
	else \
		echo "  fff-mcp  : NOT FOUND — install it: brew install dmtrKovalenko/fff/fff-mcp"; missing="yes"; \
	fi; \
	if [ -n "$$missing" ]; then \
		echo "Error: file tooling is incomplete, the agent would silently lose those tools."; \
		exit 1; \
	fi; \
	echo "File tooling OK."

workspace: ## [Day34] Create the agent's write root (idempotent)
	@mkdir -p "$(WORKSPACE_DIR)"
	@echo "Write root: $(abspath $(WORKSPACE_DIR))"

clean-workspace: ## [Day34] Remove everything the agent wrote into workspace/ (NOT part of `clean`)
	@if [ -d "$(WORKSPACE_DIR)" ]; then \
		find "$(WORKSPACE_DIR)" -mindepth 1 ! -name '.gitkeep' -delete; \
		echo "Cleared: $(abspath $(WORKSPACE_DIR))"; \
	else \
		echo "Nothing to clear: $(WORKSPACE_DIR) does not exist"; \
	fi

# Positive scenario — exercises all six requirements in one task: read files,
# search across several of them, analyse the result, and create a new file.
TOOLING_TASK ?= Изучи компонент agent в этом проекте: найди через multi_grep все Go-файлы пакета internal/policy, прочитай их через read_multiple_files и разберись, как устроен guard файловых операций. Затем создай файл workspace/policy_overview.md с кратким описанием: какие инструменты считаются read/write, как работают deny-глобы и что происходит при попытке записи вне workspace. Ссылайся на конкретные файлы.

run-tooling-demo: $(BINARY_NAME) check-env check-tooling workspace ## [Day34] Demo: read + search + analyse + write a report into workspace/ (TOOLING_TASK=)
	@echo "Task: $(TOOLING_TASK)"
	@echo "=========================================="
	./$(BINARY_NAME) --config "$(AGENT_CONFIG)" --query "$(TOOLING_TASK)" $(DEBUG_FLAG)
	@echo "=========================================="
	@echo "Files in $(WORKSPACE_DIR):"
	@ls -la "$(WORKSPACE_DIR)"

# Negative scenario — the permission model itself. The agent is told to modify a
# file outside the write root; success means the guard refused (READ_ONLY_PATH
# appears in the output) AND the target file is byte-identical afterwards.
PERM_TEST_TASK ?= Открой файл $(PERM_TEST_FILE) и замени в нём первую строку комментария на слово BROKEN. Правку внеси прямо в этот файл.

run-tooling-perm-test: $(BINARY_NAME) check-env check-tooling workspace ## [Day34] Negative test: writing outside workspace/ must fail with READ_ONLY_PATH
	@echo "Target (must stay unchanged): $(PERM_TEST_FILE)"
	@before=$$(shasum "$(PERM_TEST_FILE)" | awk '{print $$1}'); \
	out=$$(./$(BINARY_NAME) --config "$(AGENT_CONFIG)" --query "$(PERM_TEST_TASK)" $(DEBUG_FLAG) 2>&1); \
	echo "$$out"; \
	after=$$(shasum "$(PERM_TEST_FILE)" | awk '{print $$1}'); \
	echo "=========================================="; \
	fail=""; \
	if echo "$$out" | grep -q "READ_ONLY_PATH"; then \
		echo "  [ok]   guard refused the write (READ_ONLY_PATH)"; \
	else \
		echo "  [FAIL] no READ_ONLY_PATH in the output"; fail="yes"; \
	fi; \
	if [ "$$before" = "$$after" ]; then \
		echo "  [ok]   $(PERM_TEST_FILE) is unchanged"; \
	else \
		echo "  [FAIL] $(PERM_TEST_FILE) WAS MODIFIED — the permission model leaked"; fail="yes"; \
	fi; \
	echo "=========================================="; \
	if [ -n "$$fail" ]; then exit 1; fi; \
	echo "  Итог: PASSED"

test: ## Run unit tests
	$(GO) test -v ./...

test-coverage: ## Generate coverage.html
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

vet: ## Static analysis
	$(GO) vet ./...

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

clean: ## Clean build artifacts
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -f *_result.txt
	rm -f $(MCP_CONFIG_FILE) $(MCP_HIST_FILE) \
		$(MCP_HIST_FILE:.json=.stats.json) \
		$(MCP_HIST_FILE:.json=.task.json) \
		$(MCP_HIST_FILE:.json=.mcp.yaml)
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

# ---------------------------------------------------------------------------
# Day33: CRM agent simulation. Replays 20 customer tickets
# (testdata/crm/tickets/*.json) for the FigurVault figure store through the
# grounded RAG pipeline over the store KB index (SHOP_DB), asserting grounded,
# sourced, keyword-covering answers + the off-topic guard. Report:
# crm_agent_result.txt.
# ---------------------------------------------------------------------------
SHOP_DB   ?= testdata/crm/shop.db
CRM_KB    ?= testdata/crm/kb
CRM_TIMEOUT ?= 1200s
# Embedding endpoint for the store index (must match at index and query time).
EMBED_URL   ?= http://localhost:11434
EMBED_MODEL ?= nomic-embed-text

build-crm-index: ## [Day33] Build the FigurVault store KB index (SHOP_DB) from CRM_KB (EMBED_URL=, EMBED_MODEL=)
	@echo "[crm] indexing $(CRM_KB) → $(SHOP_DB)  embed=$(EMBED_MODEL)@$(EMBED_URL)"
	@rm -f "$(SHOP_DB)"
	@$(MAKE) -C ../rag run-index-structural \
		INPUT="../agent/$(CRM_KB)" DB="../agent/$(SHOP_DB)" \
		EMBED_URL="$(EMBED_URL)" EMBED_MODEL="$(EMBED_MODEL)"

run-crm-agent: check-env ## [Day33] CRM ticket simulation over the store KB (needs SHOP_DB + LLM + embeddings)
	@echo "[crm] db=$(SHOP_DB)  gen=$(LLM_MODEL)@$(LLM_BASE_URL)  embed=$(EMBED_MODEL)@$(EMBED_URL)"
	@SHOP_DB="$(SHOP_DB)" EMBED_URL="$(EMBED_URL)" EMBED_MODEL="$(EMBED_MODEL)" \
	LLM_PROVIDER="$(LLM_PROVIDER)" LLM_BASE_URL="$(LLM_BASE_URL)" LLM_MODEL="$(LLM_MODEL)" LLM_API_KEY="$(LLM_API_KEY)" \
	$(GO) test -tags integration -run TestCRMAgentTickets -v -timeout $(CRM_TIMEOUT) .

build-crm-index-local: ## [Day33][local] Build the store KB index with local embeddings (LM Studio)
	@$(MAKE) build-crm-index EMBED_URL="$(LOCAL_EMBED_URL)" EMBED_MODEL="$(LOCAL_EMBED_MODEL)"

run-crm-agent-local: ## [Day33][local] CRM ticket simulation on the local LLM + local embeddings
	@echo "[local] gen=$(LOCAL_LLM_MODEL)@$(LOCAL_LLM_BASE_URL)  embed=$(LOCAL_EMBED_MODEL)@$(LOCAL_EMBED_URL)"
	@$(MAKE) run-crm-agent \
		LLM_PROVIDER="$(LOCAL_LLM_PROVIDER)" LLM_BASE_URL="$(LOCAL_LLM_BASE_URL)" \
		LLM_MODEL="$(LOCAL_LLM_MODEL)" LLM_API_KEY="$(LOCAL_LLM_API_KEY)" \
		EMBED_URL="$(LOCAL_EMBED_URL)" EMBED_MODEL="$(LOCAL_EMBED_MODEL)" \
		CRM_TIMEOUT="$(LOCAL_TEST_TIMEOUT)"

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
	@echo "Day31 — config-driven orchestrator (main entry point):"
	@echo "  make run-orchestrator              Solve AGENT_TASK via the orchestrator + sub-agents"
	@echo "  make run-orchestrator-interactive  Interactive orchestrator REPL"
	@echo "  make run-orchestrator-rag          Same, with RAG enabled via flags"
	@echo "  make run-orchestrator-local        Same, against a local LLM endpoint"
	@echo ""
	@echo "  Config: AGENT_CONFIG=$(AGENT_CONFIG)"
	@echo "    Every memory layer is ON by default; RAG and MCP are opt-in."
	@echo "    Explicit CLI flags override the config. See agent.config.yaml for"
	@echo "    the full schema and the sub-agent roster."
	@echo ""
	@echo "Day34 — файловые инструменты и права:"
	@echo "  make check-tooling            Проверить, что npx и fff-mcp установлены"
	@echo "  make workspace                Создать каталог записи ($(WORKSPACE_DIR))"
	@echo "  make run-tooling-demo         Демо: чтение + поиск + анализ + файл в workspace/"
	@echo "  make run-tooling-perm-test    Негативный тест: запись вне workspace/ → READ_ONLY_PATH"
	@echo "  make clean-workspace          Очистить workspace/ (в общий clean НЕ входит)"
	@echo ""
	@echo "  Чтение — весь Day34/, запись — только $(WORKSPACE_DIR)."
	@echo "  Асимметрию обеспечивает клиентский guard (internal/policy), не сами серверы."
	@echo "  Установка: brew install dmtrKovalenko/fff/fff-mcp (npx ставится с Node.js)"
	@echo ""
	@echo "Env parameters:"
	@echo "  LLM_API_KEY    API key (required)"
	@echo "  LLM_PROVIDER   openai | openrouter | gigachat (default: openrouter)"
	@echo "  LLM_MODEL      Model name (default: openai/gpt-4o-mini)"
	@echo "  LLM_BASE_URL   Provider endpoint (default: openrouter)"
	@echo "  DEBUG=1        Add --debug to any target"
	@echo ""

.DEFAULT_GOAL := help
