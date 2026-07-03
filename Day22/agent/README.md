# Day 22: RAG-агент (retrieval-augmented generation)

Расширение Day21. Главное нововведение Day22 — **RAG в пайплайне агента**: запрос обогащается релевантными фрагментами из базы знаний перед обращением к LLM.

```
вопрос → поиск релевантных чанков → объединение с вопросом → запрос к LLM
```

Агент по-прежнему умеет всё из предыдущих дней (MCP-инструменты, 3-слойная память, профиль, инварианты, интерактивный FSM), а Day22 добавляет к CLI-режиму флаг `--rag` и связку с индексом, который строит компонент `rag/`.

## Что нового (Day2 → Day22)

| Возможность | День появления |
|---|---|
| Базовый HTTP-клиент, `--format`, `--debug` | Day2 |
| `--system` (системная роль) | Day3 |
| `--temperature` | Day4 |
| История диалога (`--history`), `--show-cost` | Day9 |
| Авто-суммаризация (`--summary`) | Day9 |
| Стратегии контекста (`--strategy`) | Day10 |
| Ветки и checkpoints (`--strategy branching`) | Day10 |
| 3-слойная память: WM + LTM + Clean Architecture | Day11 |
| Профиль пользователя (`--profile`) | Day12 |
| Интерактивный режим, FSM, пауза/возобновление | Day13 |
| Инварианты (`--invariants`), 3 compliance-gate | Day14 |
| `/yes`-only review prompts, `PendingPlan`, CoT compliance | Day15 |
| MCP-серверы: подключение, YAML-конфиг, `/mcp-*` slash-команды | Day16 |
| Собственный Petstore MCP-сервер (18 инструментов, stdio) | Day17 |
| HTTP+SSE транспорт, фоновый сборщик отчётов (22 инструмента) | Day18 |
| Multi-MCP (пул серверов), провайдер GigaChat | Day20 |
| Standalone RAG-конвейер (`rag/`: index/search/stats) | Day21 |
| **RAG в агенте: `--rag`, поиск→объединение→LLM** | **Day22** |
| **Интеграционные тесты: 10 контрольных вопросов, RAG vs no-RAG** | **Day22** |

---

## RAG (retrieval-augmented generation)

В Day22 CLI-запрос может быть «заземлён» (grounded) на базе знаний. Пайплайн:

1. **вопрос** — текст из `--query`;
2. **поиск** — вопрос эмбеддится и по косинусной близости ищутся top-K чанков в SQLite-индексе (`rag.db`), построенном компонентом `rag/`;
3. **объединение** — найденные фрагменты + вопрос собираются в единый grounded-промпт (`=== CONTEXT === … === QUESTION === …`) с инструкцией отвечать только по контексту и ссылаться на источники `[n]`;
4. **LLM** — промпт уходит в обычный `ChatUseCase.Execute`.

### Архитектура (Clean Architecture)

| Слой | Компонент |
|---|---|
| domain | `RetrievedChunk` — фрагмент + similarity |
| port | `Retriever` — интерфейс шага поиска |
| adapter | `internal/adapter/rag/` — HTTP-эмбеддер + read-only поиск по SQLite (`modernc.org/sqlite`) |
| usecase | `RAGUseCase` / `BuildRAGPrompt` — объединение контекста с вопросом |

Агент — отдельный Go-модуль, а пакеты компонента `rag/` лежат в его `internal/`, поэтому ретривер реализован внутри агента (переиспользовать через импорт нельзя из-за правила Go про internal-пакеты).

### Быстрый старт

```bash
cd Day22/agent

# LLM — как обычно (пример: OpenRouter). Эмбеддинги — локально.
export LLM_PROVIDER=openrouter
export LLM_API_KEY=your-api-key
export LLM_MODEL=deepseek/deepseek-v4-flash

make run-rag RAG_QUESTION="Что такое MVCC в PostgreSQL и зачем нужен VACUUM?"

# или напрямую:
./ai-adv-agent-day22 \
    --query "Что такое MVCC в PostgreSQL и зачем нужен VACUUM?" \
    --rag --rag-db ../rag/rag.db --rag-top-k 5 \
    --rag-embed-url http://127.0.0.1:1234 \
    --rag-embed-model text-embedding-nomic-embed-text-v2-moe \
    --history ""
```

> **Важно:** запрос нужно эмбеддить **той же моделью**, которой построен индекс. `../rag/rag.db` построен `text-embedding-nomic-embed-text-v2-moe` (768 dims). Другая модель (даже той же размерности) даёт бессмысленную близость (~0.06 вместо ~0.5).

### RAG-флаги

