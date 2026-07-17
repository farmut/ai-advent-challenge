# Day 33: конфиг-ориентированный оркестратор саб-агентов

Day31 — рефакторинг разрозненных флагов одиночного агента в **единый оркестратор, управляемый одним YAML-конфигом** (`agent.config.yaml`). Главный процесс больше не решает задачу сам: он ведёт LLM-цикл планирования, **спаунит саб-агентов** из ростера и **маршрутизирует их результаты** друг между другом, пока не сможет ответить.

```
пользователь → [оркестратор: цикл планирования, ≤ max_rounds]
                 │  каждый раунд — один JSON-экшен:
                 │   {"action":"spawn","agent","task","context"}   запустить саб-агента
                 │   {"action":"ask_user","question","plan"}       согласовать план (human-in-the-loop)
                 │   {"action":"finish","answer"}                  выдать итоговый ответ
                 ▼
             саб-агенты (researcher / coder / reviewer …)
                 │  свой промпт-роль, свой доступ (RAG / MCP), своя эфемерная память
                 ▼
             результаты возвращаются в транскрипт оркестратора → следующий раунд
```

**Легаси-пути одиночного агента не тронуты**: без `--config` бинарь работает как в Day30 (RAG с реранкингом и цитированием, MCP-инструменты, 3-слойная память, профиль, инварианты, интерактивный FSM). `--config` (или env `AGENT_CONFIG`) включает режим оркестратора.

## Быстрый старт

```bash
cd Day31/agent
make build                 # бинарь ai-adv-agent-agent

# секреты — в окружении, не в YAML
export LLM_PROVIDER=openrouter
export LLM_API_KEY=sk-...
export LLM_MODEL=openai/gpt-4o-mini

# один запрос
make run-orchestrator AGENT_TASK="Проанализируй репозиторий и предложи 3 улучшения"

# интерактивный TUI-сеанс
make run-orchestrator-interactive
```

Прямой вызов:

```bash
./ai-adv-agent-agent --config agent.config.yaml --query "…"
./ai-adv-agent-agent --config agent.config.yaml --interactive
```

## Конфиг `agent.config.yaml`

Один файл включает и выключает возможности. **Каждый слой памяти включён по умолчанию** — слой указывают только чтобы выключить или настроить. RAG и MCP — opt-in.

Приоритет (низкий → высокий): **встроенные дефолты → YAML → явные CLI-флаги**. `config.Load` мержит файл поверх `config.Default()` (пропущенный `enabled:` оставляет слой включённым), `ResolveEnv` заполняет пустые LLM-поля из `LLM_*`, а `config.Overrides` (собран из `flag.Visit`, указатель-на-поле) даёт победить только тем флагам, которые пользователь реально ввёл.

```yaml
llm:
  provider: ""            # env LLM_PROVIDER (openai | openrouter | gigachat)
  model: ""               # env LLM_MODEL
  base_url: ""            # env LLM_BASE_URL (дефолт провайдера, если пусто)
  temperature: null       # null = дефолт провайдера
  max_tokens: 0           # 0 = без лимита
  context_window: 0       # 0 = без клиентской обрезки промпта
  ca_cert: ""             # PEM для self-signed HTTPS (приватный LiteLLM, Day30)

memory:
  dir: chat_history.json  # база; путь каждого слоя выводится из неё
  auto_update: false      # обновлять WM+LTM доп. LLM-вызовом после хода
  stm:  { enabled: true, limit: 10, summary: false }
  wm:   { enabled: true }
  ltm:  { enabled: true }
  facts: { enabled: true }
  profile: { enabled: true }
  task_memory: { enabled: true }
  # выключить слой:  ltm: { enabled: false }

rag:
  enabled: false          # true + db/embed_* → включить RAG
  db: rag.db
  embed_url: http://localhost:11434
  embed_model: nomic-embed-text
  top_k: 20               # размер пула ДО фильтрации
  threshold: 0.5          # отсечка по релевантности
  top_k_final: 10         # оставить ПОСЛЕ фильтра
  rerank: { enabled: false, model: "", mode: auto }  # mode: api | chat | auto

mcp:
  enabled: false
  servers: []             # инлайн-серверы или file: путь к MCP-YAML
  #   - { name: petstore, type: sse, url: "http://127.0.0.1:8931/sse" }

invariants:
  enabled: false
  path: ""                # пусто → выводится из memory.dir

orchestrator:
  enabled: true
  max_rounds: 8
  subagents:
    - name: researcher     # аналитик: собирает факты и контекст
      prompt: "Ты аналитик-исследователь. Собираешь факты…"
      rag: true            # доступ к RAG (требует rag.enabled)
      mcp: []              # напр. ["petstore"] или ["*"] — все серверы
      memory: { wm: true }
    - name: coder          # инженер: пишет и правит код
      prompt: "Ты инженер. Пишешь и правишь код по контексту…"
      rag: false
      mcp: []
      memory: { wm: true }
    - name: reviewer       # ревьюер: проверяет результат
      prompt: "Ты ревьюер. Проверяешь корректность и риски…"
      rag: false
      mcp: []
      memory: { wm: false }
```

