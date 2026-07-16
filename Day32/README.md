# Day 32: конфиг-ориентированный оркестратор саб-агентов

Day31 — рефакторинг разрозненных флагов одиночного агента в **единый оркестратор, управляемый одним YAML-конфигом**. Главный процесс больше не решает задачу сам: он ведёт LLM-цикл планирования, **спаунит саб-агентов** из ростера и **маршрутизирует их результаты** друг между другом, пока не сможет ответить.

```
пользователь → [оркестратор: цикл планирования]
                 │  каждый раунд — один JSON-экшен:
                 │   spawn    → запустить саб-агента (task + context)
                 │   ask_user → согласовать план с пользователем (human-in-the-loop)
                 │   finish   → выдать итоговый ответ
                 ▼
             саб-агенты (researcher / coder / reviewer …)
                 │  свой промпт-роль, свой доступ (RAG / MCP), своя эфемерная память
                 ▼
             результаты возвращаются в транскрипт оркестратора
```

Все прошлые возможности агента сохранены — MCP-инструменты, 3-слойная память, профиль, инварианты, RAG с реранкингом и цитированием. **Легаси-пути одиночного агента не тронуты**: без `--config` всё работает как раньше.

## Компоненты дня

| Каталог | Что это |
|---|---|
| `agent/` | Оркестратор + саб-агенты, MCP-клиент, RAG-пайплайн, TUI. Основной бинарь. |
| `mcp-server/` | Petstore MCP-сервер (stdio + HTTP/SSE), 22 инструмента. |
| `git-mcp-server/` | Read-only git MCP-сервер (stdio): `git_current_branch` / `git_list_files` / `git_diff`. |
| `rag/` | Индексатор корпуса (index / search / stats) для RAG. |

## Новое в Day32: агентское ревью PR в CI

Workflow [`.github/workflows/pr-agent-review.yml`](../.github/workflows/pr-agent-review.yml) на каждый PR запускает оркестратор с конфигом [`agent/agent.review.yaml`](agent/agent.review.yaml):

```
PR открыт/обновлён
  → diff-reader   (MCP github: get_pull_request_diff / get_pull_request_files)
  → bug-hunter    (потенциальные баги: файл, строка, серьёзность)
  → architect     (архитектурные проблемы)
  → code-reviewer (рекомендации по коду)
  → publisher     (MCP github: pending review → inline-комментарии → submit COMMENT
                   со сводкой «Баги / Архитектура / Рекомендации»)
```

- GitHub-доступ — официальный **github-mcp-server** (stdio, `docker run … ghcr.io/github/github-mcp-server`); токен и `GITHUB_TOOLSETS=pull_requests` наследуются из окружения workflow.
- Конфиг ревью эфемерный: вся память/RAG/консультант выключены, `temperature: 0.2`.
- Секреты: `OPENROUTER_API_KEY` (LLM) + встроенный `GITHUB_TOKEN` (`pull-requests: write`). PR из форков пропускаются (нет секретов и write-токена).
- Гвард-тест `TestReviewConfigIsValid` держит конфиг валидным (память выключена, ровно сервер `github`, гейтинг инструментов пяти ролей).

Локальный запуск ревью (нужен docker и токен):

```bash
cd agent && make build
export LLM_PROVIDER=openrouter LLM_API_KEY=sk-... LLM_MODEL=openai/gpt-4o
export GITHUB_PERSONAL_ACCESS_TOKEN=ghp_... GITHUB_TOOLSETS=pull_requests
./ai-adv-agent-agent --config agent.review.yaml \
  --query "Проведи code review pull request #123 в репозитории owner/repo"
```

## Быстрый старт

```bash
cd agent
make build                 # бинарь ai-adv-agent-agent

# секреты — в окружении
export LLM_PROVIDER=openrouter
export LLM_API_KEY=sk-...
export LLM_MODEL=openai/gpt-4o-mini

# один запрос
make run-orchestrator AGENT_TASK="Проанализируй репозиторий и предложи 3 улучшения"

# интерактивный TUI-сеанс
make run-orchestrator-interactive
```

Прямой вызов без Makefile:

```bash
./ai-adv-agent-agent --config agent.config.yaml --query "…"
./ai-adv-agent-agent --config agent.config.yaml --interactive
```

`--config` (или env `AGENT_CONFIG`) включает режим оркестратора. Без него — старый одиночный агент.

## Конфиг `agent.config.yaml`

Один файл включает и выключает возможности. **Каждый слой памяти включён по умолчанию** — слой указывают только чтобы выключить или настроить. RAG и MCP — opt-in.

Приоритет (низкий → высокий): **встроенные дефолты → YAML → явные CLI-флаги**. `config.Load` мержит файл поверх `config.Default()` (пропущенный `enabled:` оставляет слой включённым), `ResolveEnv` заполняет пустые LLM-поля из `LLM_*`, а `config.Overrides` (собран из `flag.Visit`) даёт победить только тем флагам, которые пользователь реально ввёл.

