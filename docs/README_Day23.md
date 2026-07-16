# Day 23: RAG с реранкером (двухэтапное извлечение)

Одноэтапное извлечение превращается в **retrieve → rerank → filter**.

## Что нового
- `port.Reranker` + два адаптера, переоценивающих чанки 0..1 **выделенной rerank-моделью** (своя модель, опционально свой провайдер/эндпоинт/ключ, отдельно от отвечающей LLM):
  - chat-based cross-encoder (`reranker.go`, скоринг одним `/chat/completions`);
  - нативный rerank-API (`rerank_api.go`) — Cohere-style `POST /rerank` для purpose-built моделей (`cohere/rerank-*`, `rerank-v3.5`, `jina-reranker-*`).
- Флаг `--rag-rerank-mode` (`api`/`chat`/`auto`, по умолч. `auto` — `api`, если в имени модели есть «rerank»).
- `usecase.RAGUseCase` принимает опциональный реранкер и `RAGConfig` (`TopKRetrieve`, `Rerank`, `Threshold`, `TopKFinal`); возвращает `RAGResult` с разбивкой по этапам. Чанки ниже порога отбрасываются, выжившие обрезаются до `TopKFinal`.
- Флаги: `--rag-rerank`, `--rag-rerank-model` / `--rag-rerank-mode` / `--rag-rerank-provider` / `--rag-rerank-url` / `--rag-rerank-key`, `--rag-threshold`, `--rag-top-k-final` (`--rag-top-k` теперь — размер пула ДО фильтрации).
- Интеграционный тест `TestRAGRerankCompare` (`//go:build integration`, `run-rag-rerank-compare`): 10 контрольных вопросов через `RAGUseCase` с реранкером и без, side-by-side отчёт (`rag_rerank_compare_result.txt`).

## Далее
Day24 делает ответ структурированным и обоснованным (источники + цитаты).
