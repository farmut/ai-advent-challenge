.PHONY: build test vet clean help \
        run-smoke run-live-get-pet run-live-inventory \
        check-env agent-check-day16 \
        agent-setup-mcp agent-profile-init \
        run-agent run-agent-persist \
        run-http run-http-smoke \
        check-gigachat-env \
        run-gigachat-ping run-gigachat-agent run-gigachat-persist run-gigachat-tools

BINARY_NAME := petstore-mcp-server
HTTP_ADDR   ?= :8080
GO          := go
LDFLAGS     := -s -w

# ---------------------------------------------------------------------------
# Agent integration settings
# ---------------------------------------------------------------------------

# Path to the Day18 agent directory (../agent relative to this Makefile).
# Override on the command line if needed: make run-agent AGENT_DIR=/other/path
AGENT_DIR  ?= $(abspath ../agent)
AGENT_BIN  ?= $(AGENT_DIR)/ai-adv-agent-day18

# Legacy alias so targets that reference DAY16_BIN still work.
DAY16_DIR  := $(AGENT_DIR)
DAY16_BIN  := $(AGENT_BIN)

# MCP YAML config written by agent-setup-mcp and read by the agent.
SESSION_CFG  ?= /tmp/petstore_mcp.yaml

# Conversation history file.  run-agent resets it each launch;
# run-agent-persist keeps it across restarts.
SESSION_HIST ?= /tmp/petstore_session.json

# Optional profile Markdown file injected as the top system-message layer.
# Leave empty to skip: make run-agent SESSION_PROF=
# Set to a path to enable: make run-agent SESSION_PROF=/tmp/my_profile.md
SESSION_PROF ?=

# Pass MEMORY_UPDATE=1 to auto-update WM+LTM after every agent turn.
MEMORY_UPDATE_FLAG = $(if $(MEMORY_UPDATE),--memory-update,)

# Pass DEBUG=1 to add --debug to the agent call.
DEBUG_FLAG = $(if $(DEBUG),--debug,)

export LLM_PROVIDER ?= openrouter
export LLM_MODEL    ?= openai/gpt-4o-mini
export LLM_BASE_URL ?= https://openrouter.ai/api/v1
export LLM_API_KEY

# GigaChat integration test settings.
# GIGACHAT_SCOPE=GIGACHAT_API_PERS is for individual ("физическое лицо") accounts.
GIGACHAT_SCOPE ?= GIGACHAT_API_PERS
GIGACHAT_MODEL ?= GigaChat
GIGACHAT_HIST  ?= /tmp/gigachat_session.json

build: ## Build the MCP server binary
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) .

test: ## Run unit tests (no network required — uses httptest mock server)
	$(GO) test -v ./...

vet: ## Static analysis
	$(GO) vet ./...

clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)

# ---------------------------------------------------------------------------
# HTTP+SSE transport
# ---------------------------------------------------------------------------

run-http: build ## Start petstore-mcp-server in HTTP+SSE mode (HTTP_ADDR=:8080)
	@echo "=========================================="
	@echo "  Petstore MCP Server — HTTP+SSE mode"
	@echo "  Listening on $(HTTP_ADDR)"
	@echo "  Endpoints:"
	@echo "    GET  /sse                    — open SSE stream"
	@echo "    POST /message?sessionId=<id> — send JSON-RPC"
	@echo "    GET  /health                 — liveness probe"
	@echo "  Press Ctrl+C to stop."
	@echo "=========================================="
	./$(BINARY_NAME) -addr $(HTTP_ADDR)