Доступ каждого саб-агента к инструментам гейтится ролью: `rag: true` даёт RAG-груундинг, `mcp: ["*"]` — все MCP-серверы, `mcp: ["petstore"]` — по allow-list, `mcp: []` — только LLM.

## Память

Оркестратор — **memory-aware**: инжектит общие блоки профиль / LTM / WM / память-задачи в промпт планирования и **подгружает диалоговую историю (STM)**, чтобы помнить прошлые задачи сеанса (уточнения вроде «теперь добавь…» видят прошлый результат). Саб-агенты работают на **эфемерной** памяти (in-memory STM+WM, засеянная контекстом от оркестратора; общие LTM/профиль — **только на чтение** через `storage.ReadOnlyLTM`), поэтому не затирают сессионную память.

| Слой | Флаг (легаси) | Файл | Назначение |
|---|---|---|---|
| STM | `--history` | `chat_history.json` | История диалога |
| WM | `--memory-wm` | `*.wm.json` | Факты текущей задачи |
| LTM | `--memory-ltm` | `*.ltm.json` | Профиль, стабильные предпочтения |
| Память задачи | — | `*.taskmem.json` | Цель / что уточнено / ограничения |
| Профиль | `--profile` | `*.profile.md` | Профиль пользователя (верхний слой) |

Пути WM/LTM/и пр. выводятся из `memory.dir` автоматически.

## Интерактивный режим (TUI)

Полноэкранный интерфейс (`tui.go`, gotui v5): шапка + прокручиваемый лог диалога + многострочный ввод. Задача выполняется в фоне, прогресс оркестратора/саб-агентов стримится в лог. Вне TTY или с `--no-tui` — обычный строчный REPL.

**Ввод** (многострочный `widgets.TextArea`):
- **Enter** — новая строка (набор/абзацы работают; вставка из буфера сохраняется целиком — gotui отдаёт вставленный `\n` как Enter)
- **Ctrl+S** — отправить (модификаторы на Enter в gotui/tcell не детектятся; если комбинация не доходит — `/keys` включает диагностику нажатий в логе)
- **↑/↓** — курсор между строками; на верхней/нижней границе — история ввода (shell-style, с сохранением черновика)

**Навигация и выделение**:
- **тачпад/колесо — прокрутка лога работает по умолчанию** (мышь захвачена приложением; без захвата терминал вообще не передаёт скролл — крутится его собственный буфер)
- **выделение текста при захваченной мыши** — модификатором терминала: Option+drag (iTerm2) / Fn+drag (Terminal.app); либо **`/select`** (или F2) — отпустить мышь для нативного выделения без модификаторов (ценой тачпад-скролла)
- **PgUp / PgDn / Home / End** — прокрутка лога (работает всегда, в обоих режимах); follow-bottom отпускается при скролле вверх и возвращается внизу
- **Ctrl+C** — выход

**Копирование**: `/copy` кладёт последний ответ в системный буфер (`pbcopy`/`wl-copy`/`xclip`/`xsel`, с фолбэком на OSC 52 — работает и по SSH).

