# Day 22: RAG в агенте

RAG встроен в пайплайн запроса агента: *вопрос → поиск релевантных чанков → объединение с вопросом → LLM*.

## Что нового (три компонента: `rag/`, `agent/`, `mcp-server/`)
- `port.Retriever` + самодостаточный адаптер извлечения (`agent/internal/adapter/rag/`): HTTP-клиент эмбеддингов + read-only SQLite-поиск по индексу, построенному компонентом `rag` (зависимость `modernc.org/sqlite`).
- `usecase.RAGUseCase` / `BuildRAGPrompt` — заземляет вопрос на извлечённый контекст.
- Флаги: `--rag`, `--rag-db`, `--rag-top-k`, `--rag-embed-url`, `--rag-embed-model`, `--rag-embed-key`.
- Makefile-цель `run-rag`.

> Запрос эмбеддится **той же моделью**, что и индекс.

## Далее
Day23 добавляет этап реранкинга (retrieve → rerank → filter).
