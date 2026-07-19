.PHONY: build test vet clean help \
        agent-build agent-check-day16 agent-setup-mcp agent-profile-init \
        run-agent run-agent-persist \
        run-smoke run-live-get-pet run-live-inventory \
        check-gigachat-env \
        run-gigachat-ping run-gigachat-agent run-gigachat-persist run-gigachat-tools \
        run-multi-mcp-setup run-multi-mcp-tools-check run-multi-mcp-session \
        litellm-issue-key litellm-fetch-cert litellm-ping run-litellm \
        check-tooling workspace clean-workspace run-tooling-demo run-tooling-perm-test

MCP_DIR   := $(CURDIR)/mcp-server
AGENT_DIR ?= $(MCP_DIR)/../agent
AGENT_BIN ?= $(abspath $(AGENT_DIR))/ai-adv-agent-day20

DEBUG_FLAG = $(if $(DEBUG),--debug,)

# ---------------------------------------------------------------------------
# LiteLLM integration (talk to the public proxy on ai-adv-stage over HTTPS)
# ---------------------------------------------------------------------------
# The proxy is OpenAI-compatible and published by nginx on 443 with a
# self-signed cert (litellm bound to 127.0.0.1:4000 behind it). The agent
# reaches it directly over HTTPS — no SSH tunnel:
#   LLM_PROVIDER=openai  LLM_BASE_URL=$(LITELLM_URL)/v1  --ca-cert $(LITELLM_CACERT)
# The cert is *pinned* (added to the trust pool), TLS verification stays ON.
# Friends need: (1) the endpoint URL, (2) a user key, (3) the litellm-ca.crt file
# (fetch it themselves with `make litellm-fetch-cert`, no SSH access required).
LITELLM_URL         ?= https://81.26.176.39
LITELLM_HOSTPORT    ?= 81.26.176.39:443
LITELLM_CACERT      ?= $(CURDIR)/litellm-ca.crt
LITELLM_MODEL       ?= gemma4:12b
LITELLM_HIST        ?= /tmp/litellm_session_day30.json
# litellm-issue-key still uses SSH (owner-only, needs the master key).
LITELLM_SSH_HOST    ?= ai-adv-stage
LITELLM_REMOTE_PORT ?= 4000
# Master key is only needed to mint user keys (litellm-issue-key). Never commit it.
LITELLM_MASTER_KEY  ?=

# GigaChat override variables (forwarded to mcp-server/Makefile).
GIGACHAT_SCOPE ?= GIGACHAT_API_PERS
GIGACHAT_MODEL ?= GigaChat
GIGACHAT_HIST  ?= /tmp/gigachat_session.json

# Multi-MCP variables (forwarded to mcp-server/Makefile).
MULTI_MCP_WORKSPACE ?= $(HOME)/workspace
MULTI_MCP_CFG       ?= /tmp/multi_mcp_day20.yaml
MULTI_MCP_HIST      ?= /tmp/multi_mcp_session_day20.json
GITHUB_TOKEN        ?= $(GITHUB_PERSONAL_ACCESS_TOKEN)

# Delegate every target to mcp-server/Makefile, forwarding all variables.
build test vet clean run-smoke run-live-get-pet run-live-inventory \
agent-build agent-check-day16 agent-setup-mcp agent-profile-init \
run-agent run-agent-persist \
check-gigachat-env \
run-gigachat-ping run-gigachat-agent run-gigachat-persist run-gigachat-tools \
run-multi-mcp-setup run-multi-mcp-tools-check run-multi-mcp-session:
	$(MAKE) -C $(MCP_DIR) $@ \
		SESSION_PROF="$(SESSION_PROF)" \
		SESSION_HIST="$(SESSION_HIST)" \
		SESSION_CFG="$(SESSION_CFG)" \
		MEMORY_UPDATE="$(MEMORY_UPDATE)" \
		DEBUG="$(DEBUG)" \
		AGENT_DIR="$(AGENT_DIR)" \
		AGENT_BIN="$(AGENT_BIN)" \
		GIGACHAT_SCOPE="$(GIGACHAT_SCOPE)" \
		GIGACHAT_MODEL="$(GIGACHAT_MODEL)" \
		GIGACHAT_HIST="$(GIGACHAT_HIST)" \
		MULTI_MCP_WORKSPACE="$(MULTI_MCP_WORKSPACE)" \
		MULTI_MCP_CFG="$(MULTI_MCP_CFG)" \
		MULTI_MCP_HIST="$(MULTI_MCP_HIST)" \
		GITHUB_TOKEN="$(GITHUB_TOKEN)"

# ---------------------------------------------------------------------------
# Day34: file tooling + permission model — proxied to agent/Makefile, which owns
# the targets. Read root is Day34/, write root is Day34/workspace/.
# ---------------------------------------------------------------------------
AGENT_MAKE_DIR := $(CURDIR)/agent

check-tooling: ## [Day34] Verify npx and fff-mcp are installed
	@$(MAKE) -C $(AGENT_MAKE_DIR) check-tooling

workspace: ## [Day34] Create the agent's write root (workspace/)
	@$(MAKE) -C $(AGENT_MAKE_DIR) workspace

clean-workspace: ## [Day34] Clear workspace/ (not part of `clean`)
	@$(MAKE) -C $(AGENT_MAKE_DIR) clean-workspace

