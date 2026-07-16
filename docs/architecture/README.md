# Архитектура проекта (по коду Day31)

Документ описывает архитектуру агента на срезе **Day31** — конфиг-ориентированного оркестратора саб-агентов. Источник — код `Day31/agent`. Практика использования — [docs/usage](../usage/README.md).

## Обзор

Агент — один Go-модуль (`ai-adv-agent`), построенный по **Clean Architecture**. Точка входа (`main.go`) при наличии `--config` уходит в **режим оркестратора**: главный процесс не решает задачу сам, а планирует через LLM и делегирует работу саб-агентам, маршрутизируя их результаты. Без `--config` работают легаси-пути одиночного агента (Day2–Day30), не тронутые рефактором.

День — трёхкомпонентный workspace, каждый компонент — свой Go-модуль:

| Компонент | Роль |
|---|---|
| `agent/` | Оркестратор, саб-агенты, LLM-клиент, RAG-адаптер, MCP-пул, TUI |
| `mcp-server/` | Petstore MCP-сервер (stdio + HTTP/SSE), 22 инструмента |
| `rag/` | Индексатор корпуса (`index` / `search` / `stats`) для RAG |

## Слои (Clean Architecture)

Зависимости направлены **внутрь**: `domain` ← `port` ← `usecase` ← `app` → `adapter`. Ключевое правило: `usecase` не импортирует адаптеры — сборка конкретных реализаций вынесена в композиционный корень `internal/app`.

```
internal/
  domain/    — сущности: Message, Usage, RetrievedChunk, RAGAnswer, TaskMemory,
               WorkingMemory, LongTermMemory, MCPTool, MCPServerConfig, pricing …
  port/      — контракты-интерфейсы: LLMClient, Retriever, Reranker, ToolExecutor,
               MCPPool, HistoryRepository, WorkingMemoryRepository,
               LongTermMemoryRepository, TaskMemoryRepository, UserProfileRepository …
  usecase/   — ChatUseCase (+ ExecuteWithTools: MCP tool-loop), RAGUseCase
               (retrieve→rerank→filter→grounded), память (UpdateWM/UpdateLTM,
               TaskMemory…), SlidingWindow, промпт-билдеры. Без импорта адаптеров.
  adapter/
    llm/     — OpenAI-совместимый HTTP-клиент (ca_cert, gen-defaults, GigaChat)
    rag/     — эмбеддер + read-only SQLite-поиск + 2 реранкера (chat / native /rerank)
    mcp/     — MCP-клиент и пул подключений (stdio + HTTP/SSE)
    storage/ — файловые + in-memory репозитории (mem.go), ReadOnlyLTM
  config/    — YAML-документ и контракт слияния (Load / Default / ResolveEnv /
               Overrides / Validate)
  app/       — КОМПОЗИЦИОННЫЙ КОРЕНЬ: Toolbelt, MemoryFactory, Orchestrator, SubAgent
```

Верхний уровень `agent/` (пакет `main`): `main.go` (флаги + легаси-пути), `main_config.go` (режим оркестратора: `runOrchestrator` / REPL), `tui.go` + `tui_render.go` (TUI), `commands.go` (slash-команды), `clipboard.go`.

## Поток управления (режим оркестратора)

```
main.go --config
   │  config.Load(path) → merge на config.Default()
   │  config.ResolveEnv(&cfg)      // пустые LLM-поля из LLM_*
   │  buildOverrides(flags).Apply  // побеждают только реально введённые флаги (flag.Visit)
   │  config.Validate(cfg)
   ▼
app.Build(cfg) → *Toolbelt         // LLM + RAG(опц.) + MCP-пул(опц.) + MemoryFactory
   ▼
app.NewOrchestrator(tb) → строит роестр SubAgent из cfg.Orchestrator.SubAgents
   ▼
Orchestrator.Handle(ctx, task):
   transcript = priorDialogue() + {user: task}     // подгрузка STM-истории
   loop round < max_rounds:
     resp = LLM.Chat(system=systemPrompt(), transcript)   // system = memory-блоки + ростер + JSON-протокол
     act  = parseAction(resp)                              // {action: spawn|ask_user|finish}
       spawn    → SubAgent.Run(task, context) → результат обратно в transcript
       ask_user → UserPrompter.AskUser(plan) → ответ в transcript (или автономно без prompter)
       finish   → answer, выход
   persistTurn(task, answer)                               // сохранить историю (+ опц. UpdateWM/UpdateLTM)
```

### Контракт оркестратора (JSON-протокол)
Оркестратор-LLM каждый раунд выдаёт строго один JSON-объект:
- `{"action":"spawn","agent":"<имя>","task":"…","context":"…"}` — запустить саб-агента (context несёт результаты прошлых агентов — это маршрутизация).
- `{"action":"ask_user","question":"…","plan":"…"}` — human-in-the-loop согласование.
- `{"action":"finish","answer":"…"}` — итог.

Проза вместо JSON трактуется как финальный ответ (не роняет run). Неизвестный экшен — модель возвращается к протоколу подсказкой.

## Toolbelt — общий набор возможностей

`app.Toolbelt` (`toolbelt.go`) — единый bundle, который получают и оркестратор, и **каждый** саб-агент (одинаковая досягаемость, гейтится только ролью):

- `LLM port.LLMClient` — общий HTTP-клиент.
- `RAG *usecase.RAGUseCase` + `RAGCfg` — nil, если `rag.enabled=false`.
- `MCPPool` + `MCPTools` + `MCPServers` + **`ToolRouting`** (tool-имя → сервер).
- `Memory *MemoryFactory`.

