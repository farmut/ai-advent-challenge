package usecase

import (
	"context"
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// MCPUseCase manages MCP server configurations and tool discovery.
type MCPUseCase struct {
	repo   port.MCPRepository
	lister port.MCPToolLister
}

// NewMCPUseCase wires the use case with a config repository and a tool lister.
func NewMCPUseCase(repo port.MCPRepository, lister port.MCPToolLister) *MCPUseCase {
	return &MCPUseCase{repo: repo, lister: lister}
}

// AddServer adds a new MCP server to the config file.
// Returns an error if a server with the same name already exists.
func (uc *MCPUseCase) AddServer(cfg domain.MCPServerConfig) error {
	if err := validateServerConfig(cfg); err != nil {
		return err
	}
	current, err := uc.repo.Load()
	if err != nil {
		return fmt.Errorf("loading MCP config: %w", err)
	}
	for _, s := range current.Servers {
		if s.Name == cfg.Name {
			return fmt.Errorf("MCP server %q already exists (use --mcp-remove to remove it first)", cfg.Name)
		}
	}
	current.Servers = append(current.Servers, cfg)
	return uc.repo.Save(current)
}

// RemoveServer removes the named server from the config file.
func (uc *MCPUseCase) RemoveServer(name string) error {
	current, err := uc.repo.Load()
	if err != nil {
		return fmt.Errorf("loading MCP config: %w", err)
	}
	filtered := current.Servers[:0]
	found := false
	for _, s := range current.Servers {
		if s.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, s)
	}
	if !found {
		return fmt.Errorf("MCP server %q not found", name)
	}
	current.Servers = filtered
	return uc.repo.Save(current)
}

// ListServers returns all configured MCP servers.
func (uc *MCPUseCase) ListServers() ([]domain.MCPServerConfig, error) {
	cfg, err := uc.repo.Load()
	if err != nil {
		return nil, fmt.Errorf("loading MCP config: %w", err)
	}
	return cfg.Servers, nil
}

// ListTools connects to the named server (or all servers when name=="") and
// returns a map of server name → tools.  Errors from individual servers are
// collected but do not prevent other servers from being queried.
func (uc *MCPUseCase) ListTools(ctx context.Context, serverName string) (map[string][]domain.MCPTool, []error) {
	cfg, err := uc.repo.Load()
	if err != nil {
		return nil, []error{fmt.Errorf("loading MCP config: %w", err)}
	}

	results := make(map[string][]domain.MCPTool)
	var errs []error

	for _, srv := range cfg.Servers {
		if serverName != "" && srv.Name != serverName {
			continue
		}
		tools, err := uc.lister.ListTools(ctx, srv)
		if err != nil {
			errs = append(errs, fmt.Errorf("server %q: %w", srv.Name, err))
			continue
		}
		results[srv.Name] = tools
	}

	if serverName != "" && len(results) == 0 && len(errs) == 0 {
		errs = append(errs, fmt.Errorf("MCP server %q not found", serverName))
	}

	return results, errs
}

// validateServerConfig performs basic sanity checks on a server config before saving.
func validateServerConfig(cfg domain.MCPServerConfig) error {
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("--mcp-name is required")
	}
	switch cfg.Type {
	case domain.MCPStdio:
		if cfg.Command == "" {
			return fmt.Errorf("--mcp-command is required for stdio servers")
		}
	case domain.MCPSSE:
		if cfg.URL == "" {
			return fmt.Errorf("--mcp-url is required for sse servers")
		}
	default:
		return fmt.Errorf("--mcp-type must be %q or %q", domain.MCPStdio, domain.MCPSSE)
	}
	return nil
}
