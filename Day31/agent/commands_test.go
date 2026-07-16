package main

import (
	"strings"
	"testing"
)

func TestClassifyInput(t *testing.T) {
	cases := []struct {
		in   string
		want cmdKind
	}{
		{"проанализируй репозиторий", cmdInput},
		{"", cmdInput},
		{"/help", cmdHelp},
		{"/помощь", cmdHelp},
		{"/HELP", cmdHelp}, // case-insensitive
		{"/?", cmdHelp},
		{"/agents", cmdAgents},
		{"/агенты", cmdAgents},
		{"/memory", cmdMemory},
		{"/память", cmdMemory},
		{"/mcp", cmdMCP},
		{"/mcp-list", cmdMCP},
		{"/tools", cmdTools},
		{"/инструменты", cmdTools},
		{"/copy", cmdCopy},
		{"/копировать", cmdCopy},
		{"/select", cmdSelect},
		{"/выделение", cmdSelect},
		{"/clear", cmdClear},
		{"/exit", cmdQuit},
		{"/выход", cmdQuit},
		{"/quit extra args", cmdQuit}, // only the first word matters
		{"/bogus", cmdUnknown},
		{"/", cmdUnknown},
		{"  /help  ", cmdHelp}, // trimmed
	}
	for _, c := range cases {
		if got := classifyInput(c.in, orchestratorCommands); got != c.want {
			t.Errorf("classifyInput(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// A slash line must never be classified as model input, so the REPL/TUI never
// forward it to the LLM.
func TestSlashNeverInput(t *testing.T) {
	for _, in := range []string{"/help", "/bogus", "/", "/exit", "/память"} {
		if classifyInput(in, orchestratorCommands) == cmdInput {
			t.Errorf("%q must not be treated as model input", in)
		}
	}
}

func TestRenderCommandHelp(t *testing.T) {
	h := renderCommandHelp(orchestratorCommands)
	for _, want := range []string{"/help", "/помощь", "/agents", "/memory", "/mcp", "/tools", "/copy", "/select", "/exit"} {
		if !strings.Contains(h, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}
