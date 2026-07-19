BINARY_NAME := git-mcp-server

.PHONY: build test vet clean help

build: ## Build the git MCP server binary
	go build -ldflags "-s -w" -o $(BINARY_NAME) .

test: ## Run unit tests (uses a throw-away temp git repository)
	go test -v ./...

vet: ## Static analysis
	go vet ./...

clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
