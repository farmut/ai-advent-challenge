package storage

import (
	"fmt"
	"os"

	"ai-adv-agent/internal/domain"
	"gopkg.in/yaml.v3"
)

// MCPConfigFile is a file-backed MCPRepository that stores server configs in YAML.
type MCPConfigFile struct{ path string }

// NewMCPConfigFile creates an MCPConfigFile at the given path (should end in .yaml).
func NewMCPConfigFile(path string) *MCPConfigFile { return &MCPConfigFile{path} }

func (r *MCPConfigFile) Load() (domain.MCPConfig, error) {
	empty := domain.MCPConfig{}
	if r.path == "" {
		return empty, nil
	}
	data, err := os.ReadFile(r.path)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("cannot read MCP config file: %w", err)
	}
	var cfg domain.MCPConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return empty, fmt.Errorf("invalid MCP config YAML: %w", err)
	}
	return cfg, nil
}

func (r *MCPConfigFile) Save(cfg domain.MCPConfig) error {
	if r.path == "" {
		return nil
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot marshal MCP config: %w", err)
	}
	return os.WriteFile(r.path, data, 0644)
}
