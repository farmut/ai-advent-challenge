# Day 26: диалоговый RAG-агент с памятью диалога и памятью задачи

Расширение Day24. Главное нововведение Day25 — интерактивный RAG-сеанс (`--rag --interactive`) становится **диалоговым и с памятью**:

1. **История диалога сохраняется** — реплики «вопрос → ответ» пишутся в файл `--history` и подгружаются при следующем запуске (сеанс возобновляется). Последнее окно сообщений подкладывается в промпт, чтобы модель понимала уточняющие вопросы.
2. **На каждый новый вопрос — свежий поиск через RAG** — запрос «якорится» на цель диалога, поэтому даже короткий уточняющий вопрос («а по умолчанию?») находит релевантный контекст.
3. **Память задачи** — ассистент ведёт и сохраняет три вещи: **цель диалога**, **что пользователь уже уточнил** и **какие ограничения/термины зафиксированы**. Память обновляется LLM после каждого хода, инжектится в каждый ответ (ассистент не переспрашивает уже уточнённое и держится ограничений) и хранится в `<history>.taskmem.json`. Команда `/memory` печатает её.

```
вопрос → [+ цель диалога] поиск (top-K) → rerank → фильтр по порогу → top-K финальных
       → grounded LLM-вызов (+ память задачи + окно истории)
       → {ответ, источники, цитаты}
       └─ (ничего не прошло порог) → «не знаю, уточните вопрос»
       ↳ сохранить историю + обновить память задачи (цель / уточнено / ограничения)
```

Обоснованный ответ (Day24) не изменился: модель по-прежнему возвращает **ответ + источники** (файл + раздел/`chunk_id`) **+ цитаты** (дословные фрагменты), а при релевантности ниже порога честно говорит **«не знаю»** без вызова LLM.

Агент по-прежнему умеет всё из предыдущих дней (MCP-инструменты, 3-слойная память, профиль, инварианты, интерактивный FSM). Day22 добавил флаг `--rag`; Day23 — этап реранкинга; Day24 — структурированный ответ с цитированием и RAG-REPL; Day25 делает RAG-REPL диалоговым (память диалога + память задачи).

## Что нового (Day2 → Day25)

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
| RAG в агенте: `--rag`, поиск→объединение→LLM | Day22 |
| Интеграционные тесты: 10 контрольных вопросов, RAG vs no-RAG | Day22 |
| Reranker: этап `--rag-rerank` c выделенной моделью, порог, top-K до/после | Day23 |
| Интеграционный тест: 10 контрольных вопросов, RAG с reranker vs без | Day23 |
| **Структурированный ответ: ответ + источники (файл + раздел/`chunk_id`) + цитаты** | **Day24** |
| **Guard низкой релевантности: ниже порога → «не знаю» + просьба уточнить** | **Day24** |
| **Интерактивный RAG-сеанс (`--rag --interactive`)** | **Day24** |
| **Интеграционный тест: 10 вопросов — источники + цитаты + смысл (LLM-судья)** | **Day24** |
| **Память диалога в RAG-сеансе: история сохраняется и подгружается (`--history`)** | **Day25** |
| **Свежий RAG-поиск на каждый вопрос, «заякоренный» на цель диалога** | **Day25** |
| **Память задачи: цель + что уточнено + ограничения/термины (`<history>.taskmem.json`, `/memory`)** | **Day25** |

---

## RAG (retrieval-augmented generation)

CLI-запрос «заземляется» (grounded) на базе знаний, а на выходе получается **структурированный ответ с цитированием** (в интерактивном RAG-сеансе Day25 — ещё и с памятью диалога и памятью задачи, см. ниже):

