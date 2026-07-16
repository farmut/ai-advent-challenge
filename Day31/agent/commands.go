package main

import (
	"fmt"
	"strings"
)

// cmdKind is the classification of an interactive input line: either regular
// text meant for the model, or one of the slash commands.
type cmdKind int

const (
	cmdInput   cmdKind = iota // regular input → send to the agent
	cmdHelp                   // /help, /помощь — enter documentation-consultant mode
	cmdEnd                    // /end, /конец — leave documentation-consultant mode
	cmdAgents                 // /agents, /агенты
	cmdMemory                 // /memory, /память
	cmdMCP                    // /mcp, /mcp-list
	cmdTools                  // /tools, /инструменты
	cmdCopy                   // /copy, /копировать — copy last answer to clipboard
	cmdSelect                 // /select, /выделение — toggle mouse text-selection mode
	cmdKeys                   // /keys, /клавиши — toggle key-press diagnostics in the log
	cmdClear                  // /clear, /очистить
	cmdQuit                   // /exit, /quit, /выход
	cmdUnknown                // an unrecognised /command (NOT sent to the model)
)

// replCommand describes one slash command: its aliases and a short description.
type replCommand struct {
	kind  cmdKind
	names []string
	desc  string
}

// orchestratorCommands is the command set for the interactive orchestrator
// (both the plain REPL and the TUI). Every entry has a Russian and an English
// alias so /help and /помощь (and their siblings) both work.
var orchestratorCommands = []replCommand{
	{cmdHelp, []string{"/help", "/помощь", "/?"}, "режим консультанта по документации проекта (+ список команд)"},
	{cmdEnd, []string{"/end", "/конец"}, "выйти из режима консультанта по документации"},
	{cmdAgents, []string{"/agents", "/агенты"}, "показать саб-агентов из конфига"},
	{cmdMemory, []string{"/memory", "/память"}, "показать память оркестратора (задача/WM/LTM)"},
	{cmdMCP, []string{"/mcp", "/mcp-list", "/мсп"}, "показать подключённые MCP-серверы"},
	{cmdTools, []string{"/tools", "/инструменты"}, "показать доступные MCP-инструменты"},
	{cmdCopy, []string{"/copy", "/копировать"}, "скопировать последний ответ в буфер обмена"},
	{cmdSelect, []string{"/select", "/выделение"}, "переключить: тачпад-прокрутка ↔ нативное выделение мышью (TUI)"},
	{cmdKeys, []string{"/keys", "/клавиши"}, "диагностика клавиш: показывать коды нажатий в логе (TUI)"},
	{cmdClear, []string{"/clear", "/очистить"}, "очистить вывод на экране"},
	{cmdQuit, []string{"/exit", "/quit", "/выход"}, "выйти из интерактивного режима"},
}

// classifyInput maps a raw input line to a command kind against cmds. Any line
// starting with '/' is treated as a command and never forwarded to the model:
// an unrecognised slash line yields cmdUnknown (so a typo is reported, not sent).
// Everything else yields cmdInput.
func classifyInput(s string, cmds []replCommand) cmdKind {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "/") {
		return cmdInput
	}
	word := strings.ToLower(strings.Fields(t)[0])
	for _, c := range cmds {
		for _, n := range c.names {
			if n == word {
				return c.kind
			}
		}
	}
	return cmdUnknown
}

// renderCommandHelp formats the command list for the /help output.
func renderCommandHelp(cmds []replCommand) string {
	var b strings.Builder
	b.WriteString("Доступные команды:\n")
	for _, c := range cmds {
		fmt.Fprintf(&b, "  %-26s %s\n", strings.Join(c.names, ", "), c.desc)
	}
	b.WriteString("\nЛюбая строка, начинающаяся с «/», трактуется как команда и не отправляется модели.")
	return b.String()
}