```
--rag                 Включить RAG: поиск релевантных чанков перед запросом к LLM
--rag-db <path>       SQLite-индекс, построенный компонентом rag/ (умолч. rag.db)
--rag-top-k <n>       Сколько чанков извлекать (умолч. 5)
--rag-embed-url <url> База URL эмбеддингов (умолч. http://localhost:11434)
--rag-embed-model <m> Модель эмбеддингов (должна совпадать с индексом)
--rag-embed-key <k>   API-ключ эмбеддингов (пусто для локальных рантаймов)
```

### Контрольные вопросы и интеграционные тесты

`eval_questions.json` — мини-набор из 10 контрольных вопросов по базе «PostgreSQL 15 изнутри». Для каждого задано ожидание (что должно быть в ответе) и источники, которые должны быть использованы (файл + раздел + anchor-слова для проверки извлечения).

```bash
# Проверка извлечения: для каждого вопроса нужный источник должен попасть в top-K
make run-rag-eval

# Полный пайплайн с ответом LLM (мягкая проверка терминов в ответе)
RUN_LLM=1 make run-rag-eval

# Сравнение «с RAG» vs «без RAG» с итоговым отчётом
RUN_LLM=1 make run-rag-compare
```

| Тест (тег `integration`) | Что делает | Отчёт |
|---|---|---|
| `TestRAGEval` | 10 вопросов → проверяет, что ожидаемые источники извлечены | `rag_eval_result.txt` |
| `TestRAGCompare` | каждый вопрос дважды (с RAG и без) → сравнение полноты и цитирования | `rag_compare_result.txt` |

Тесты `t.Skip`, если индекс или эндпоинт эмбеддингов недоступны; `TestRAGCompare` требует `RUN_LLM=1` и LLM-креды.

---

## MCP (Model Context Protocol)

MCP — открытый стандарт для подключения языковых моделей к внешним инструментам. Day19 использует собственный MCP-сервер (`Day19/mcp-server`), подключаемый через **HTTP+SSE транспорт**.

| Транспорт | Как работает | Пример |
|---|---|---|
| **stdio** | Сервер запускается как дочерний процесс; общение через stdin/stdout JSON-RPC 2.0 | `./petstore-mcp-server` |
| **sse** | Сервер работает постоянно по HTTP; подключение через Server-Sent Events, запросы через POST | `http://localhost:8080/sse` |

В Day19 используется **sse**: MCP-сервер стартует один раз и живёт на протяжении всей сессии агента. Это позволяет фоновому сборщику (`report_start_collection`) накапливать данные между вызовами.

### Конфигурационный YAML (генерируется автоматически)

`make run-agent` записывает конфиг автоматически перед запуском агента:

```yaml
servers:
  - name: petstore
    type: sse
    url: http://localhost:8080/sse
```

---

## Быстрый старт

```bash
cd Day19/mcp-server

export LLM_PROVIDER=openrouter
export LLM_API_KEY=your-api-key
export LLM_MODEL=openai/gpt-4o-mini

# Свежая сессия (история очищается каждый запуск)
make run-agent

# Постоянная сессия (история сохраняется между запусками)
make run-agent-persist

# С GigaChat провайдером
export LLM_API_KEY=<gigachat_oauth_key>
make run-gigachat-agent
```

`make run-agent` автоматически:
1. Собирает MCP-сервер (`petstore-mcp-server`)
2. Запускает его в HTTP+SSE режиме на `:8080`
3. Ждёт готовности (health-check до 3 с)
4. Записывает SSE-конфиг в `/tmp/petstore_mcp.yaml`
5. Запускает агент с этим конфигом
6. Завершает MCP-сервер при выходе

---

## Инструменты Petstore (22 штуки)

MCP-сервер предоставляет 22 инструмента. Агент вызывает их автоматически через механизм tool calling:

| Группа | Инструменты |
|---|---|
| **Pet** | `pet_add`, `pet_update`, `pet_find_by_status`, `pet_find_by_tags`, `pet_get_by_id`, `pet_update_with_form`, `pet_delete` |
| **Store** | `store_get_inventory`, `store_place_order`, `store_get_order`, `store_delete_order` |
| **User** | `user_create`, `user_create_with_list`, `user_login`, `user_logout`, `user_get`, `user_update`, `user_delete` |
| **Reports** | `report_start_collection`, `report_stop_collection`, `report_collection_status`, `report_show_sold` |

### Фоновый сбор отчётов

Инструменты группы **Reports** управляют горутиной внутри MCP-сервера, которая периодически запрашивает проданных питомцев и сохраняет снимки в JSON-файл:

```
Task> Начни собирать отчёт о проданных питомцах каждые 30 секунд, файл /tmp/sold_pets.json

[PLANNING] Plan: вызвать report_start_collection с interval_seconds=30, report_file=/tmp/sold_pets.json
> /yes

[EXECUTION] Calling tool: report_start_collection
Result: Collection started. Collecting every 30s → /tmp/sold_pets.json

...через несколько минут...

Task> Покажи собранный отчёт
[EXECUTION] Calling tool: report_show_sold
Result: Sold Pets Report — /tmp/sold_pets.json
  Interval: 30s | Snapshots collected: 4
  ...
```