Гейтинг доступа к MCP по роли:
- `MCPToolsFor(allow)` — инструменты только разрешённых серверов (`["*"]` — все, `[]` — ни одного).
- `ToolExecutor(allow)` — диспатчит вызов в нужную MCP-сессию, отказывает на инструментах вне allow-list (`serverAllowed`).

`Build` собирает Toolbelt из валидированного конфига и регистрирует `closers` (RAG-БД, MCP-сессии); `Close` освобождает их в обратном порядке.

## Память — shared vs ephemeral

`app.MemoryFactory` (`memory.go`) резолвит путь каждого слоя один раз из `memory.dir` и уважает per-layer toggles (выключенный слой → `""`-path no-op репозиторий: читает пусто, не пишет).

| Флейвор | Кому | Состав |
|---|---|---|
| **Shared** (`SharedChat`) | Оркестратор (свой диалог) | Файловые STM/WM/LTM/facts/profile/task-memory — персистят между запусками |
| **Ephemeral** (`EphemeralChat`) | Саб-агент (скретч) | In-memory STM (засеян контекстом) + in-memory WM; **read-only** shared LTM (`ReadOnlyLTM`) + shared профиль |

Так саб-агент **никогда не затирает** сессионную LTM/историю оркестратора. Слои памяти оркестратора инжектятся в его планирующий промпт (`systemPrompt`): профиль → LTM → WM → память задачи; плюс `priorDialogue()` подкладывает окно STM-истории (это чинит «забывание» прошлых задач сеанса).

Слои (пути выводятся из `memory.dir`, напр. `chat_history.json`):

| Слой | Файл | Назначение |
|---|---|---|
| STM | `chat_history.json` | История диалога |
| WM | `*.wm.json` | Факты текущей задачи |
| LTM | `*.ltm.json` | Профиль, стабильные предпочтения |
| Facts | `*.facts.json` | KV-факты |
| Профиль | `*.profile.md` | Профиль пользователя |
| Память задачи | `*.taskmem.json` | Цель / уточнено / ограничения |

## SubAgent — воркер

`app.SubAgent` (`subagent.go`) переиспользует тот же движок, что и остальной агент:
1. Берёт `EphemeralChat` (эфемерная память по роли).
2. Собирает вход: `Контекст от оркестратора:\n… \n\nЗадача:\n…` (роль-промпт — системное сообщение).
3. Если `role.RAG` и RAG включён — `RAGUseCase.BuildPrompt` (retrieve → [rerank] → filter) заземляет вход; при `RerankErr` деградирует на similarity-порядок.
4. Инструменты и executor — по allow-list роли (`MCPToolsFor` / `ToolExecutor`).
5. `ChatUseCase.ExecuteWithTools` — MCP tool-loop; возвращает текст, который оркестратор маршрутизирует дальше.

## Config — декларативный контракт

`internal/config` (`config.go` / `load.go` / `override.go`):
- `Default()` — встроенные дефолты (все слои памяти включены; RAG/MCP выключены).
- `Load(path)` — мержит YAML **поверх** дефолтов (пропущенный `enabled:` оставляет слой включённым).
- `ResolveEnv(&cfg)` — заполняет пустые LLM-поля из `LLM_PROVIDER`/`LLM_MODEL`/`LLM_API_KEY`/`LLM_BASE_URL`/`LLM_CA_CERT`.
- `Overrides` — pointer-per-field, собран из `flag.Visit`; `Apply` даёт победить **только** реально введённым флагам (не затирая конфиг zero-дефолтами).
- `Validate(cfg)` — контракт валидности (провайдер/модель, согласованность RAG/MCP, ростер).

Приоритет: **defaults → YAML → явные флаги**.

## TUI

`tui.go` (gotui v5) — полноэкранный интерфейс: шапка + прокручиваемый лог (`widgets.List`) + многострочный ввод (`widgets.TextArea`). Задача выполняется в фоновой горутине, прогресс оркестратора/саб-агентов стримится в лог через инжектируемый `io.Writer` (`Orchestrator.SetOutput` → канал). Ключи: **Enter — новая строка, Ctrl+D — отправка** (модификаторы на Enter в gotui/tcell не детектятся); ↑/↓ — курсор/история ввода; PgUp/PgDn/Home/End — прокрутка. **Выделение мышью/тачпадом — по умолчанию** (при старте `DisableMouse`); `/select` (или F2) переключает в режим прокрутки колесом. `tui_render.go` — цветной markdown-lite рендер с экранированием разметки (`escMarkup` от `ui.ParseStyles`) и verbatim-фенсами. Fallback на строчный REPL при `--no-tui` или не-TTY.

## Расширяемость

- **Новый саб-агент** — добавить запись в `orchestrator.subagents` (имя, роль-промпт, `rag`, `mcp`-allow-list, `memory.wm`). Код не трогается.
- **Новый MCP-сервер** — `mcp.servers` инлайн или `mcp.file:`; раздать роли через её `mcp`-список.
- **Новый провайдер LLM** — реализация `port.LLMClient` + ветка в `resolve.go`/`buildLLMClient`.
- **Другой store RAG** — реализация `port.Retriever` в `adapter/rag`.

## Ключевые файлы

| Файл | Содержимое |
|---|---|
| `agent/main.go` / `main_config.go` | Флаги, легаси-пути / режим оркестратора |
| `internal/app/orchestrator.go` | Цикл планирования, JSON-протокол, persist/prior-dialogue |
| `internal/app/subagent.go` | Воркер: RAG-груундинг + MCP tool-loop |
| `internal/app/toolbelt.go` | Общие возможности + гейтинг MCP по роли |
| `internal/app/memory.go` | shared vs ephemeral память |
| `internal/config/*.go` | Декларативный конфиг и слияние |
| `agent/tui.go` / `tui_render.go` | TUI и рендеринг |