1. **вопрос** — текст из `--query` (или строка, введённая в интерактивном сеансе);
2. **поиск (retrieve)** — вопрос эмбеддится и по косинусной близости ищется пул из top-K чанков (`--rag-top-k` — размер пула **до** фильтрации) в SQLite-индексе (`rag.db`), построенном компонентом `rag/`;
3. **rerank** *(опционально, `--rag-rerank`)* — извлечённые чанки переоцениваются **отдельной моделью-реранкером**: она смотрит на вопрос и каждый чанк совместно и выставляет релевантность 0..1. Реранкер может жить на своей модели/провайдере/эндпоинте, отдельно от отвечающей LLM;
4. **фильтр** — чанки со скором ниже `--rag-threshold` отбрасываются (сравнивается rerank-скор, если реранк был, иначе similarity), а выжившие обрезаются до `--rag-top-k-final`;
5. **grounded LLM-вызов** — финальные фрагменты (каждый с маркером `[n]`, файлом, разделом и `chunk_id`) + вопрос собираются в промпт, который просит модель вернуть **JSON**: `{answer, sources, quotes}`. Ответ строится только по контексту, каждое утверждение опирается на цитату;
6. **структурированный ответ** — маркеры `[n]` разворачиваются обратно в конкретные чанки:
   - **ответ** — развёрнутый текст;
   - **источники** — `источник + раздел + chunk_id + релевантность`;
   - **цитаты** — дословные фрагменты найденных чанков под каждое утверждение;
7. **guard низкой релевантности** — если после фильтра **не осталось ни одного чанка**, LLM **не вызывается**: возвращается честное «не знаю, уточните вопрос» (`Grounded=false`, без источников/цитат).

Парсер терпим к обрезанному JSON (reasoning-модели съедают бюджет токенов): `salvageRAGAnswerJSON` восстанавливает ответ и все целые цитаты до обрыва, а при полном провале откатывается на цитирование всех найденных чанков — источники и цитаты присутствуют всегда.

### Диалоговый RAG-сеанс с памятью (Day25)

`--rag --interactive` запускает диалоговый REPL: между ходами он **сохраняет историю диалога**, **делает свежий RAG-поиск на каждый вопрос** и ведёт **память задачи**.

- **История диалога** — `runRAGInteractive` грузит историю из `--history` в начале сеанса и дозаписывает пару «вопрос → ответ» после каждого хода. В промпт к модели подкладывается скользящее окно последних сообщений (8), чтобы модель понимала уточняющие вопросы, не раздувая контекст.
- **Свежий поиск** — каждый вопрос проходит полный конвейер (retrieve → [rerank] → filter). Поисковый запрос «якорится» на `TaskMemory.Goal`, поэтому короткий уточняющий вопрос всё равно извлекает контекст по теме диалога.
- **Память задачи** (`domain.TaskMemory`) хранит ровно три вещи:
  - **Цель диалога** (`Goal`) — что в итоге хочет пользователь;
  - **Что уже уточнено** (`Clarified`) — подтверждённые пользователем факты;
  - **Ограничения и термины** (`Constraints`) — зафиксированные определения/условия.

  После каждого хода `UpdateTaskMemory` просит LLM обновить память (сохраняя прежние пункты и без дублей), она пишется в `<history>.taskmem.json` и как системная директива инжектится в каждый ответ — ассистент не переспрашивает уже уточнённое и держится ограничений. Обновление устойчиво: при пустом ответе reasoning-модели или ошибке прежняя память не затирается, диалог продолжается.

Команды REPL: `/exit` или `/quit` — выход, `Ctrl-D` (EOF) — тоже, `/memory` — показать текущую память задачи.

### Архитектура (Clean Architecture)

