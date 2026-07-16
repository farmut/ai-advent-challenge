# Day 21: standalone RAG-конвейер

Отдельный компонент `rag/` — индексация корпуса и поиск.

## Что нового
- Модуль `rag/` на Clean Architecture: пакеты `internal/` — `reader`, `chunker`, `embedder`, `store`, `domain`.
- Подкоманды `index` / `search` / `stats`.
- Чанкинг: fixed и structural.
- Подключаемые эмбеддинги: локальный Ollama `nomic-embed-text` по умолчанию, LM Studio, или любой OpenAI-совместимый эндпоинт через `EMBED_URL` / `EMBED_MODEL` / `EMBED_KEY`.
- Персистентный векторный store.
- Корпус: `postgresql_internals-15.pdf`, `lib.txt`.

## Далее
Day22 подключает RAG в агент.