```yaml
llm:
  provider: ""            # env LLM_PROVIDER (openai | openrouter | gigachat)
  model: ""               # env LLM_MODEL
  temperature: null       # null = дефолт провайдера
  max_tokens: 0           # 0 = без лимита
  ca_cert: ""             # PEM для self-signed HTTPS (приватный LiteLLM, Day30)

memory:
  dir: chat_history.json  # база; путь каждого слоя выводится из неё
  auto_update: false      # обновлять WM+LTM доп. LLM-вызовом после хода
  stm:  { enabled: true, limit: 10 }
  wm:   { enabled: true }
  ltm:  { enabled: true }
  profile: { enabled: true }
  task_memory: { enabled: true }

rag:  { enabled: false, db: rag.db, top_k: 20, threshold: 0.5, top_k_final: 10 }
mcp:  { enabled: false, servers: [] }
invariants: { enabled: false }

orchestrator:
  enabled: true
  max_rounds: 8
  subagents:
    - name: researcher     # аналитик: собирает факты
      rag: true            # доступ к RAG (требует rag.enabled)
      mcp: []              # напр. ["petstore"] или ["*"] — все серверы
      memory: { wm: true }
    - name: coder          # инженер: пишет и правит код
    - name: reviewer       # ревьюер: проверяет результат
```

Доступ каждого саб-агента к инструментам гейтится его ролью: `rag: true` даёт RAG-груундинг, `mcp: ["*"]` — все MCP-серверы, `mcp: []` — только LLM.

## Память

Оркестратор — **memory-aware**: инжектит общие блоки профиль / LTM / WM / память-задачи в промпт планирования и **подгружает диалоговую историю (STM)**, чтобы помнить прошлые задачи сеанса (уточнения вроде «теперь добавь…» видят прошлый результат). Саб-агенты работают на **эфемерной** памяти (in-memory STM+WM, засеянная контекстом от оркестратора; общие LTM/профиль — только на чтение), поэтому не затирают сессионную память.

| Слой | Файл | Назначение |
|---|---|---|
| STM | `chat_history.json` | История диалога |
| WM | `*.wm.json` | Факты текущей задачи |
| LTM | `*.ltm.json` | Профиль, стабильные предпочтения |
| Память задачи | `*.taskmem.json` | Цель / что уточнено / ограничения |

## Интерактивный режим (TUI)

Полноэкранный интерфейс (`tui.go`, gotui v5): шапка + прокручиваемый лог диалога + многострочный ввод. Задача выполняется в фоне, прогресс оркестратора/саб-агентов стримится в лог. Вне TTY или с `--no-tui` — обычный строчный REPL.

**Ввод** (многострочный):
- **Enter** — новая строка (вставка/абзацы работают, вставка из буфера сохраняется целиком)
- **Ctrl+S** — отправить (если комбинация не доходит — /keys покажет, что шлёт клавиатура)
- **↑/↓** — курсор между строками; на верхней/нижней границе — история ввода

**Навигация и выделение**:
- **тачпад/колесо — прокрутка лога по умолчанию** (мышь захвачена); выделение — Option+drag (iTerm2) / Fn+drag (Terminal.app)
- **PgUp / PgDn / Home / End** — прокрутка лога (работает всегда)
- **`/select`** (или F2) — отпустить мышь: нативное выделение без модификаторов (ценой тачпад-скролла)
- **Ctrl+C** — выход

**Копирование**: `/copy` кладёт последний ответ в системный буфер (`pbcopy`/`wl-copy`/`xclip`/`xsel`, с фолбэком на OSC 52 — работает и по SSH).

**Слэш-команды** (двуязычные): `/help`·`/помощь`, `/agents`·`/агенты`, `/memory`·`/память`, `/mcp`, `/tools`·`/инструменты`, `/copy`·`/копировать`, `/select`·`/выделение`, `/clear`·`/очистить`, `/exit`·`/выход`. Любая строка на `/` — команда, в LLM не уходит.

**Согласование плана** (human-in-the-loop): экшен `ask_user` ставит выполнение на паузу — оркестратор показывает план и ждёт, пока пользователь **согласует, прокомментирует или вернёт на доработку**; ответ вплетается в транскрипт. В одноразовом CLI (без prompter) оркестратор действует автономно.

## Make-цели (в `agent/`)

| Цель | Что делает |
|---|---|
| `make build` / `test` / `vet` / `clean` | Сборка / юнит-тесты / статанализ / очистка |
| `run-orchestrator AGENT_TASK="…"` | Один запрос через оркестратор |
| `run-orchestrator-interactive` | Интерактивный TUI-сеанс |
| `run-orchestrator-rag` | Включить RAG флагами поверх конфига (`RAG_DB=`, `RAG_TOP_K=`, `RAG_THRESHOLD=`, `RAG_TOP_K_FINAL=`) |
| `run-orchestrator-local LOCAL_LLM_BASE_URL=…` | Оркестратор на локальном OpenAI-совместимом эндпоинте (LM Studio / Ollama) |

`AGENT_CONFIG` по умолчанию `agent.config.yaml`.

## Переменные окружения

| Переменная | Обяз. | По умолчанию |
|---|---|---|
| `LLM_PROVIDER` | да | — (`openai` / `openrouter` / `gigachat`) |
| `LLM_API_KEY` | да | — |
| `LLM_MODEL` | нет | дефолт провайдера |
| `LLM_BASE_URL` | нет | дефолт провайдера |
| `LLM_CA_CERT` | нет | — (PEM для self-signed HTTPS) |
| `AGENT_CONFIG` | нет | `agent.config.yaml` |

Секреты держите в окружении — в YAML их не пишите (пустые LLM-поля берутся из `LLM_*`).

## Тесты

Юнит-тесты покрывают: контракт мерджа/оверрайда/валидации конфига, изоляцию эфемерной vs общей памяти, цикл оркестратора spawn→route→finish на скриптованном mock-LLM, TUI-рендеринг (markdown-lite, экранирование разметки, прокрутка/история), слэш-команды. Guard проверяет, что shipped `agent.config.yaml` валиден.

```bash
cd agent && make test
```