**Рендеринг лога** (`tui_render.go`): цветные теги стадий (`[orchestrator]`, `[subagent …]`, `[rag]`, `[mcp]`), markdown-lite для планов и ответов (заголовки `#`, списки `- `/`+ `, `**жирный**`, `` `код` ``, правила `---`), фенсы ```` ``` ```` — verbatim за гаттером. Разметка защищена от порчи «непроизвольным» кодом (флангинг-проверка для `**`, `* ` — не маркер списка) — корректность важнее цвета.

**Слэш-команды** (двуязычные): `/help`·`/помощь` (список команд **+ вход в режим консультанта по документации**), `/end`·`/конец` (выход из режима консультанта), `/agents`·`/агенты`, `/memory`·`/память`, `/mcp`·`/mcp-list`, `/tools`·`/инструменты`, `/copy`·`/копировать`, `/select`·`/выделение`, `/clear`·`/очистить`, `/exit`·`/выход`. Любая строка на `/` — команда, в LLM не уходит (неизвестная `/cmd` сообщается, не отправляется).

**Режим консультанта по документации** (`/help` → … → `/end`): вопросы отвечаются не оркестратором, а заземлённым Q&A по индексу документации (`consultant.rag` в конфиге, по умолчанию `../rag/docs.db`) с доступом к MCP-инструментам из `consultant.mcp` (по умолчанию `["git"]` — ветка/файлы/diff). История режима — эфемерная (сырые вопрос/ответ, окно 12 сообщений), общая история сессии не трогается. Embed-модель консультанта обязана совпадать с той, которой построен `docs.db`.

**Согласование плана** (human-in-the-loop): экшен `ask_user` ставит выполнение на паузу — оркестратор показывает план и ждёт, пока пользователь **согласует, прокомментирует или вернёт на доработку**; ответ вплетается в транскрипт. В одноразовом CLI (без prompter) оркестратор действует автономно.

## Архитектура (Clean Architecture)

| Пакет | Что |
|---|---|
| `internal/config` | YAML-документ, `Load` / `Default` / `ResolveEnv` / `Overrides` / `Validate` — контракт слияния и приоритетов |
| `internal/app` | Композиционный корень (держит `usecase` свободным от адаптеров): `Toolbelt` (общий LLM, RAG-пайплайн, пул MCP-инструментов + маршрутизация tool→server), `MemoryFactory` (shared vs ephemeral память), `Orchestrator`, `SubAgent` |
| `internal/usecase` | `ChatUseCase.ExecuteWithTools` (MCP tool-loop), `RAGUseCase` (retrieve→rerank→filter→grounded), память (`UpdateWM`/`UpdateLTM`/`TaskMemory…`) |
| `internal/adapter` | `llm/` (OpenAI-совместимый HTTP-клиент, `ca_cert`, gen-defaults), `rag/` (эмбеддер + SQLite-поиск + 2 реранкера), `storage/` (файловые + in-memory репозитории, `ReadOnlyLTM`) |
| `internal/domain` / `internal/port` | Сущности и контракты-интерфейсы |

Каждый `SubAgent` переиспользует тот же движок, что и остальной агент — `ChatUseCase.ExecuteWithTools` плюс опциональный RAG-груундинг через `RAGUseCase.BuildPrompt` — на своей эфемерной памяти, с доступом к инструментам по роли.

## Make-цели (Day31)

| Цель | Что делает |
|---|---|
| `make build` / `test` / `vet` / `clean` | Сборка / юнит-тесты / статанализ / очистка |
| `run-orchestrator AGENT_TASK="…"` | Один запрос через оркестратор (`AGENT_CONFIG=`, умолч. `agent.config.yaml`) |
| `run-orchestrator-interactive` | Интерактивный TUI-сеанс |
| `run-orchestrator-rag` | Включить RAG флагами поверх конфига (`RAG_DB=`, `RAG_TOP_K=`, `RAG_THRESHOLD=`, `RAG_TOP_K_FINAL=`) |
| `run-orchestrator-local LOCAL_LLM_BASE_URL=…` | Оркестратор на локальном OpenAI-совместимом эндпоинте (LM Studio / Ollama) |

Makefile также сохраняет RAG-набор (`run-rag*`, `run-rag-eval`/`grounded`/`compare`/`rerank-compare` + их `-local`), MCP-config-тесты, telegram-opt и LiteLLM-цели из Day30.

## RAG и MCP (сохранены из прошлых дней)

RAG-конвейер (`--rag`): вопрос → эмбеддинг → поиск top-K в `rag.db` → опциональный реранк выделенной моделью → фильтр по порогу → grounded LLM-вызов со **структурированным ответом** (ответ + источники + дословные цитаты; при релевантности ниже порога — честное «не знаю» без вызова LLM). Индекс строит компонент `../rag/` (`index`/`search`/`stats`).

> Запрос эмбеддится **той же моделью**, что и индекс. `../rag/rag.db` построен `text-embedding-nomic-embed-text-v2-moe` (768 dims) — другая модель даёт бессмысленную близость.

MCP (`mcp.enabled: true`): пул подключений к нескольким серверам (stdio + HTTP/SSE). Собственный Petstore MCP-сервер — в `../mcp-server/` (22 инструмента `pet_*`/`store_*`/`user_*`/`report_*`). Саб-агенты получают инструменты по allow-list роли; в TUI `/mcp` и `/tools` показывают живой набор серверов и инструментов.

## Переменные окружения

| Переменная | Обяз. | По умолчанию |
|---|---|---|
| `LLM_PROVIDER` | да | — (`openai` / `openrouter` / `gigachat`) |
| `LLM_API_KEY` | да | — |
| `LLM_MODEL` | нет | дефолт провайдера |
| `LLM_BASE_URL` | нет | дефолт провайдера |
| `LLM_CA_CERT` | нет | — (PEM для self-signed HTTPS) |
| `AGENT_CONFIG` | нет | `agent.config.yaml` |

Секреты держите в окружении — пустые LLM-поля в YAML берутся из `LLM_*`.

## Тесты

```bash
make test          # юнит-тесты
make vet
```

Юнит-тесты покрывают: контракт мерджа/оверрайда/валидации конфига, изоляцию эфемерной vs общей памяти, цикл оркестратора spawn→route→finish на скриптованном mock-LLM, TUI-рендеринг (markdown-lite, экранирование разметки, прокрутка/история), слэш-команды. Guard проверяет, что shipped `agent.config.yaml` валиден. Интеграционные RAG-тесты (`-tags integration`) и `telegram-opt` требуют эндпоинт эмбеддингов / LLM-креды.
