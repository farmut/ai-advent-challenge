# Day 17: MCP (Model Context Protocol) — подключение внешних инструментов

Расширение Day15. Агент получает возможность подключаться к MCP-серверам, которые предоставляют наборы инструментов (filesystem, базы данных, API и т.д.). Конфигурация серверов хранится в YAML-файле. Внутри интерактивной сессии доступны `/mcp-*` slash-команды для управления серверами и просмотра инструментов прямо во время работы с агентом.

## Что нового в Day16

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
| **MCP-серверы: подключение, YAML-конфиг, список инструментов** | **Day16** |
| **`/mcp-*` slash-команды в интерактивном режиме** | **Day16** |

---

## MCP (Model Context Protocol)

MCP — открытый стандарт для подключения языковых моделей к внешним инструментам. Сервер предоставляет набор `tools` (функций), которые агент может вызывать. Day16 добавляет поддержку двух транспортов:

| Транспорт | Как работает | Пример |
|---|---|---|
| **stdio** | Сервер запускается как дочерний процесс; общение через stdin/stdout JSON-RPC 2.0 | `npx @modelcontextprotocol/server-filesystem` |
| **sse** | Сервер доступен по HTTP; подключение через Server-Sent Events, запросы через POST | `http://localhost:8080/sse` |

### Конфигурационный YAML-файл

Подключённые серверы фиксируются в YAML-файле (по умолчанию `<history>.mcp.yaml`):

```yaml
servers:
    - name: filesystem
      type: stdio
      command: npx
      args:
        - -y
        - '@modelcontextprotocol/server-filesystem'
        - /home/user/projects
    - name: remote-api
      type: sse
      url: http://localhost:8080/sse
```

Файл можно редактировать вручную или через CLI/slash-команды.

---

## CLI-управление MCP

### Добавить сервер

```bash
# stdio-сервер (subprocess)
./ai-adv-agent-day16 --mcp-add \
    --mcp-name filesystem \
    --mcp-type stdio \
    --mcp-command npx \
    --mcp-args "-y @modelcontextprotocol/server-filesystem /tmp"

# SSE-сервер (HTTP)
./ai-adv-agent-day16 --mcp-add \
    --mcp-name remote \
    --mcp-type sse \
    --mcp-url http://localhost:8080/sse

# С дополнительными переменными окружения для stdio
./ai-adv-agent-day16 --mcp-add \
    --mcp-name mydb \
    --mcp-type stdio \
    --mcp-command npx \
    --mcp-args "-y @modelcontextprotocol/server-sqlite db.sqlite" \
    --mcp-env "DEBUG=1"
```

### Просмотр и удаление

```bash
# Список настроенных серверов
./ai-adv-agent-day16 --mcp-list

# Список инструментов от конкретного сервера
./ai-adv-agent-day16 --mcp-tools filesystem

# Список инструментов от всех серверов
./ai-adv-agent-day16 --mcp-tools-all

# Удалить сервер
./ai-adv-agent-day16 --mcp-remove filesystem
```

Пример вывода `--mcp-list`:

```
MCP config: /tmp/agent.mcp.yaml
Configured MCP servers (2):

  Name:    filesystem
  Type:    stdio
  Command: npx
  Args:    -y @modelcontextprotocol/server-filesystem /tmp

  Name:    remote
  Type:    sse
  URL:     http://localhost:8080/sse
```

Пример вывода `--mcp-tools filesystem`:

```
=== Server: filesystem (14 tools) ===

   1. read_file             Read the complete contents of a file as text.
   2. read_text_file        Read file from the filesystem as text.
   3. write_file            Create or overwrite a file with new content.
   4. list_directory        Get a listing of all files and directories.
   ...
```

---

## Slash-команды `/mcp-*` в интерактивном режиме

Все MCP-команды доступны **в любом промпте** интерактивного агента — в том числе во время review плана, выполнения или валидации. Они не влияют на состояние FSM: после вывода результата агент переспрашивает тот же промпт.

```
╔══════════════════════════════════════════╗
║   Interactive Agent  —  Day 16          ║
║   Phases: planning → execution →        ║
║           validation → done             ║
╠══════════════════════════════════════════╣
║  All review prompts:                     ║
║    /yes  — approve / proceed             ║
║    /no   — reject without comment        ║
║    text  — revision comment              ║
║    Enter — pause                         ║
╠══════════════════════════════════════════╣
║  /exit     — quit the agent              ║
║  /restart  — discard task, start over    ║
║  /resume   — resume a paused task        ║
║  /pause    — suspend at any prompt       ║
╠══════════════════════════════════════════╣
║  MCP (available everywhere):             ║
║  /mcp-list           — list servers      ║
║  /mcp-tools [name]   — list tools        ║
║  /mcp-add stdio/sse  — add server        ║
║  /mcp-remove <name>  — remove server     ║
╚══════════════════════════════════════════╝
```