run-http-smoke: build ## Smoke-test HTTP+SSE: connect SSE, call initialize, verify response
	@echo "=== HTTP+SSE smoke test ==="
	@PORT=$$(echo "$(HTTP_ADDR)" | tr -d ':'); \
	[ -z "$$PORT" ] && PORT=8080; \
	ADDR="http://localhost:$$PORT"; \
	./$(BINARY_NAME) -addr :$$PORT & SRV_PID=$$!; \
	sleep 0.3; \
	echo "--- health check ---"; \
	curl -sf "$$ADDR/health" | python3 -m json.tool; \
	echo ""; \
	echo "--- SSE: open stream, send initialize, read response ---"; \
	python3 - <<'PYEOF' "$$ADDR" \
	import sys, json, time, threading, urllib.request \
	\
	addr = sys.argv[1] \
	events = [] \
	session_url = None \
	\
	def read_sse(): \
	    global session_url \
	    with urllib.request.urlopen(addr + "/sse") as r: \
	        buf = "" \
	        for line in r: \
	            line = line.decode().rstrip("\n") \
	            if line.startswith("event: "): \
	                ev = line[7:] \
	            elif line.startswith("data: "): \
	                data = line[6:] \
	                if ev == "endpoint": \
	                    session_url = addr + data \
	                    print(f"  endpoint → {session_url}") \
	                elif ev == "message": \
	                    events.append(json.loads(data)) \
	                    print(f"  message  → {json.dumps(events[-1], indent=2)}") \
	                    break \
	\
	t = threading.Thread(target=read_sse, daemon=True) \
	t.start() \
	time.sleep(0.2) \
	req = json.dumps({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0.1"}}}).encode() \
	urllib.request.urlopen(urllib.request.Request(session_url, data=req, method="POST", headers={"Content-Type":"application/json"})) \
	t.join(timeout=3) \
	assert events and events[0].get("result",{}).get("protocolVersion") == "2024-11-05", "FAIL: unexpected response" \
	print("\n  ✓ PASS: initialize round-trip OK") \
	PYEOF \
	kill $$SRV_PID 2>/dev/null; wait $$SRV_PID 2>/dev/null; true

run-smoke: build ## Smoke: MCP handshake + tools/list (no network)
	@echo "=== Petstore MCP Server smoke test ==="
	@{ \
	  printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0.0.1"}}}\n'; \
	  printf '{"jsonrpc":"2.0","method":"notifications/initialized"}\n'; \
	  printf '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n'; \
	} | ./$(BINARY_NAME) 2>/dev/null | ./scripts/show_tools.sh
	@echo "=== smoke test complete ==="

run-live-get-pet: build ## Live: fetch pet id=1 from the real Petstore API
	@echo "=== Live: get pet id=1 ==="
	@{ \
	  printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1.0"}}}\n'; \
	  printf '{"jsonrpc":"2.0","method":"notifications/initialized"}\n'; \
	  printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"pet_get_by_id","arguments":{"petId":1}}}\n'; \
	} | ./$(BINARY_NAME) 2>/dev/null | ./scripts/show_result.sh 2
	@echo "=== done ==="

run-live-inventory: build ## Live: fetch store inventory from the real Petstore API
	@echo "=== Live: store inventory ==="
	@{ \
	  printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1.0"}}}\n'; \
	  printf '{"jsonrpc":"2.0","method":"notifications/initialized"}\n'; \
	  printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"store_get_inventory","arguments":{}}}\n'; \
	} | ./$(BINARY_NAME) 2>/dev/null | ./scripts/show_result.sh 2
	@echo "=== done ==="

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*##"}{printf "  %-26s %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

check-env: ## Verify LLM_API_KEY is set
	@if [ -z "$(LLM_API_KEY)" ]; then \
		echo "Error: LLM_API_KEY is not set"; \
		echo "  export LLM_API_KEY=your-api-key"; \
		exit 1; \
	fi
	@echo "  LLM_PROVIDER=$(LLM_PROVIDER)"
	@echo "  LLM_MODEL=$(LLM_MODEL)"

agent-build: ## Build the Day18 agent binary (ai-adv-agent-day18)
	$(MAKE) -C $(AGENT_DIR) build DAY=day18

agent-check-day16: ## Verify the Day18 agent binary exists (builds it if missing)
	@if [ ! -f "$(AGENT_BIN)" ]; then \
		echo "Agent binary not found: $(AGENT_BIN)"; \
		echo "Building it now..."; \
		$(MAKE) --no-print-directory agent-build; \
	fi

# ---------------------------------------------------------------------------
# MCP setup
# ---------------------------------------------------------------------------

agent-setup-mcp: build ## Write SESSION_CFG YAML with the petstore-mcp-server binary path
	@SERVER_BIN="$(CURDIR)/$(BINARY_NAME)"; \
	printf 'servers:\n  - name: petstore\n    type: stdio\n    command: %s\n' \
		"$$SERVER_BIN" > "$(SESSION_CFG)"; \
	echo "=========================================="; \
	echo "  MCP config written: $(SESSION_CFG)"; \
	echo "  petstore server:    $$SERVER_BIN"; \
	echo "=========================================="

# ---------------------------------------------------------------------------
# Profile init (delegates to the Day16 agent binary)
# ---------------------------------------------------------------------------

agent-profile-init: agent-check-day16 ## Init or overwrite SESSION_PROF via interactive dialog (SESSION_PROF= required)
	@if [ -z "$(SESSION_PROF)" ]; then \
		echo "Error: SESSION_PROF is not set"; \
		echo "  make agent-profile-init SESSION_PROF=/tmp/my_profile.md"; \
		exit 1; \
	fi
	$(DAY16_BIN) --profile "$(SESSION_PROF)" --profile-init

# ---------------------------------------------------------------------------
# Interactive agent sessions
# ---------------------------------------------------------------------------

run-agent: build agent-check-day16 check-env ## Fresh interactive session with HTTP+SSE MCP server (auto-started); history cleared each run (HTTP_ADDR=:8080, SESSION_PROF=, MEMORY_UPDATE=1, DEBUG=1)
	@HIST="$(SESSION_HIST)"; \
	CFG="$(SESSION_CFG)"; \
	PROF="$(SESSION_PROF)"; \
	PORT=$$(echo "$(HTTP_ADDR)" | tr -d ':'); \
	[ -z "$$PORT" ] && PORT=8080; \
	MCP_URL="http://localhost:$$PORT"; \
	\
	echo "=========================================="; \
	echo "  Starting MCP server (HTTP+SSE) on :$$PORT..."; \
	./$(BINARY_NAME) -addr :$$PORT 2>/tmp/petstore-mcp-stderr.log & SRV_PID=$$!; \
	trap "echo ''; echo '  Stopping MCP server (pid $$SRV_PID)...'; kill $$SRV_PID 2>/dev/null; wait $$SRV_PID 2>/dev/null; true" EXIT INT TERM; \
	READY=0; \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		curl -sf "$$MCP_URL/health" >/dev/null 2>&1 && READY=1 && break; \
		sleep 0.2; \
	done; \
	if [ "$$READY" = "0" ]; then \
		echo "  Error: MCP server did not start within 3 seconds"; \
		cat /tmp/petstore-mcp-stderr.log; \
		exit 1; \
	fi; \
	echo "  MCP server ready  pid=$$SRV_PID"; \
	\
	printf 'servers:\n  - name: petstore\n    type: sse\n    url: %s/sse\n' "$$MCP_URL" > "$$CFG"; \
	\
	rm -f "$$HIST" "$${HIST%.json}.task.json" "$${HIST%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  Petstore Agent  —  fresh session"; \
	echo "  Agent binary: $(DAY16_BIN)"; \
	echo "  MCP config:   $$CFG  (type: sse, url: $$MCP_URL/sse)"; \
	echo "  History:      $$HIST  (cleared each run)"; \
	echo "  Model:        $(LLM_MODEL)  |  Provider: $(LLM_PROVIDER)"; \
	if [ -n "$$PROF" ]; then echo "  Profile:      $$PROF"; fi; \
	echo "=========================================="; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then \
		echo "--- Active profile ---"; cat "$$PROF"; echo ""; \
	fi; \
	echo "  MCP slash commands (type at any Task/Execution prompt):"; \
	echo "    /mcp-list              — list registered MCP servers"; \
	echo "    /mcp-tools             — list all tools from all servers"; \
	echo "    /mcp-tools petstore    — list petstore tools only"; \
	echo ""; \
	echo "  FSM commands:  /yes  /no  Enter=pause  /exit"; \
	echo "=========================================="; echo ""; \
	PROF_FLAG=""; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then PROF_FLAG="--profile $$PROF"; fi; \
	$(DAY16_BIN) \
		--interactive \
		--history "$$HIST" \
		--mcp-config "$$CFG" \
		--history-limit 50 \
		--show-cost \
		$$PROF_FLAG \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)

# ---------------------------------------------------------------------------
# GigaChat integration tests
# ---------------------------------------------------------------------------
# Prerequisites: export LLM_API_KEY=<GigaChat_OAuth_client_credentials_key>
# The agent reads GIGACHAT_SCOPE to select the OAuth scope (default: GIGACHAT_API_PERS).
# LLM_BASE_URL is intentionally cleared so the agent uses its built-in GigaChat URL.

check-gigachat-env: ## [GigaChat] Verify LLM_API_KEY (OAuth credentials) is set
	@if [ -z "$(LLM_API_KEY)" ]; then \
		echo "Error: LLM_API_KEY is not set (required: GigaChat OAuth client credentials)"; \
		echo "  export LLM_API_KEY=<your_gigachat_auth_key>"; \
		exit 1; \
	fi
	@echo "  LLM_PROVIDER   = gigachat"
	@echo "  LLM_MODEL      = $(GIGACHAT_MODEL)"
	@echo "  GIGACHAT_SCOPE = $(GIGACHAT_SCOPE)"

run-gigachat-ping: agent-build check-gigachat-env ## [GigaChat] One-shot connectivity test: get OAuth token + send a single query
	@echo "=========================================="; \
	echo "  GigaChat Ping Test"; \
	echo "  Model:  $(GIGACHAT_MODEL)  |  Scope: $(GIGACHAT_SCOPE)"; \
	echo "=========================================="; \
	echo ""; \
	LLM_PROVIDER=gigachat \
	LLM_MODEL="$(GIGACHAT_MODEL)" \
	LLM_BASE_URL="" \
	GIGACHAT_SCOPE="$(GIGACHAT_SCOPE)" \
	$(AGENT_BIN) \
		--query "Кто ты? Ответь одним предложением." \
		--history "" \
		--show-cost \
		$(DEBUG_FLAG); \
	echo ""; \
	echo "  Result: GigaChat connection OK"; \
	echo "=========================================="

run-gigachat-agent: build agent-build check-gigachat-env ## [GigaChat] Fresh interactive agent session with HTTP+SSE MCP petstore (SESSION_PROF=, MEMORY_UPDATE=1, DEBUG=1)
	@HIST="$(GIGACHAT_HIST)"; \
	CFG="$(SESSION_CFG)"; \
	PROF="$(SESSION_PROF)"; \
	PORT=$$(echo "$(HTTP_ADDR)" | tr -d ':'); \
	[ -z "$$PORT" ] && PORT=8080; \
	MCP_URL="http://localhost:$$PORT"; \
	\
	echo "=========================================="; \
	echo "  Starting MCP server (HTTP+SSE) on :$$PORT..."; \
	./$(BINARY_NAME) -addr :$$PORT 2>/tmp/petstore-mcp-stderr.log & SRV_PID=$$!; \
	trap "echo ''; echo '  Stopping MCP server (pid $$SRV_PID)...'; kill $$SRV_PID 2>/dev/null; wait $$SRV_PID 2>/dev/null; true" EXIT INT TERM; \
	READY=0; \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		curl -sf "$$MCP_URL/health" >/dev/null 2>&1 && READY=1 && break; \
		sleep 0.2; \
	done; \
	if [ "$$READY" = "0" ]; then \
		echo "  Error: MCP server did not start within 3 seconds"; \
		cat /tmp/petstore-mcp-stderr.log; \
		exit 1; \
	fi; \
	echo "  MCP server ready  pid=$$SRV_PID"; \
	\
	printf 'servers:\n  - name: petstore\n    type: sse\n    url: %s/sse\n' "$$MCP_URL" > "$$CFG"; \
	\
	rm -f "$$HIST" "$${HIST%.json}.task.json" "$${HIST%.json}.stats.json"; \
	echo "=========================================="; \
	echo "  GigaChat Petstore Agent  —  fresh session"; \
	echo "  Agent:      $(AGENT_BIN)"; \
	echo "  Model:      $(GIGACHAT_MODEL)  |  Scope: $(GIGACHAT_SCOPE)"; \
	echo "  MCP config: $$CFG  (type: sse, url: $$MCP_URL/sse)"; \
	echo "  History:    $$HIST  (cleared each run)"; \
	if [ -n "$$PROF" ]; then echo "  Profile:    $$PROF"; fi; \
	echo "=========================================="; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then \
		echo "--- Active profile ---"; cat "$$PROF"; echo ""; \
	fi; \
	echo "  FSM commands:  /yes  /no  Enter=pause  /exit  |  /mcp-tools  /mcp-list"; \
	echo "=========================================="; echo ""; \
	PROF_FLAG=""; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then PROF_FLAG="--profile $$PROF"; fi; \
	LLM_PROVIDER=gigachat \
	LLM_MODEL="$(GIGACHAT_MODEL)" \
	LLM_BASE_URL="" \
	GIGACHAT_SCOPE="$(GIGACHAT_SCOPE)" \
	$(AGENT_BIN) \
		--interactive \
		--history "$$HIST" \
		--mcp-config "$$CFG" \
		--history-limit 50 \
		--show-cost \
		$$PROF_FLAG \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)

run-gigachat-persist: build agent-build check-gigachat-env ## [GigaChat] Persistent session with HTTP+SSE MCP server; history kept across restarts (SESSION_PROF=, DEBUG=1)
	@HIST="$(GIGACHAT_HIST)"; \
	CFG="$(SESSION_CFG)"; \
	PROF="$(SESSION_PROF)"; \
	PORT=$$(echo "$(HTTP_ADDR)" | tr -d ':'); \
	[ -z "$$PORT" ] && PORT=8080; \
	MCP_URL="http://localhost:$$PORT"; \
	\
	echo "=========================================="; \
	echo "  Starting MCP server (HTTP+SSE) on :$$PORT..."; \
	./$(BINARY_NAME) -addr :$$PORT 2>/tmp/petstore-mcp-stderr.log & SRV_PID=$$!; \
	trap "echo ''; echo '  Stopping MCP server (pid $$SRV_PID)...'; kill $$SRV_PID 2>/dev/null; wait $$SRV_PID 2>/dev/null; true" EXIT INT TERM; \
	READY=0; \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		curl -sf "$$MCP_URL/health" >/dev/null 2>&1 && READY=1 && break; \
		sleep 0.2; \
	done; \
	if [ "$$READY" = "0" ]; then \
		echo "  Error: MCP server did not start within 3 seconds"; \
		cat /tmp/petstore-mcp-stderr.log; \
		exit 1; \
	fi; \
	echo "  MCP server ready  pid=$$SRV_PID"; \
	\
	printf 'servers:\n  - name: petstore\n    type: sse\n    url: %s/sse\n' "$$MCP_URL" > "$$CFG"; \
	\
	echo "=========================================="; \
	echo "  GigaChat Petstore Agent  —  persistent session"; \
	echo "  Agent:      $(AGENT_BIN)"; \
	echo "  Model:      $(GIGACHAT_MODEL)  |  Scope: $(GIGACHAT_SCOPE)"; \
	echo "  MCP config: $$CFG  (type: sse, url: $$MCP_URL/sse)"; \
	echo "  History:    $$HIST  (kept across restarts)"; \
	if [ -n "$$PROF" ]; then echo "  Profile:    $$PROF"; fi; \
	echo "=========================================="; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then \
		echo "--- Active profile ---"; cat "$$PROF"; echo ""; \
	fi; \
	echo "  FSM commands:  /yes  /no  Enter=pause  /exit  |  /mcp-tools  /mcp-list"; \
	echo "=========================================="; echo ""; \
	PROF_FLAG=""; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then PROF_FLAG="--profile $$PROF"; fi; \
	LLM_PROVIDER=gigachat \
	LLM_MODEL="$(GIGACHAT_MODEL)" \
	LLM_BASE_URL="" \
	GIGACHAT_SCOPE="$(GIGACHAT_SCOPE)" \
	$(AGENT_BIN) \
		--interactive \
		--history "$$HIST" \
		--mcp-config "$$CFG" \
		--history-limit 50 \
		--show-cost \
		$$PROF_FLAG \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)

run-gigachat-tools: build agent-build check-gigachat-env ## [GigaChat] Tool-call smoke test: function_call → MCP petstore tools/call → result (HTTP+SSE)
	@HIST=$$(mktemp /tmp/gigachat_tools_XXXXXX.json); \
	CFG=$$(mktemp /tmp/gigachat_tools_XXXXXX.yaml); \
	OUT=$$(mktemp); \
	PORT=$$(echo "$(HTTP_ADDR)" | tr -d ':'); \
	[ -z "$$PORT" ] && PORT=8080; \
	MCP_URL="http://localhost:$$PORT"; \
	\
	echo "=========================================="; \
	echo "  Starting MCP server (HTTP+SSE) on :$$PORT..."; \
	./$(BINARY_NAME) -addr :$$PORT 2>/tmp/petstore-mcp-stderr.log & SRV_PID=$$!; \
	trap "kill $$SRV_PID 2>/dev/null; wait $$SRV_PID 2>/dev/null; rm -f $$HIST $$CFG $$OUT $${HIST%.json}.stats.json; true" EXIT INT TERM; \
	READY=0; \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		curl -sf "$$MCP_URL/health" >/dev/null 2>&1 && READY=1 && break; \
		sleep 0.2; \
	done; \
	if [ "$$READY" = "0" ]; then \
		echo "  Error: MCP server did not start"; cat /tmp/petstore-mcp-stderr.log; exit 1; \
	fi; \
	printf 'servers:\n  - name: petstore\n    type: sse\n    url: %s/sse\n' "$$MCP_URL" > "$$CFG"; \
	\
	echo "  GigaChat Tool-Calling Integration Test"; \
	echo "  Model:  $(GIGACHAT_MODEL)  |  Scope: $(GIGACHAT_SCOPE)"; \
	echo "  MCP:    $$MCP_URL/sse  (type: sse)"; \
	echo "  Tests:  GigaChat function_call → MCP tools/call → response"; \
	echo "=========================================="; echo ""; \
	printf 'Fetch the pet with id 1 from the Petstore API. Show its name and status.\n/yes\n/yes\n/yes\nexit\n' | \
	LLM_PROVIDER=gigachat \
	LLM_MODEL="$(GIGACHAT_MODEL)" \
	LLM_BASE_URL="" \
	GIGACHAT_SCOPE="$(GIGACHAT_SCOPE)" \
	$(AGENT_BIN) \
		--interactive \
		--history "$$HIST" \
		--mcp-config "$$CFG" \
		--history-limit 50 \
		--show-cost \
		$(DEBUG_FLAG) \
	>"$$OUT" 2>&1; \
	echo "--- Agent output ---"; cat "$$OUT"; echo ""; echo "--------------------"; echo ""; \
	FAILED=0; \
	echo "  Checks:"; \
	for PHASE in PLANNING EXECUTION VALIDATION; do \
		if grep -qF "[$$PHASE]" "$$OUT"; then \
			echo "  ✓ phase $$PHASE present"; \
		else \
			echo "  ✗ FAIL: phase $$PHASE not found"; FAILED=$$((FAILED+1)); \
		fi; \
	done; \
	if grep -qi "pet\|doggie\|name\|status" "$$OUT"; then \
		echo "  ✓ petstore data in output (tool call succeeded)"; \
	else \
		echo "  ? petstore data not found — GigaChat may not have invoked the tool"; \
	fi; \
	echo ""; echo "=========================================="; \
	if [ "$$FAILED" = "0" ]; then \
		echo "  Result: PASS"; \
	else \
		echo "  Result: $$FAILED check(s) FAILED"; exit 1; \
	fi; \
	echo "=========================================="

run-agent-persist: build agent-check-day16 check-env ## Persistent session with HTTP+SSE MCP server (auto-started); history kept across restarts (HTTP_ADDR=:8080, SESSION_PROF=, MEMORY_UPDATE=1, DEBUG=1)
	@HIST="$(SESSION_HIST)"; \
	CFG="$(SESSION_CFG)"; \
	PROF="$(SESSION_PROF)"; \
	PORT=$$(echo "$(HTTP_ADDR)" | tr -d ':'); \
	[ -z "$$PORT" ] && PORT=8080; \
	MCP_URL="http://localhost:$$PORT"; \
	\
	echo "=========================================="; \
	echo "  Starting MCP server (HTTP+SSE) on :$$PORT..."; \
	./$(BINARY_NAME) -addr :$$PORT 2>/tmp/petstore-mcp-stderr.log & SRV_PID=$$!; \
	trap "echo ''; echo '  Stopping MCP server (pid $$SRV_PID)...'; kill $$SRV_PID 2>/dev/null; wait $$SRV_PID 2>/dev/null; true" EXIT INT TERM; \
	READY=0; \
	for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do \
		curl -sf "$$MCP_URL/health" >/dev/null 2>&1 && READY=1 && break; \
		sleep 0.2; \
	done; \
	if [ "$$READY" = "0" ]; then \
		echo "  Error: MCP server did not start within 3 seconds"; \
		cat /tmp/petstore-mcp-stderr.log; \
		exit 1; \
	fi; \
	echo "  MCP server ready  pid=$$SRV_PID"; \
	\
	printf 'servers:\n  - name: petstore\n    type: sse\n    url: %s/sse\n' "$$MCP_URL" > "$$CFG"; \
	\
	echo "=========================================="; \
	echo "  Petstore Agent  —  persistent session"; \
	echo "  Agent binary: $(DAY16_BIN)"; \
	echo "  MCP config:   $$CFG  (type: sse, url: $$MCP_URL/sse)"; \
	echo "  History:      $$HIST  (kept across restarts)"; \
	echo "  Model:        $(LLM_MODEL)  |  Provider: $(LLM_PROVIDER)"; \
	if [ -n "$$PROF" ]; then echo "  Profile:      $$PROF"; fi; \
	echo "=========================================="; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then \
		echo "--- Active profile ---"; cat "$$PROF"; echo ""; \
	fi; \
	echo "  FSM commands:  /yes  /no  Enter=pause  /exit  |  /mcp-tools  /mcp-list"; \
	echo "=========================================="; echo ""; \
	PROF_FLAG=""; \
	if [ -n "$$PROF" ] && [ -f "$$PROF" ]; then PROF_FLAG="--profile $$PROF"; fi; \
	$(DAY16_BIN) \
		--interactive \
		--history "$$HIST" \
		--mcp-config "$$CFG" \
		--history-limit 50 \
		--show-cost \
		$$PROF_FLAG \
		$(MEMORY_UPDATE_FLAG) \
		$(DEBUG_FLAG)
