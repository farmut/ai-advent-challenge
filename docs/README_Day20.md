# Day 20: multi-MCP и GigaChat

Агент говорит с несколькими MCP-серверами сразу и получает нового провайдера.

## Что нового
- **Multi-MCP**: пул подключений (`agent/internal/adapter/mcp/pool.go`) — агент работает с несколькими серверами одновременно (напр. Petstore + GitHub MCP + filesystem workspace).
- Интеграция провайдера **GigaChat** (цели `run-gigachat-*` / `run-multi-mcp-*`).
- Аналитический профиль (`profile_analitic.md`, анализ GitHub-проекта) + `lib.txt`.

## Далее
Day21 добавляет отдельный RAG-конвейер (`rag/`).
