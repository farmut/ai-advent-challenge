# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

This is an "AI Advent Challenge" — a series of incremental daily Go exercises (Day1–Day24), each building a more capable CLI tool for querying LLM APIs. Every day is an **independent Go module** with its own `go.mod`, binary, and `Makefile`.

Starting from Day11 the code is structured using **Clean Architecture** with packages under `internal/` (`domain`, `port`, `adapter`, `usecase`). From Day17 onward a day is no longer a single binary but a small **multi-component workspace** (`agent/` + `mcp-server/`, later `+ rag/`), each component its own Go module.

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
- **Day16** — Adds MCP (Model Context Protocol) client support to the agent: connect to external tool servers over two transports — `stdio` (server launched as a subprocess, JSON-RPC 2.0 over stdin/stdout) and `sse` (HTTP + Server-Sent Events). Servers are declared in a YAML config; new `/mcp-*` slash commands (list servers, list tools, etc.) work inside the interactive session.
- **Day17** — Introduces a custom Go **Petstore MCP server** (`mcp-server/`) that wraps the public [Swagger Petstore](https://petstore.swagger.io/) REST API in 18 MCP tools (`pet_*`, `store_*`, `user_*`), served over `stdio`. The day becomes a two-component workspace: `agent/` (MCP client from Day16) + `mcp-server/`.
- **Day18** — Adds an **HTTP+SSE transport** to the MCP server (persistent-service mode via `-addr`) plus a **background report collector** goroutine that periodically snapshots sold pets independently of agent calls (`report_start_collection` / `report_stop_collection` / `report_collection_status` / `report_show_sold`), growing the tool count to 22. The Makefile's `run-agent` auto-starts the MCP server in SSE mode and writes the agent's config.
- **Day19** — Checkpoint that refines the Day18 server: reworked report collector and tool implementations (`petstore/report.go`, `petstore/tools.go`) with test updates. Same 22-tool surface.
- **Day20** — Adds **multi-MCP** support: an MCP connection pool (`agent/internal/adapter/mcp/pool.go`) lets the agent talk to several servers at once (e.g. Petstore + GitHub MCP + filesystem workspace). Also adds **GigaChat** provider integration (dedicated `run-gigachat-*` / `run-multi-mcp-*` Makefile targets) and an analytics profile (`profile_analitic.md`, GitHub project analysis) plus `lib.txt`.
- **Day21** — Adds a standalone **RAG pipeline** (`rag/` module) with Clean Architecture packages under `internal/` (`reader`, `chunker`, `embedder`, `store`, `domain`) and `index` / `search` / `stats` subcommands. Supports fixed and structural chunking, pluggable embeddings (local Ollama `nomic-embed-text` by default, LM Studio, or any OpenAI-compatible endpoint via `EMBED_URL` / `EMBED_MODEL` / `EMBED_KEY`), and a persistent vector store. Ships a source corpus (`postgresql_internals-15.pdf`, `lib.txt`).
- **Day22** — Started as a copy of Day21 (three components: `rag/`, `agent/`, `mcp-server/`). Wires **RAG into the agent** so a CLI query runs the pipeline *question → retrieve relevant chunks → combine with question → LLM*. New agent pieces: `port.Retriever`, a self-contained retrieval adapter (`agent/internal/adapter/rag/` — embeddings HTTP client + read-only SQLite search over the index built by the `rag` component, added `modernc.org/sqlite` dependency), and `usecase.RAGUseCase` / `BuildRAGPrompt` (grounds the question on the retrieved context). New CLI flags: `--rag`, `--rag-db`, `--rag-top-k`, `--rag-embed-url`, `--rag-embed-model`, `--rag-embed-key`; Makefile target `run-rag`.
- **Day23** — Started as a copy of Day22 (three components: `rag/`, `agent/`, `mcp-server/`). Turns the agent's single-stage RAG retrieval into a **two-stage retrieve → rerank → filter** pipeline. New `port.Reranker` interface with two adapters that re-score retrieved chunks 0..1 using a **dedicated rerank model** (its own model, and optionally its own provider/endpoint/key, kept separate from the answer-generating LLM): a chat-based cross-encoder (`agent/internal/adapter/rag/reranker.go`, scores via one `/chat/completions` call) and a **native rerank-API** adapter (`agent/internal/adapter/rag/rerank_api.go`) that calls the Cohere-style `POST /rerank` endpoint used by purpose-built rerank models (`cohere/rerank-*`, `rerank-v3.5`, `jina-reranker-*`) which are not chat models. A `--rag-rerank-mode` flag (`api`/`chat`/`auto`, default `auto` — picks `api` when the model name contains "rerank") routes between them. `usecase.RAGUseCase` now takes an optional reranker and a `RAGConfig` (`TopKRetrieve`, `Rerank`, `Threshold`, `TopKFinal`), returning a `RAGResult` that breaks down each stage; chunks scoring below the threshold (rerank score when reranked, else similarity) are dropped and the survivors capped to `TopKFinal`. New CLI flags: `--rag-rerank`, `--rag-rerank-model` / `--rag-rerank-mode` / `--rag-rerank-provider` / `--rag-rerank-url` / `--rag-rerank-key` (each falls back to the main LLM config), `--rag-threshold`, `--rag-top-k-final` (and `--rag-top-k` now means the retrieval pool size *before* filtering). Makefile `run-rag` gains `RAG_RERANK` / `RAG_RERANK_MODEL` / `RAG_THRESHOLD` / `RAG_TOP_K_FINAL` variables. Adds an integration test `TestRAGRerankCompare` (`agent/rag_rerank_compare_test.go`, `//go:build integration`, Makefile `run-rag-rerank-compare`) that runs the 10 control questions through `RAGUseCase` twice — with and without the reranker on the same retrieval pool/threshold/cap — and writes a side-by-side report (`rag_rerank_compare_result.txt`) scoring context precision (expected sources kept), answer keyword coverage, and citations.
- **Day24** — Started as a copy of Day23 (three components: `rag/`, `agent/`, `mcp-server/`). Turns the agent's RAG output into a **structured, grounded answer with citations**. New domain types (`agent/internal/domain/rag.go`: `AnswerSource{Source, Section, ChunkID, Score}`, `AnswerQuote{Source, Section, ChunkID, Text}`, `RAGAnswer{Answer, Sources, Quotes, Grounded}`) and a new usecase (`agent/internal/usecase/rag_answer.go`): `RAGUseCase.Answer` runs the full pipeline (retrieve → [rerank] → filter → grounded LLM call) and returns the answer **plus the sources it relies on** (source + section/chunk_id) and **verbatim quotes** (fragments of the retrieved chunks). `BuildAnswerPrompt` asks the model for JSON (`{answer, sources:[marker], quotes:[{marker,text}]}`) referencing each context chunk's `[n]` marker; `parseRAGAnswer` maps markers back to concrete chunks and falls back to citing all retrieved chunks if the model returns prose or omits sources/quotes. **Low-relevance guard:** when nothing clears `--rag-threshold` the pipeline short-circuits *without an LLM call* to an honest "Не знаю…" reply (`Grounded=false`, no sources/quotes) asking the user to clarify. `main.go`'s `--rag` path now prints the structured answer via `printRAGAnswer` (`=== Источники ===` / `=== Цитаты ===`) and returns, bypassing the plain chat pipeline. New integration test `TestRAGGroundedAnswers` (`agent/rag_grounded_test.go`, `//go:build integration`, Makefile `run-rag-grounded`) runs the 10 control questions and asserts each answer (1) has sources, (2) has quotes, (3) has an answer whose **meaning matches its quotes** — verified by an LLM judge returning `SUPPORTED`/`UNSUPPORTED` — plus a low-relevance spot-check that an off-topic question yields the ungrounded "не знаю" reply; results go to `rag_grounded_result.txt`. Also adds an **interactive RAG session**: `--rag --interactive` runs a REPL (`runRAGInteractive` in `main.go`) that answers each typed question with the grounded pipeline (answer + sources + quotes, `/exit`/`/quit` to leave); the FSM agent path is gated to plain `--interactive` only. Makefile target `run-rag-interactive` (reranks by default with `cohere/rerank-4-fast`, retrieve 20 → threshold 0.5 → keep 10, all `RAG_*` knobs overridable).

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
