# Day 11: 3-слойная память + Clean Architecture

Ключевой рефакторинг: введена архитектура пакетов и трёхслойная система памяти.

## Что нового
- **Clean Architecture**: пакеты под `internal/` — `domain`, `port`, `adapter/llm`, `adapter/storage`, `usecase`.
- **3 слоя памяти**:
  - Layer 1 — STM: история диалога (`--history`, `*.json`).
  - Layer 2 — WM: рабочая память / факты задачи (`--memory-wm`, `*.wm.json`).
  - Layer 3 — LTM: долгосрочная память / профиль (`--memory-ltm`, `*.ltm.json`).
- `--memory-update` — авто-обновление WM+LTM после каждого вызова (2 доп. LLM-вызова).
- Слои памяти инжектятся как префиксы системного сообщения.
- Пути WM/LTM выводятся из `--history`, если не заданы явно.

## Раскладка пакетов
```
internal/
  domain/    — Message, Usage, SessionStats, FactsStore, WorkingMemory, LongTermMemory, BranchState, pricing
  port/      — контракты: LLMClient, HistoryRepository, StatsRepository, SummaryRepository, FactsRepository, WorkingMemoryRepository, LongTermMemoryRepository, BranchRepository
  adapter/llm/      — HTTP-клиент (OpenAI-совместимый)
  adapter/storage/  — файловые реализации репозиториев
  usecase/   — ChatUseCase, BranchUseCase, HistoryUseCase, MemoryUseCase
```

## Далее
Day12 добавляет управление профилем пользователя.