| Слой | Компонент |
|---|---|
| domain | `RetrievedChunk` (+ `Similarity` / `RerankScore`, `Score(reranked)`); `AnswerSource` (`Source`/`Section`/`ChunkID`/`Score`), `AnswerQuote` (+ `Text`), `RAGAnswer` (`Answer`/`Sources`/`Quotes`/`Grounded`); **`TaskMemory`** (`Goal`/`Clarified`/`Constraints`, `IsEmpty()`) — память задачи диалога |
| port | `Retriever` — шаг поиска; `Reranker` — шаг переоценки; **`TaskMemoryRepository`** — хранилище памяти задачи |
| adapter | `internal/adapter/rag/` — HTTP-эмбеддер + read-only поиск по SQLite (`modernc.org/sqlite`); два реранкера: `LLMReranker` (chat-модель как cross-encoder) и `APIReranker` (выделенный `/rerank`-эндпоинт: Cohere/Jina-стиль); **`storage.TaskMemoryFile`** (+ `TaskMemoryPath`) — JSON-файл памяти задачи |
| usecase | `RAGUseCase` + `RAGConfig` (`TopKRetrieve`/`Rerank`/`Threshold`/`TopKFinal`); `Answer()` — полный конвейер до структурированного ответа; **`AnswerWithContext()`** — то же + окно истории и память задачи (`ConversationContext`); `BuildAnswerPrompt` (JSON-контракт), `parseRAGAnswer`/`salvageRAGAnswerJSON`; **`UpdateTaskMemory`/`TaskMemorySystemBlock`** — обновление и рендер памяти задачи; `BuildRAGPrompt`/`BuildPrompt` сохранены |

Агент — отдельный Go-модуль, а пакеты компонента `rag/` лежат в его `internal/`, поэтому ретривер реализован внутри агента (переиспользовать через импорт нельзя из-за правила Go про internal-пакеты).

**Два транспорта реранкера.** Выделенные rerank-модели (`cohere/rerank-*`, `rerank-v3.5`, `jina-reranker-*`) — это кросс-энкодеры на отдельном `POST /rerank`-эндпоинте, а не chat-модели: гнать их через `/chat/completions` нельзя (ответ 400). Флаг `--rag-rerank-mode` выбирает транспорт: `api` (нативный `/rerank`), `chat` (скоринг через chat-модель) или `auto` — по умолчанию: `api`, если имя модели содержит «rerank», иначе `chat`.

### Быстрый старт

```bash
cd Day25/agent

# LLM — как обычно (пример: OpenRouter). Эмбеддинги — локально.
export LLM_PROVIDER=openrouter
export LLM_API_KEY=your-api-key
export LLM_MODEL=deepseek/deepseek-v4-flash

# Один вопрос: структурированный ответ + источники + цитаты (с reranker)
make run-rag RAG_RERANK=1 RAG_RERANK_MODEL=cohere/rerank-4-fast \
    RAG_TOP_K=20 RAG_THRESHOLD=0.5 RAG_TOP_K_FINAL=10 \
    RAG_QUESTION="Какие бывают этапы выполнения очистки таблиц?"

# Диалоговый RAG-сеанс (Day25): память диалога + память задачи.
# История и память задачи пишутся в RAG_HISTORY_FILE (умолч. /tmp/rag_session_agent.json)
# и <history>.taskmem.json — сеанс возобновляется. /memory — показать память задачи, /exit — выход.
# (по умолчанию реранкинг cohere/rerank-4-fast, 20 → порог 0.5 → 10.)
make run-rag-interactive

# или напрямую (с сохранением истории и памяти задачи):
./ai-adv-agent-agent \
    --interactive --rag --rag-db ../rag/rag.db \
    --rag-top-k 20 --rag-rerank --rag-rerank-model cohere/rerank-4-fast \
    --rag-threshold 0.5 --rag-top-k-final 10 \
    --rag-embed-url http://127.0.0.1:1234 \
    --rag-embed-model text-embedding-nomic-embed-text-v2-moe \
    --history /tmp/rag_session.json
```

Пример вывода одного вопроса:

