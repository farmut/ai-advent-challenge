# Day 25: диалоговая память RAG-сеанса

Интерактивный RAG-сеанс становится диалоговым и с памятью.

## Что нового (в `runRAGInteractive`)
1. **Персист истории диалога** — загружается при старте (сеанс возобновляется), дозаписывается после каждого хода; путь выводится из `--history`.
2. **Свежий поиск на каждый вопрос** — полный конвейер retrieve → [rerank] → filter; запрос «якорится» на цель диалога.
3. **Память задачи** — цель диалога, что уже уточнено, зафиксированные ограничения/термины.

## Реализация
- `usecase/task_memory.go`: `TaskMemorySystemBlock` рендерит память как секцию системного сообщения («не переспрашивай уже уточнённое»), `UpdateTaskMemory` обновляет goal/clarified/constraints после каждого обмена (JSON round-trip, при ошибке прежнее состояние сохраняется).
- `domain.TaskMemory`, `port.TaskMemoryRepository`, `storage.NewTaskMemoryFile` / `TaskMemoryPath` (`*.taskmem.json` рядом с историей).
- `RAGUseCase.AnswerWithContext` принимает `ConversationContext{History, TaskMemory}`: окно истории (`ragHistoryWindow = 8`, т.е. 4 хода, через `SlidingWindow`) + блок памяти задачи.
- REPL-команда `/memory` печатает память задачи.

## Далее
Day26 — чекпоинт (без изменений кода).