---

## CLI-управление MCP

### Добавить сервер вручную

```bash
# SSE-сервер (HTTP — рекомендуется для Day19)
./ai-adv-agent-day19 --mcp-add \
    --mcp-name petstore \
    --mcp-type sse \
    --mcp-url http://localhost:8080/sse

# stdio-сервер (subprocess)
./ai-adv-agent-day19 --mcp-add \
    --mcp-name petstore \
    --mcp-type stdio \
    --mcp-command /path/to/petstore-mcp-server
```

### Просмотр и удаление

```bash
./ai-adv-agent-day19 --mcp-list
./ai-adv-agent-day19 --mcp-tools petstore
./ai-adv-agent-day19 --mcp-tools-all
./ai-adv-agent-day19 --mcp-remove petstore
```

---

## Slash-команды `/mcp-*` в интерактивном режиме

```
╔══════════════════════════════════════════╗
║   Interactive Agent  —  Day 18          ║
╠══════════════════════════════════════════╣
║  /yes  — approve / proceed              ║
║  /no   — reject without comment         ║
║  Enter — pause                          ║
║  text  — revision comment               ║
╠══════════════════════════════════════════╣
║  /exit     — quit the agent             ║
║  /restart  — discard task, start over   ║
║  /pause    — suspend at any prompt      ║
╠══════════════════════════════════════════╣
║  MCP (available everywhere):            ║
║  /mcp-list           — list servers     ║
║  /mcp-tools [name]   — list tools       ║
║  /mcp-add stdio/sse  — add server       ║
║  /mcp-remove <name>  — remove server    ║
╚══════════════════════════════════════════╝
```

| Команда | Описание |
|---|---|
| `/mcp-list` | Список всех настроенных серверов |
| `/mcp-tools` | Инструменты от всех серверов |
| `/mcp-tools <name>` | Инструменты от конкретного сервера |
| `/mcp-add stdio <name> <command> [args...]` | Добавить stdio-сервер |
| `/mcp-add sse <name> <url>` | Добавить SSE-сервер |
| `/mcp-remove <name>` | Удалить сервер |

---

## FSM (машина состояний, без изменений с Day15)

```
  ┌─────────┐  /yes    ┌───────────┐  /yes    ┌────────────┐  /yes  ┌──────┐
  │PLANNING │ ───────► │ EXECUTION │ ───────► │ VALIDATION │ ─────► │ DONE │
  └─────────┘          └───────────┘          └────────────┘        └──────┘
       ▲                                              │ violation (авто)
       └──────────────────────────────────────────────┘  или /no / текст
```

| Фаза | Пользователь |
|---|---|
| **PLANNING** | `/yes` — принять план; `/no` или текст — переплан; Enter — пауза |
| **EXECUTION** | `/yes` — к валидации; всё остальное — пауза |
| **VALIDATION** | `/yes` — принять; `/no` или текст — переплан; Enter — пауза |

---

## Флаги

```
# MCP
--mcp-config <path>       Конфиг-файл серверов (.yaml). Умолч.: <history>.mcp.yaml
--mcp-add                 Добавить сервер (используется с флагами ниже)
--mcp-name <name>         Имя сервера
--mcp-type stdio|sse      Тип транспорта
--mcp-command <cmd>       Исполняемый файл для stdio-сервера
--mcp-args '<a1 a2 ...>'  Аргументы для stdio-сервера (пробел-разделённые)
--mcp-url <url>           SSE endpoint (для sse)
--mcp-env KEY=VALUE       Дополнительная переменная окружения для stdio (повторяемый)
--mcp-remove <name>       Удалить сервер по имени
--mcp-list                Список настроенных серверов
--mcp-tools <name>        Список инструментов от сервера <name>
--mcp-tools-all           Список инструментов от всех серверов

# Режимы
--query           Запрос к LLM (обязателен в CLI-режиме)
--interactive     Запустить интерактивный режим с машиной состояний

# Общие
--system          Системное сообщение
--format          markdown | json (умолч. markdown)
--format-hint     Кастомная инструкция форматирования
--max-tokens      Лимит токенов ответа (0 = без лимита)
--stop            Stop-последовательность (повторяемый)
--temperature     Температура 0.0–2.0 (умолч.: от провайдера)
--debug           Вывод JSON-payload в stderr
--show-tokens     Вывод разбивки токенов в stderr
--show-cost       Вывод оценки стоимости (подразумевает --show-tokens)

# Инварианты
--invariants      Путь к файлу инвариантов (.md; умолч. <history>.invariants.md)

# Layer 1 — STM (история диалога)
--history         Путь к файлу истории (умолч. chat_history.json; "" = отключено)
--history-limit   Макс. сообщений при --summary (умолч. 10; 0 = без лимита)
--summary         Включить авто-суммаризацию при переполнении

# Layer 2 — Working Memory
--memory-wm       Путь к файлу рабочей памяти (умолч. <history>.wm.json)

# Layer 3 — Long-term Memory
--memory-ltm      Путь к файлу долгосрочной памяти (умолч. <history>.ltm.json)
--memory-update   Обновить WM и LTM после ответа (2 доп. LLM-вызова)

# Профиль пользователя
--profile         Путь к профилю (.md, умолч. <history>.profile.md)
--profile-init    Интерактивная инициализация профиля и выход
--profile-name    Установить имя пользователя и выйти
--profile-set     Установить предпочтение key=value (повторяемый), затем выйти
--profile-delete  Удалить предпочтение по ключу и выйти
--profile-list    Вывести текущий профиль и выйти

# Стратегии контекста
--strategy        sliding-window | sticky-facts | branching
--window-size     Размер окна (умолч. 5)

# Ветки (только --strategy branching)
--checkpoint      Сохранить checkpoint текущей ветки
--branch-new      Создать и переключиться на новую ветку
--from-checkpoint Источник для --branch-new
--branch-switch   Переключиться на существующую ветку
--branch-list     Показать все ветки и checkpoints
```