```
<развёрнутый ответ по контексту>

=== Источники ===
  [1] postgresql_internals-15.pdf — Очистка (VACUUM) (chunk_id=120, релевантность 0.881)
  [2] postgresql_internals-15.pdf — Очистка (VACUUM) (chunk_id=122, релевантность 0.834)

=== Цитаты ===
  [1] postgresql_internals-15.pdf (chunk_id=120):
      «Основная, обычная очистка выполняется командой VACUUM. Она обрабатывает таблицу полностью…»
```

Интерактивный сеанс (`--rag --interactive`, Day25): каждый вопрос отвечается тем же grounded-конвейером, но с учётом истории диалога и памяти задачи; история и память задачи сохраняются (`--history` + `<history>.taskmem.json`). `/memory` показывает память задачи, `/exit` / `/quit` завершают, `Ctrl-D` (EOF) — тоже.

> **Важно:** запрос нужно эмбеддить **той же моделью**, которой построен индекс. `../rag/rag.db` построен `text-embedding-nomic-embed-text-v2-moe` (768 dims). Другая модель (даже той же размерности) даёт бессмысленную близость (~0.06 вместо ~0.5).

### RAG-флаги

```
--rag                    Включить RAG: поиск релевантных чанков перед запросом к LLM
--rag-db <path>          SQLite-индекс, построенный компонентом rag/ (умолч. rag.db)
--rag-top-k <n>          Размер пула извлечения ДО фильтрации (умолч. 10)
--rag-embed-url <url>    База URL эмбеддингов (умолч. http://localhost:11434)
--rag-embed-model <m>    Модель эмбеддингов (должна совпадать с индексом)
--rag-embed-key <k>      API-ключ эмбеддингов (пусто для локальных рантаймов)

  Этап reranker (второй этап, опционально):
--rag-rerank             Включить переоценку извлечённых чанков реранкером
--rag-rerank-model <m>   Модель реранкера (умолч. — основная LLM_MODEL)
--rag-rerank-mode <m>    Транспорт: api | chat | auto (умолч. auto)
--rag-rerank-provider    Провайдер реранкера (умолч. — основной LLM_PROVIDER)
--rag-rerank-url <url>   Base URL эндпоинта реранкера (умолч. — URL основного провайдера)
--rag-rerank-key <k>     API-ключ реранкера (умолч. — основной LLM_API_KEY)

  Фильтрация:
--rag-threshold <f>      Отсекать чанки со скором ниже порога 0..1 (0 = выкл.)
--rag-top-k-final <n>    Сколько чанков оставить ПОСЛЕ реранка/фильтра (умолч. 5; 0 = все прошедшие порог)
```

Каждый `--rag-rerank-*` при отсутствии откатывается на основной конфиг LLM — достаточно указать один `--rag-rerank-model`, чтобы гонять реранк на другой модели того же провайдера.

### Контрольные вопросы и интеграционные тесты

`eval_questions.json` — мини-набор из 10 контрольных вопросов по базе «PostgreSQL 15 изнутри». Для каждого задано ожидание (что должно быть в ответе) и источники, которые должны быть использованы (файл + раздел + anchor-слова для проверки извлечения).

```bash
# Проверка извлечения: для каждого вопроса нужный источник должен попасть в top-K
make run-rag-eval

# Day24: 10 вопросов → обоснованный ответ; проверяет, что в КАЖДОМ ответе есть
# источники и цитаты, и что смысл ответа совпадает с цитатами (LLM-судья).
# Всегда с реранкингом (cohere/rerank-4-fast, 20 → порог 0.5 → 10).
make run-rag-grounded

# Сравнение «с RAG» vs «без RAG» с итоговым отчётом
RUN_LLM=1 make run-rag-compare

# Сравнение «с reranker» vs «без reranker» с итоговым отчётом (Day23)
make run-rag-rerank-compare RAG_RERANK_MODEL=cohere/rerank-4-fast \
    RAG_TOP_K=20 RAG_THRESHOLD=0.5 RAG_TOP_K_FINAL=10
```