### Синтаксис команд

| Команда | Описание |
|---|---|
| `/mcp-list` | Список всех настроенных серверов |
| `/mcp-tools` | Инструменты от всех серверов |
| `/mcp-tools <name>` | Инструменты от конкретного сервера |
| `/mcp-add stdio <name> <command> [args...]` | Добавить stdio-сервер |
| `/mcp-add sse <name> <url>` | Добавить SSE-сервер |
| `/mcp-remove <name>` | Удалить сервер |

### Пример сессии

```
Task> /mcp-list
[mcp] No MCP servers configured.
      Use /mcp-add to register one.

Task> /mcp-add stdio myfs npx -y @modelcontextprotocol/server-filesystem /tmp
[mcp] Server "myfs" added.

Task> /mcp-tools myfs
[mcp] Server "myfs" — 14 tool(s):
   1. read_file                       Read the complete contents of a file as text.
   2. list_directory                  Get a listing of all files and directories.
   ...

Task> Прочитай файл /tmp/hello.txt и напиши его содержимое заглавными буквами

[PLANNING] ...
```

### Поведение при вводе MCP-команды внутри FSM

```
Approve this plan? [/yes = approve | /no = reject | Enter = pause | text = revision comment]
> /mcp-tools myfs
[mcp] Server "myfs" — 14 tool(s):
   1. read_file ...
   ...

Approve this plan? [/yes = approve | /no = reject | Enter = pause | text = revision comment]
>
```

Команда обрабатывается, промпт повторяется — FSM не меняет фазу.

---

## MCP протокол (JSON-RPC 2.0)

Каждое подключение к серверу выполняет три шага:

```
Клиент                              Сервер
  │                                    │
  │── initialize ──────────────────►   │  negotiation (protocolVersion, capabilities)
  │◄──────────────── initialize resp ──│
  │── notifications/initialized ────►  │  handshake complete
  │── tools/list ───────────────────►  │  запрос списка инструментов
  │◄────────────────── tools/list resp ┤
  │                                    │
  │  (соединение закрывается)          │
```

Для **stdio** — соединение на время одного вызова `ListTools`: запускается subprocess, выполняется handshake, читается список инструментов, процесс завершается.

Для **SSE**:
1. `GET /sse` — устанавливается SSE-поток
2. Сервер присылает событие `endpoint` с URL сессии
3. JSON-RPC отправляется как `POST <session-url>`
4. Ответы приходят как SSE-события `message`

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

---

## Архитектура кода

### Clean Architecture (Day11+, расширена в Day16)

```
┌─────────────────────────────────────────────────────────────────┐
│  main.go  (Composition Root)                                    │
│  Флаги, env, создание зависимостей, CLI/interactive split       │
└───────────────────────────┬─────────────────────────────────────┘
                            │ создаёт
       ┌────────────────────▼─────────────────────────────┐
       │  internal/usecase/                               │
       │  ChatUseCase, AgentUseCase (+ WithMCP),          │
       │  BranchUseCase, HistoryUseCase,                  │
       │  MemoryUseCase, ProfileUseCase,                  │
       │  MCPUseCase ◄── NEW                              │
       └────────────┬─────────────────────────────────────┘
                    │ зависит от интерфейсов
       ┌────────────▼─────────────────────────────────────┐
       │  internal/port/                                  │
       │  LLMClient, HistoryRepository, StatsRepository,  │
       │  WMRepository, LTMRepository, BranchRepository,  │
       │  ProfileRepository, TaskRepository,              │
       │  InvariantsRepository,                           │
       │  MCPRepository, MCPToolLister ◄── NEW            │
       └────────────▲─────────────────────────────────────┘
                    │ реализует
       ┌────────────┴─────────────────────────────────────┐
       │  internal/adapter/                               │
       │  llm/client.go        — HTTP OpenAI-клиент       │
       │  storage/file.go      — файловые репозитории     │
       │  storage/mcp.go       — MCPConfigFile (YAML) ◄── NEW │
       │  mcp/client.go        — StdioClient + SSEClient ◄── NEW │
       └──────────────────────────────────────────────────┘

       ┌──────────────────────────────────────────────────┐
       │  internal/domain/                               │  ← нет внешних зависимостей
       │  Message, Usage, SessionStats,                  │
       │  WorkingMemory, LongTermMemory,                 │
       │  FactsStore, BranchState, UserProfile,          │
       │  TaskState / TaskPhase,                         │
       │  MCPServerConfig, MCPConfig, MCPTool ◄── NEW    │
       └──────────────────────────────────────────────────┘
```