run-tooling-demo: ## [Day34] Demo: read + search + analyse + write a report into workspace/ (TOOLING_TASK=)
	@$(MAKE) -C $(AGENT_MAKE_DIR) run-tooling-demo $(if $(TOOLING_TASK),TOOLING_TASK="$(TOOLING_TASK)",) DEBUG="$(DEBUG)"

run-tooling-perm-test: ## [Day34] Negative test: writing outside workspace/ must fail with READ_ONLY_PATH
	@$(MAKE) -C $(AGENT_MAKE_DIR) run-tooling-perm-test DEBUG="$(DEBUG)"

litellm-issue-key: ## Mint a user API key on the LiteLLM proxy (LITELLM_MASTER_KEY=sk-... required)
	@if [ -z "$(LITELLM_MASTER_KEY)" ]; then \
		echo "Error: LITELLM_MASTER_KEY is not set"; \
		echo "  make litellm-issue-key LITELLM_MASTER_KEY=sk-<master-key>"; \
		exit 1; \
	fi
	@echo "Issuing user key on $(LITELLM_SSH_HOST) (model=$(LITELLM_MODEL))..."
	@ssh $(LITELLM_SSH_HOST) 'curl -s -X POST http://127.0.0.1:$(LITELLM_REMOTE_PORT)/key/generate \
		-H "Authorization: Bearer $(LITELLM_MASTER_KEY)" \
		-H "Content-Type: application/json" \
		-d "{\"models\":[\"$(LITELLM_MODEL)\"]}"' \
		| python3 -c 'import sys,json; d=json.load(sys.stdin); print("\nUser key:", d.get("key") or d)' \
		|| echo "  (request failed — check master key / proxy)"

litellm-fetch-cert: ## Download the proxy's self-signed cert to LITELLM_CACERT (no SSH needed)
	@echo "Fetching cert from $(LITELLM_HOSTPORT) ..."
	@echo | openssl s_client -connect $(LITELLM_HOSTPORT) -servername $(LITELLM_HOSTPORT) 2>/dev/null \
		| openssl x509 > "$(LITELLM_CACERT)" 2>/dev/null; \
	if [ -s "$(LITELLM_CACERT)" ]; then \
		echo "Saved: $(LITELLM_CACERT)"; \
		openssl x509 -in "$(LITELLM_CACERT)" -noout -subject -enddate; \
	else \
		echo "Error: could not fetch cert (is 443 reachable?)"; rm -f "$(LITELLM_CACERT)"; exit 1; \
	fi

# Internal: check prerequisites, then run $(AGENT_CMD) against the public HTTPS
# endpoint. AGENT_CMD is set by the caller (litellm-ping / run-litellm).
define LITELLM_RUN_AGENT
	@if [ -z "$(LLM_API_KEY)" ]; then \
		echo "Error: LLM_API_KEY is not set (a LiteLLM user key)"; \
		echo "  Issue one:  make litellm-issue-key LITELLM_MASTER_KEY=sk-<master>"; \
		echo "  Then:       export LLM_API_KEY=sk-<user-key>"; \
		exit 1; \
	fi
	@if [ ! -f "$(LITELLM_CACERT)" ]; then \
		echo "Error: CA cert not found: $(LITELLM_CACERT)"; \
		echo "  Fetch it once:  make litellm-fetch-cert"; \
		exit 1; \
	fi
	@if [ ! -f "$(AGENT_BIN)" ]; then \
		echo "Agent binary missing, building..."; \
		$(MAKE) -C $(AGENT_DIR) build DAY=day20; \
	fi
	@echo "Endpoint: $(LITELLM_URL)  |  Model: $(LITELLM_MODEL)  |  cert: $(LITELLM_CACERT)"; \
	echo "=========================================="; \
	LLM_PROVIDER=openai \
	LLM_BASE_URL="$(LITELLM_URL)/v1" \
	LLM_API_KEY="$(LLM_API_KEY)" \
	LLM_MODEL="$(LITELLM_MODEL)" \
	$(AGENT_CMD) --ca-cert "$(LITELLM_CACERT)"
endef

litellm-ping: ## Smoke test: one-shot HTTPS query to the LiteLLM proxy through the agent (LLM_API_KEY required)
	$(eval AGENT_CMD := $(AGENT_BIN) --history "" --max-tokens 800 --query "Answer in one short sentence: what is PostgreSQL?" $(DEBUG_FLAG))
	$(LITELLM_RUN_AGENT)

run-litellm: ## Interactive agent session against the LiteLLM proxy over HTTPS (LLM_API_KEY required)
	@H="$(LITELLM_HIST)"; rm -f "$$H" "$${H%.json}.task.json" "$${H%.json}.stats.json" 2>/dev/null || true
	$(eval AGENT_CMD := $(AGENT_BIN) --interactive --history "$(LITELLM_HIST)" --history-limit 50 --show-cost $(DEBUG_FLAG))
	$(LITELLM_RUN_AGENT)

help: ## Show available targets
	@$(MAKE) -C $(MCP_DIR) help
	@echo ""
	@echo "  LiteLLM integration (this Makefile):"
	@grep -E '^[a-zA-Z_-]+:.*##' $(firstword $(MAKEFILE_LIST)) \
		| awk 'BEGIN{FS=":.*##"}{printf "    %-20s %s\n", $$1, $$2}'
