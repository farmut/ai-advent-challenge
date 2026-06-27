package domain

// MCPServerType identifies the transport used to communicate with an MCP server.
type MCPServerType string

const (
	MCPStdio MCPServerType = "stdio"
	MCPSSE   MCPServerType = "sse"
)

// MCPServerConfig holds connection details for a single MCP server entry.
type MCPServerConfig struct {
	Name    string        `yaml:"name"`
	Type    MCPServerType `yaml:"type"`
	Command string        `yaml:"command,omitempty"` // stdio: executable path
	Args    []string      `yaml:"args,omitempty"`    // stdio: command arguments
	Env     []string      `yaml:"env,omitempty"`     // stdio: extra env vars (KEY=VALUE)
	URL     string        `yaml:"url,omitempty"`     // sse: SSE endpoint URL
}

// MCPConfig is the top-level document stored in the YAML config file.
type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

// MCPTool describes a single tool exposed by an MCP server.
type MCPTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema,omitempty"`
}
