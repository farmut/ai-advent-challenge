# Day 24: структурированный обоснованный ответ

RAG-вывод превращается в **структурированный ответ с цитированием**.

## Что нового
- Новые domain-типы (`agent/internal/domain/rag.go`): `AnswerSource{Source, Section, ChunkID, Score}`, `AnswerQuote{Source, Section, ChunkID, Text}`, `RAGAnswer{Answer, Sources, Quotes, Grounded}`.
- `usecase.RAGUseCase.Answer` — полный пайплайн (retrieve → [rerank] → filter → grounded LLM-вызов), возвращает ответ + **источники** (файл + раздел/`chunk_id`) + **дословные цитаты**.
- `BuildAnswerPrompt` просит модель вернуть JSON (`{answer, sources:[marker], quotes:[{marker,text}]}`) со ссылками на маркеры `[n]`; `parseRAGAnswer` мапит маркеры обратно на чанки, при прозе/пропусках откатывается на цитирование всех извлечённых чанков.
- **Guard низкой релевантности**: если ничего не прошло `--rag-threshold`, пайплайн короткозамыкается **без вызова LLM** на честное «Не знаю…» (`Grounded=false`).
- `main.go`: `--rag` печатает структурированный ответ (`=== Источники ===` / `=== Цитаты ===`).
- **Интерактивный RAG-сеанс** `--rag --interactive` (`runRAGInteractive`): REPL с grounded-ответами (`/exit`/`/quit`).
- Интеграционный тест `TestRAGGroundedAnswers` (`run-rag-grounded`): 10 вопросов, проверка источников/цитат/смысла (LLM-судья `SUPPORTED`/`UNSUPPORTED`) + спот-проверка guard'а. Отчёт `rag_grounded_result.txt`.

## Далее
Day25 добавляет RAG-сеансу память диалога и память задачи.