### Новые компоненты Day16

| Файл | Роль |
|---|---|
| `internal/domain/mcp.go` | Типы `MCPServerConfig`, `MCPConfig`, `MCPTool`; константы `MCPStdio`/`MCPSSE` |
| `internal/port/port.go` | Интерфейсы `MCPRepository`, `MCPToolLister` |
| `internal/adapter/storage/mcp.go` | `MCPConfigFile` — YAML-сериализация через `gopkg.in/yaml.v3` |
| `internal/adapter/mcp/client.go` | `Client.ListTools()`: запускает stdio-subprocess или SSE-сессию, выполняет MCP JSON-RPC handshake, возвращает список инструментов |
| `internal/usecase/mcp.go` | `MCPUseCase`: `AddServer`, `RemoveServer`, `ListServers`, `ListTools` |

### Slash-команды в AgentUseCase

`AgentUseCase` получил метод `WithMCP(*MCPUseCase)` и `handleMCPCommand(cmd string)`. Оба метода `prompt()` и `reviewPrompt()` переписаны в цикл: если ввод начинается с `/mcp`, вызывается обработчик и промпт повторяется.

```
reviewPrompt() loop:
    читает строку
    ├── /mcp-* → handleMCPCommand() → continue (re-prompt)
    ├── /yes   → return approved=true
    ├── /no    → return approved=false
    ├── Enter  → return paused=true
    ├── /exit  → return ErrExitRequested
    └── текст  → return feedback=text
```

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

## Сборка и тесты

```bash
cd Day16

go build -o ai-adv-agent-day16 .   # или: make build
go test -v ./...                    # или: make test
go vet ./...                        # или: make vet
```

| Пакет | Что тестируется |
|---|---|
| `internal/domain` | `TaskState` FSM, переходы, `PendingPlan`; типы MCP |
| `internal/usecase` | `AgentUseCase`: happy path, паузы, resume, compliance; `MCPUseCase`: add/remove/list/duplicate/not-found |
| `internal/adapter/llm` | HTTP-payload |
| `internal/adapter/storage` | Все репозитории включая `MCPConfigFile` (YAML) |
| `internal/adapter/mcp` | — (интеграционные тесты через Makefile) |

---

## Makefile targets

### Ручной запуск с MCP

```bash
# Свежая сессия, авто-регистрирует filesystem MCP-сервер (если есть npx)
make run-mcp-session

# То же, но с другим корневым каталогом для filesystem сервера
make run-mcp-session MCP_FS_ROOT=/home/user/projects

# С профилем пользователя
make run-mcp-session MCP_SESSION_PROF=/tmp/my_profile.md

# Persistent-сессия (история сохраняется между запусками)
make run-mcp-session-persist
```

### Интеграционные тесты MCP

```bash
# Тест 1: YAML round-trip (add/list/remove) — без API key
make run-mcp-add-list-remove

# Тест 2: получение списка tools через реальный MCP-сервер — без API key, нужен npx
make run-mcp-tools-filesystem

# Тесты 1+2 вместе
make run-mcp-test

# Тест 3: /mcp-* slash-команды в interactive-режиме — нужен API key
make run-mcp-slash-test

# Все 3 теста
make run-mcp-all
```

### Обычный интерактивный режим

```bash
make run-interactive \
    AGENT_PROFILE_FILE=profile_go.md \
    INVARIANTS_FILE=invariants.md

make run-interactive-manual \
    INTERACTIVE_PROFILE_FILE=profile_go.md

# Автоматические интеграционные тесты FSM
make run-interactive-test-happy
make run-interactive-test-pause-resume
make run-interactive-test

make help   # полный список всех targets
```

---

## Переменные окружения

| Переменная | Обязательна | Умолч. |
|---|---|---|
| `LLM_PROVIDER` | Да | — (`openai` или `openrouter`) |
| `LLM_API_KEY` | Да | — |
| `LLM_MODEL` | Нет | `gpt-4o` (openai) / `openai/gpt-4o-mini` (openrouter) |
| `LLM_BASE_URL` | Нет | Стандартный URL провайдера |

MCP-команды (`--mcp-list`, `--mcp-add`, `--mcp-tools`) не требуют `LLM_PROVIDER` и `LLM_API_KEY`.