---

## Структура файлов на диске

| Файл | Назначение | Когда создаётся |
|---|---|---|
| `chat_history.json` | STM — диалог (Layer 1) | Всегда (если `--history` не пустой) |
| `chat_history.mcp.yaml` | MCP-конфиг серверов | При `--mcp-add` или `/mcp-add` |
| `chat_history.wm.json` | WM — рабочая память (Layer 2) | При `--memory-update` |
| `chat_history.ltm.json` | LTM — долгосрочная память (Layer 3) | При `--memory-update` |
| `chat_history.profile.md` | Профиль пользователя | При `--profile-*` |
| `chat_history.invariants.md` | Инварианты | Создаётся вручную |
| `chat_history.task.json` | Состояние задачи FSM | В интерактивном режиме; удаляется по завершении |
| `chat_history.stats.json` | Накопленная статистика токенов | При `--show-tokens` / `--show-cost` |
| `chat_history.summary.txt` | Суммаризация истории | При `--summary` + переполнении |
| `chat_history.facts.json` | KV-факты sticky-facts | При `--strategy sticky-facts` |
| `chat_history.branch-state.json` | Состояние веток | При `--strategy branching` |
| `/tmp/sold_pets.json` (пример) | Отчёт сборщика | При вызове `report_start_collection` |

---

## Сборка и тесты

```bash
cd Day22/agent

go build -o ai-adv-agent-day22 .   # или: make build
go test -v ./...                    # или: make test
go vet ./...                        # или: make vet

# Интеграционные тесты RAG (тег integration; нужен эндпоинт эмбеддингов)
go test -tags integration -run TestRAGEval -v .
```

---

## Makefile targets (из mcp-server/)

```bash
# Работа с агентом
make run-agent          # Свежая сессия: авто-старт MCP (SSE) + агент
make run-agent-persist  # Постоянная сессия: история не очищается

# GigaChat
make run-gigachat-agent   # Как run-agent с GigaChat
make run-gigachat-persist # Как run-agent-persist с GigaChat
make run-gigachat-tools   # Интеграционный тест tool calling через GigaChat

# Только MCP-сервер
make run-http             # Запуск HTTP+SSE сервера (без агента)
make run-http-smoke       # Smoke-тест HTTP+SSE

# RAG (Day22)
make run-rag              # Ответ на RAG_QUESTION, заземлённый на RAG_DB
make run-rag-eval         # 10 контрольных вопросов: проверка извлечения
make run-rag-compare      # Сравнение «с RAG» vs «без RAG» (нужен RUN_LLM=1)
```

---

## Переменные окружения

| Переменная | Обязательна | Умолч. |
|---|---|---|
| `LLM_PROVIDER` | Да | — (`openai` или `openrouter`) |
| `LLM_API_KEY` | Да | — |
| `LLM_MODEL` | Нет | `openai/gpt-4o-mini` |
| `LLM_BASE_URL` | Нет | Стандартный URL провайдера |

MCP-команды (`--mcp-list`, `--mcp-add`, `--mcp-tools`) не требуют `LLM_PROVIDER` и `LLM_API_KEY`.

Для RAG эмбеддинги настраиваются флагами `--rag-embed-*` (или переменными `EMBED_URL` / `EMBED_MODEL` в Makefile-целях). LLM-шаг использует те же `LLM_*`, что и обычный запрос, — эмбеддинги и генерация могут жить на разных эндпоинтах (например, локальные эмбеддинги + LLM через OpenRouter).
