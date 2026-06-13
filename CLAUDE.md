# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

This is an "AI Advent Challenge" — a series of incremental daily Go exercises (Day1–Day5), each building a more capable CLI tool for querying LLM APIs. Every day is an **independent Go module** with its own `go.mod`, binary, and `Makefile`.

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

Each day extends the previous by adding one capability to the same `sendChatRequest` core function pattern:

- **Day1** — Basic query via `go-openai` SDK; renders Markdown with `charmbracelet/glamour`.
- **Day2** — Rewritten using raw `net/http` (no SDK). Adds `--format`, `--format-hint`, `--max-tokens`, `--stop`, `--debug`.
- **Day3** — Adds `--system` (system role message). Makefile includes a 6-step pipeline: 4 parallel solution variants → merged input → final reviewer LLM call. Results written to `report_*.txt` / `final_report.txt`.
- **Day4** — Adds `--temperature` (0.0–2.0). Makefile's `run-temperature` target generates 3 responses at temps 0 / 0.7 / 1.2 into `temperature_result.txt`, then calls a revisor LLM for comparative evaluation.
- **Day5** — Adds dynamic model selection per call (`LLM_MODEL` env override inside the loop). Makefile's `run-integration-batch` iterates over three Qwen models, collects answers into `test_output.txt`, then calls an expert model for final evaluation.

## Testing Pattern

Unit tests (`main_test.go`) spin up a local `httptest.NewServer` to capture the outgoing JSON payload without hitting a real API. This lets tests verify request construction (model, messages, stop sequences, temperature, etc.) in isolation.

## Prompt Template Files (Day3+)

Each day with complex workflows uses `.txt` files as prompt sources:

- `query.txt` — the user query
- `system.txt` / `system_*.txt` — system role messages for different personas
- `query_by_steps.txt` — step-by-step variant of the query
- `system_revisor.txt` (Day4) — reviewer system prompt for temperature comparison
- `system_expert.txt` (Day5) — expert evaluator system prompt for batch model comparison

These files are read at runtime by the Makefile targets (not compiled in).
