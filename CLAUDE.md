# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

This is an "AI Advent Challenge" — a series of incremental daily Go exercises (Day1–Day15), each building a more capable CLI tool for querying LLM APIs. Every day is an **independent Go module** with its own `go.mod`, binary, and `Makefile`.

Starting from Day11 the code is structured using **Clean Architecture** with packages under `internal/` (`domain`, `port`, `adapter`, `usecase`).

## Common Commands (run inside each `DayN/` directory)

```bash
# Build
go build -o ai-adv-agent-dayN .
# or
make build

# Unit tests (no API key needed — use httptest mock server)
go test -v ./...
make test

# Static analysis
go vet ./...
make vet

# Clean build artifacts
make clean
```

## Required Environment Variables

All days share the same env interface:

| Variable | Required | Default |
|---|---|---|
| `LLM_PROVIDER` | Yes | — (`openai` or `openrouter`) |
| `LLM_API_KEY` | Yes | — |
| `LLM_MODEL` | No | `gpt-4o` (OpenAI) or `openai/gpt-4o-mini` (OpenRouter) |
| `LLM_BASE_URL` | No | Provider default |

## Architecture: Day-by-Day Progression

Each day extends the previous by adding one capability:

- **Day1** — Basic query via `go-openai` SDK; renders Markdown with `charmbracelet/glamour`.
- **Day2** — Rewritten using raw `net/http` (no SDK). Adds `--format`, `--format-hint`, `--max-tokens`, `--stop`, `--debug`.
- **Day3** — Adds `--system` (system role message). Makefile includes a 6-step pipeline: 4 parallel solution variants → merged input → final reviewer LLM call. Results written to `report_*.txt` / `final_report.txt`.
- **Day4** — Adds `--temperature` (0.0–2.0). Makefile's `run-temperature` target generates 3 responses at temps 0 / 0.7 / 1.2 into `temperature_result.txt`, then calls a revisor LLM for comparative evaluation.
- **Day5** — Adds dynamic model selection per call (`LLM_MODEL` env override inside the loop). Makefile's `run-integration-batch` iterates over three Qwen models, collects answers into `test_output.txt`, then calls an expert model for final evaluation.
- **Day6** — Adds persistent chat history (`--history`, JSON file). History is loaded before each call and saved after; `--history ""` disables persistence.
- **Day7** — Same codebase as Day6 (incremental checkpoint / refactoring step).
- **Day8** — Adds token tracking and cost estimation (`--show-tokens`, `--show-cost`). Introduces `knownPrices` table (OpenAI / Anthropic / Qwen via OpenRouter), per-call cost, session stats, and context-window usage display.
- **Day9** — Adds `--history-limit` and auto-summarization: when history exceeds the limit, older messages are summarized by the LLM and stored in `<history>.summary.txt`, which is injected back as a system message prefix.
- **Day10** — Adds three context management strategies (`--strategy`): `sliding-window` (keep last N messages), `sticky-facts` (KV facts store survives window rotation), `branching` (independent conversation branches with named checkpoints). Branch management flags: `--checkpoint`, `--branch-new`, `--from-checkpoint`, `--branch-switch`, `--branch-list`.
- **Day11** — Adds a 3-layer memory system and refactors to **Clean Architecture** (`internal/domain`, `internal/port`, `internal/adapter/llm`, `internal/adapter/storage`, `internal/usecase`). New flags: `--memory-wm` (Layer 2: working memory / task facts), `--memory-ltm` (Layer 3: long-term memory / profile), `--memory-update` (auto-update WM+LTM after each call via 2 extra LLM calls). Memory layers inject their content as system-message prefixes.
- **Day12** — Adds user profile management (`--profile`, `--profile-init`, `--profile-name`, `--profile-set`, `--profile-delete`, `--profile-list`). Profile is a Markdown file injected as the top layer of every system message.
- **Day13** — Adds interactive mode (`--interactive`) with a 4-phase task state machine (Planning → Execution → Validation → Done). Full pause/resume support: state is persisted to `<history>.task.json`; the task can be resumed across restarts.
- **Day14** — Adds invariant enforcement (`--invariants`). Invariants are absolute constraints loaded from a Markdown file and injected into every Planning and Execution system prompt. Three automatic compliance gates: Task Gate (task description check on iteration 1), Plan Gate (up to 3 silent retries before showing a warning), Validation Gate (auto-return to Planning on violation). Slash commands `/exit`, `/restart`, `/pause` work at any FSM prompt.
- **Day15** — Replaces `y`/`yes` approvals with `/yes`-only review prompts (`reviewPrompt()`) at all FSM gates (Planning, Execution continue, Validation, startup resume). Adds `PendingPlan` field: plan is persisted before being shown to the user so resume after a pause restores the exact same plan without an LLM re-call. Upgrades compliance checking to chain-of-thought: the checker LLM must analyse each invariant individually before writing a final `COMPLIANT` / `VIOLATION:` verdict on the last line; the parser reads only that last line, preventing mid-analysis text from masking a violation.

### Memory Layer Architecture (Day11+)

| Layer | Flag | File | Purpose |
|---|---|---|---|
| Layer 1 — STM | `--history` | `*.json` | Dialogue history (short-term memory) |
| Layer 2 — WM | `--memory-wm` | `*.wm.json` | Task facts for the current session |
| Layer 3 — LTM | `--memory-ltm` | `*.ltm.json` | User profile, stable preferences, strategic decisions |

Default paths for WM and LTM are derived automatically from the `--history` path if not set explicitly.

### Clean Architecture Package Layout (Day11+)

```
internal/
  domain/          — entities: Message, Usage, SessionStats, FactsStore, WorkingMemory, LongTermMemory, BranchState, pricing
  port/            — interface contracts: LLMClient, HistoryRepository, StatsRepository, SummaryRepository,
                     FactsRepository, WorkingMemoryRepository, LongTermMemoryRepository, BranchRepository
  adapter/
    llm/           — HTTP client implementing LLMClient (OpenAI-compatible)
    storage/       — file-backed implementations of all repository ports
  usecase/         — ChatUseCase, BranchUseCase, HistoryUseCase, MemoryUseCase (UpdateWM / UpdateLTM / UpdateFacts)
```

## Testing Pattern

Unit tests (`main_test.go` and `internal/**/*_test.go`) spin up a local `httptest.NewServer` to capture the outgoing JSON payload without hitting a real API. This lets tests verify request construction (model, messages, stop sequences, temperature, etc.) in isolation.

## Prompt Template Files (Day3+)

Each day with complex workflows uses `.txt` files as prompt sources:

- `query.txt` — the user query
- `system.txt` / `system_*.txt` — system role messages for different personas
- `query_by_steps.txt` — step-by-step variant of the query
- `system_revisor.txt` (Day4) — reviewer system prompt for temperature comparison
- `system_expert.txt` (Day5) — expert evaluator system prompt for batch model comparison
- `system_critic.txt` (Day10+) — AI critic prompt for strategy/quality comparison

These files are read at runtime by the Makefile targets (not compiled in).
