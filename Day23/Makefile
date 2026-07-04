.PHONY: build test vet clean help \
        agent-build agent-check-day16 agent-setup-mcp agent-profile-init \
        run-agent run-agent-persist \
        run-smoke run-live-get-pet run-live-inventory \
        check-gigachat-env \
        run-gigachat-ping run-gigachat-agent run-gigachat-persist run-gigachat-tools \
        run-multi-mcp-setup run-multi-mcp-tools-check run-multi-mcp-session

MCP_DIR   := $(CURDIR)/mcp-server
AGENT_DIR ?= $(MCP_DIR)/../agent
AGENT_BIN ?= $(abspath $(AGENT_DIR))/ai-adv-agent-day20

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

help: ## Show available targets
	@$(MAKE) -C $(MCP_DIR) help