| Тест (тег `integration`) | Что делает | Отчёт |
|---|---|---|
| `TestRAGEval` | 10 вопросов → проверяет, что ожидаемые источники извлечены | `rag_eval_result.txt` |
| **`TestRAGGroundedAnswers`** | **10 вопросов → обоснованный ответ; в каждом ответе есть источники (10/10) и цитаты (10/10), а смысл совпадает с цитатами (LLM-судья `SUPPORTED/UNSUPPORTED`, порог ≥60%); + спот-проверка guard'а «не знаю» на off-topic вопросе. Всегда реранкит.** | `rag_grounded_result.txt` |
| `TestRAGCompare` | каждый вопрос дважды (с RAG и без) → сравнение полноты и цитирования | `rag_compare_result.txt` |
| `TestRAGRerankCompare` | каждый вопрос дважды (с reranker и без) на одном пуле/пороге → сравнение точности контекста, полноты и цитирования | `rag_rerank_compare_result.txt` |

Тесты `t.Skip`, если индекс или эндпоинт эмбеддингов недоступны; `TestRAGGroundedAnswers`, `TestRAGCompare` и `TestRAGRerankCompare` требуют LLM-креды (реранк-модель задаётся `RAG_RERANK_MODEL`, по умолчанию `cohere/rerank-4-fast` для grounded-теста).

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
--query           Запрос к LLM (обязателен в CLI-режиме; не нужен для --rag --interactive)
--interactive     Интерактивный режим. Без --rag — FSM-агент (машина состояний);
                  с --rag — диалоговый RAG-REPL (Day25): вопрос → обоснованный ответ
                  (источники + цитаты) с памятью диалога (--history) и памятью задачи
                  (<history>.taskmem.json); /memory показывает память задачи

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
| `chat_history.json` | STM — диалог (Layer 1); в RAG-сеансе — история диалога | Всегда (если `--history` не пустой) |
| `chat_history.taskmem.json` | Память задачи RAG-диалога: цель / уточнено / ограничения (Day25) | В `--rag --interactive` |
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
cd Day25/agent

go build -o ai-adv-agent-agent .    # или: make build
go test -v ./...                    # или: make test
go vet ./...                        # или: make vet

# Интеграционные тесты RAG (тег integration; нужен эндпоинт эмбеддингов)
go test -tags integration -run TestRAGEval -v .
go test -tags integration -run TestRAGGroundedAnswers -v .  # источники+цитаты+смысл (нужны LLM-креды)
go test -tags integration -run TestRAGRerankCompare -v .    # реранк-сравнение (нужен RUN_LLM=1)
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

# RAG (Day22–25)
make run-rag                 # Один вопрос → структурированный ответ (RAG_RERANK=1 — с реранком)
make run-rag-interactive     # Диалоговый RAG-сеанс: ответ + источники + цитаты, с памятью диалога и памятью задачи (Day25; RAG_HISTORY_FILE=)
make run-rag-eval            # 10 контрольных вопросов: проверка извлечения
make run-rag-grounded        # 10 вопросов: источники + цитаты + смысл (LLM-судья), всегда реранк (Day24)
make run-rag-compare         # Сравнение «с RAG» vs «без RAG» (нужен RUN_LLM=1)
make run-rag-rerank-compare  # Сравнение «с reranker» vs «без reranker» (нужен RUN_LLM=1)
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

Для RAG эмбеддинги настраиваются флагами `--rag-embed-*` (или переменными `EMBED_URL` / `EMBED_MODEL` в Makefile-целях). LLM-шаг использует те же `LLM_*`, что и обычный запрос, — эмбеддинги и генерация могут жить на разных эндпоинтах (например, локальные эмбеддинги + LLM через OpenRouter). Реранкер (`--rag-rerank-*`) — третий независимый эндпоинт: своя модель/провайдер/ключ, по умолчанию наследует конфиг основной LLM.
