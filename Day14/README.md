# Day 14: Interactive Agent with Invariant Enforcement

Расширение Day13. Добавлена система **инвариантов** — абсолютных ограничений, которые агент обязан соблюдать на каждом этапе работы. Инварианты проверяются автоматически на трёх уровнях до того, как пользователь что-либо увидит. Также добавлены **slash-команды** (`/exit`, `/restart`, `/pause`) и улучшено управление сессией.

## Что нового в Day14

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
| **Инварианты (`--invariants`)** | **Day14** |
| **Автоматическая проверка compliance на 3 уровнях** | **Day14** |
| **Slash-команды `/exit` `/restart` `/pause`** | **Day14** |
| **Вывод путей файлов и статусов памяти при старте** | **Day14** |

---

## Система инвариантов

Инварианты — это абсолютные ограничения, которые агент **никогда не должен нарушать**: технологический стек, архитектурные правила, бизнес-требования. Они задаются в Markdown-файле и передаются флагом `--invariants`.

```bash
./ai-adv-agent-day14 --interactive --invariants invariants.md --profile my.md
```

Пример файла `invariants.md`:

```markdown
## Architecture
- Для разработки используем только Clean Architecture

## Technology Stack
- Используем Golang версию 1.25+
- Разрешено использование стандартной библиотеки Golang
- Для логирования используем только log/slog. Вывод только в stdout.
- Хранить данные можно в JSON файлах или PostgreSQL

## Business Rules
- Каждая функция должна быть прокомментирована кратким описанием
- Для каждой функции должен быть unit-тест
```

Инварианты инжектируются во все системные промпты PLANNING и EXECUTION фаз как блок `⚑ INVARIANTS — ABSOLUTE LAW (NEVER VIOLATE)`.

---

## Автоматическая проверка compliance

Проверка выполняется через отдельные `DirectCall`-вызовы к LLM (без истории, без памяти) на трёх уровнях:

```
Пользователь вводит задачу
         │
         ▼
 ┌─────────────────────────────────────────────────┐
 │  1. TASK GATE (только итерация 1)               │
 │  Нарушает ли задача инварианты по природе?      │
 │  ──────────────────────────────────────────     │
 │  VIOLATION → ⛔ Task Refused, вернуться к Task> │
 │  COMPLIANT → продолжить                         │
 └─────────────────────────────────────────────────┘
         │
         ▼
 ┌─────────────────────────────────────────────────┐
 │  2. PLAN GATE (до 3 попыток, тихо)             │
 │  Нарушает ли план инварианты?                   │
 │  ──────────────────────────────────────────     │
 │  VIOLATION (попытки 1–2) → тихий ретрай        │
 │  VIOLATION (попытка 3)   → ⚑ Warning + план   │
 │  COMPLIANT → показать план пользователю         │
 └─────────────────────────────────────────────────┘
         │ пользователь одобряет
         ▼
 ┌─────────────────────────────────────────────────┐
 │  3. VALIDATION GATE (авто)                      │
 │  Нарушает ли результат выполнения инварианты?   │
 │  ──────────────────────────────────────────     │
 │  VIOLATION → ⚑ Violation box, авто-возврат     │
 │              в PLANNING с targeted-fix          │
 │  COMPLIANT → показать prompt пользователю       │
 └─────────────────────────────────────────────────┘
         │ пользователь принимает
         ▼
        DONE
```

### Уровень 1 — Task Gate

Выполняется **один раз** при первом вводе задачи (итерация 1). Проверяет, не противоречит ли сам запрос пользователя инвариантам. Консервативен: отказывает только если задача **принципиально несовместима** с ограничениями.

```
Task> Реализуй REST API с использованием фреймворка Gin

[invariants] checking task description for compliance...

┌── ⛔ Task Refused — Conflicts with Invariants
────────────────────────────────────────
VIOLATION: задача явно требует использования Gin, что нарушает
инвариант "Разрешено использование стандартной библиотеки Golang".
────────────────────────────────────────

This task cannot be executed: it conflicts with the active invariants.
Please reformulate your request to comply with the constraints listed above.

Task>
```

### Уровень 2 — Plan Gate

После каждой генерации плана LLM выполняется проверка. При нарушении:
- **Попытки 1–2**: тихий ретрай, пользователь не видит нарушающий план
- **Попытка 3** (все попытки исчерпаны): план показывается с баннером предупреждения

```
[invariants] checking plan (attempt 1/3)...
[invariants] plan attempt 1/3 violates constraints — retrying
[invariants] checking plan (attempt 2/3)...
[invariants] plan compliant

┌── Proposed Plan
────────────────────────────────────────
1. ...
────────────────────────────────────────
```

Если все 3 попытки нарушают инварианты:

```
┌── ⚑ Plan Compliance Warning — review required before approving
────────────────────────────────────────
VIOLATION: ...
────────────────────────────────────────

┌── Proposed Plan
...
```

### Уровень 3 — Validation Gate

После выполнения плана, **до** показа prompt пользователю, проверяется результат. При нарушении FSM **автоматически** возвращается в PLANNING без вмешательства пользователя:

```
[invariants] checking execution result for compliance...

┌── ⚑ Invariant Compliance Check — VIOLATION DETECTED
────────────────────────────────────────
VIOLATION: использован пакет gin/gonic, что нарушает инвариант...
────────────────────────────────────────

[invariants] Execution result violates invariant(s) — returning to PLANNING for a targeted fix.

┌─ [PLANNING]  1. PLANNING  (re-plan #2)
```

При targeted-fix replanning контекст чётко указывает что именно нужно исправить:
> «Keep all steps that are already compliant. Fix ONLY the step(s) responsible for the violation.»

---

## Slash-команды

Работают на **любом** пользовательском prompt внутри FSM:

| Команда | Действие |
|---|---|
| `/exit` или `exit` | Завершить агент с сообщением "Goodbye!" |
| `/restart` | Удалить текущую задачу, вернуться к `Task>` |
| `/pause` или `pause` | Сохранить состояние и приостановить |
| `/resume` или `resume` | Возобновить приостановленную задачу (на `Task>`) |

```
╔══════════════════════════════════════╗
║   Interactive Agent  —  Day 14       ║
║   Phases: planning → execution →     ║
║           validation → done          ║
╠══════════════════════════════════════╣
║  /exit     — quit the agent          ║
║  /restart  — discard task, start over║
║  /resume   — resume a paused task    ║
║  /pause    — suspend at any prompt   ║
╚══════════════════════════════════════╝
```

---

## Машина состояний (FSM)

```
  ┌─────────┐   approve    ┌───────────┐   auto     ┌────────────┐   accept   ┌──────┐
  │PLANNING │ ──────────► │ EXECUTION │ ─────────► │ VALIDATION │ ─────────► │ DONE │
  └─────────┘             └───────────┘            └────────────┘            └──────┘
       ▲                                                   │ violation (auto)
       └───────────────────────────────────────────────────┘  or user reject
```

| Фаза | Что происходит | Пользователь |
|---|---|---|
| **PLANNING** | Task Gate → Plan Gate (авто) → показ плана | `y` — одобрить, `/pause`, `/restart`, `/exit`, любой текст — отклонить с фидбэком |
| **EXECUTION** | LLM выполняет план (до 120 с) | После результата: `y` — к валидации, `/pause` |
| **VALIDATION** | Validation Gate (авто) → prompt | `y` — принять, `/pause`, любой текст — отклонить, вернуться к планированию |
| **DONE** | Задача завершена, состояние очищается | — |

---

## Отображение статусов при старте

При запуске агент выводит пути всех файлов и статус загрузки каждого слоя памяти:

```
[session] Memory file paths:
  STM  (history)   : /tmp/agent_session_day14.json
  WM   (working)   : /tmp/agent_session_day14.wm.json
  LTM  (long-term) : /tmp/agent_session_day14.ltm.json
  Profile          : /tmp/agent_profile_day14.md
  Invariants       : invariants.md
  Task state       : /tmp/agent_session_day14.task.json

[memory] profile: loaded (5 items)
[memory] WM: 3 facts loaded
[memory] LTM: empty
[memory] invariants: active (512 bytes)
```

---

## Флаги

```
# Режимы
--query           Запрос к LLM (обязателен в CLI-режиме)
--interactive     Запустить интерактивный режим с машиной состояний

# Общие
--system          Системное сообщение (добавляется к фазовым промптам FSM)
--format          markdown | json (умолч. markdown)
--format-hint     Кастомная инструкция форматирования
--max-tokens      Лимит токенов ответа (0 = без лимита)
--stop            Stop-последовательность (повторяемый)
--temperature     Температура 0.0–2.0 (умолч.: от провайдера)
--debug           Вывод JSON-payload в stderr
--show-tokens     Вывод разбивки токенов в stderr
--show-cost       Вывод оценки стоимости (подразумевает --show-tokens)

# Инварианты (Day14)
--invariants      Путь к файлу инвариантов (.md; умолч. <history>.invariants.md)

# Layer 1 — STM (история диалога)
--history         Путь к файлу истории (умолч. chat_history.json; "" = отключено)
--history-limit   Макс. сообщений при --summary (умолч. 10; 0 = без лимита)
--summary         Включить авто-суммаризацию при переполнении

# Layer 2 — Working Memory
--memory-wm       Путь к файлу рабочей памяти (умолч. <history>.wm.json)

# Layer 3 — Long-term Memory
--memory-ltm      Путь к файлу долгосрочной памяти (умолч. <history>.ltm.json)

# Авто-обновление памяти
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

## Примеры использования

### Запуск с инвариантами

```bash
# Создать профиль (первый раз)
make run-profile-init PROFILE_FILE=my.md

# Запустить агент с инвариантами
make run-interactive \
    AGENT_PROFILE_FILE=profile_go.md \
    INVARIANTS_FILE=invariants.md

# Свежая сессия для ручного тестирования
make run-interactive-manual \
    INTERACTIVE_PROFILE_FILE=profile_go.md \
    INVARIANTS_FILE=invariants.md
```

### Пример сессии с compliance

```
╔══════════════════════════════════════╗
║   Interactive Agent  —  Day 14       ║
...
╚══════════════════════════════════════╝

[memory] profile: loaded (4 items)
[memory] WM: empty
[memory] invariants: active (487 bytes)

Task> Написать HTTP-сервер на Go с логгером

[invariants] checking task description for compliance...

┌─ [PLANNING] 1. PLANNING
[invariants] checking plan (attempt 1/3)...
[invariants] plan compliant

┌── Proposed Plan
────────────────────────────────────────
1. Создать пакет main с HTTP-сервером на net/http
2. Подключить log/slog для структурированного логирования
3. Реализовать handler /health
4. Добавить unit-тест для handler
────────────────────────────────────────

Approve this plan? [y/yes = approve | /pause | /restart | /exit | text = revise]
> y

┌─ [EXECUTION] 2. EXECUTION
...

┌─ [VALIDATION] 3. VALIDATION
[invariants] checking execution result for compliance...
[invariants] compliance check passed

Validate this result? [y/yes = accept | /pause | /restart | /exit | text = reject]
> y

┌─ [DONE] 4. DONE
Task completed after 1 planning iteration(s).
```

### CLI-режим (без изменений)

```bash
./ai-adv-agent-day14 --query "Что такое goroutine?"

./ai-adv-agent-day14 \
  --query "Следующий шаг?" \
  --history session.json \
  --profile my.md \
  --memory-update
```

---

## Структура файлов на диске

| Файл | Назначение | Когда создаётся |
|---|---|---|
| `chat_history.json` | STM — диалог (Layer 1) | Всегда (если `--history` не пустой) |
| `chat_history.wm.json` | WM — рабочая память (Layer 2) | При `--memory-update` |
| `chat_history.ltm.json` | LTM — долгосрочная память (Layer 3) | При `--memory-update` |
| `chat_history.profile.md` | Профиль пользователя | При `--profile-init` / `--profile-name` / `--profile-set` |
| `chat_history.invariants.md` | Инварианты | Создаётся вручную; путь задаётся `--invariants` |
| `chat_history.task.json` | Состояние задачи FSM | В интерактивном режиме; удаляется по завершении задачи |
| `chat_history.stats.json` | Накопленная статистика токенов | При `--show-tokens` / `--show-cost` |
| `chat_history.summary.txt` | Суммаризация истории | При `--summary` + переполнении |
| `chat_history.facts.json` | KV-факты sticky-facts | При `--strategy sticky-facts` |
| `chat_history.branch-state.json` | Состояние веток | При `--strategy branching` |

Все пути выводятся из `--history`. Каждый можно переопределить явным флагом.

---

## Переменные окружения

| Переменная | Обязательна | Умолч. |
|---|---|---|
| `LLM_PROVIDER` | Да | — (`openai` или `openrouter`) |
| `LLM_API_KEY` | Да | — |
| `LLM_MODEL` | Нет | `gpt-4o` (openai) / `openai/gpt-4o-mini` (openrouter) |
| `LLM_BASE_URL` | Нет | Стандартный URL провайдера |

---

## Архитектура кода

### Clean Architecture (Day11+)

```
┌────────────────────────────────────────────────────────────────┐
│  main.go  (Composition Root)                                   │
│  Флаги, env, создание зависимостей, CLI/interactive split      │
└───────────────────────────┬────────────────────────────────────┘
                            │ создаёт
       ┌────────────────────▼────────────────────┐
       │  internal/usecase/                      │
       │  ChatUseCase, AgentUseCase,             │
       │  BranchUseCase, HistoryUseCase,         │
       │  MemoryUseCase, ProfileUseCase          │
       └────────────┬────────────────────────────┘
                    │ зависит от интерфейсов
       ┌────────────▼────────────────────────────┐
       │  internal/port/                         │
       │  LLMClient, HistoryRepository,          │
       │  StatsRepository, WMRepository,         │
       │  LTMRepository, BranchRepository,       │
       │  ProfileRepository, TaskRepository,     │
       │  InvariantsRepository                   │
       └────────────▲────────────────────────────┘
                    │ реализует
       ┌────────────┴────────────────────────────┐
       │  internal/adapter/                      │
       │  llm/client.go   — HTTP OpenAI-клиент  │
       │  storage/file.go — файловые репозитории │
       └─────────────────────────────────────────┘

       ┌─────────────────────────────────────────┐
       │  internal/domain/                       │  ← нет внешних зависимостей
       │  Message, Usage, SessionStats,          │
       │  WorkingMemory, LongTermMemory,         │
       │  FactsStore, BranchState,               │
       │  UserProfile, TaskState / TaskPhase     │
       └─────────────────────────────────────────┘
```

### Pipeline compliance-проверок (Day14)

Каждая проверка использует `DirectCall` — прямой вызов к LLM без истории, памяти и статистики. Три разных system prompt для трёх разных задач:

| Функция | Prompt | Что проверяет | Когда |
|---|---|---|---|
| `checkTaskDescription` | "inherently requires violating..." | Задача пользователя | Итерация 1, до планирования |
| `checkPlanCompliance` | "steps that would violate..." | Предложенный план | После каждой генерации плана |
| `checkInvariantsCompliance` | "execution result violates..." | Результат выполнения | После execution, до user prompt |

Все три возвращают либо `COMPLIANT`, либо `VIOLATION: <описание>`.

### FSM + слои памяти (Day14)

```
Системное сообщение = Profile + LTM + WM + Phase prompt + --system flag
                                                             ▲
                                           withDomainContext() добавляет сюда
```

Флаг `--system` приписывается к каждому фазовому промпту через `withDomainContext`, а не заменяет его.

---

## Сборка и тесты

```bash
cd Day14

go build -o ai-adv-agent-day14 .   # или: make build
go test -v ./...                    # или: make test
go vet ./...                        # или: make vet
```

Тесты распределены по пакетам:

| Пакет | Что тестируется |
|---|---|
| `internal/domain` | `TaskState` FSM, переходы, `RetryPlanning`; ценообразование, токены |
| `internal/usecase` | `AgentUseCase`: happy path, все 3 точки паузы, resume, reject+re-plan; task gate (отказ/пропуск); plan compliance (compliant/silent-retry/all-fail); validation compliance (violation→replan, pass→user); slash-команды `/exit` и `/restart` на всех фазах; memory layer injection; `--system` preservation; `ChatUseCase`, `ProfileUseCase`, `HistoryUseCase` |
| `internal/adapter/llm` | HTTP-payload (токены, stop, temperature, system message) |
| `internal/adapter/storage` | Path-хелперы, Load/Save для всех репозиториев включая `InvariantsRepository` |

---

## Makefile targets

### Интерактивный режим

```bash
# Ручной запуск (профиль обязателен; инварианты опциональны)
make run-interactive \
    AGENT_PROFILE_FILE=profile_go.md \
    INVARIANTS_FILE=invariants.md

# Свежая сессия для ручного тестирования
make run-interactive-manual \
    INTERACTIVE_PROFILE_FILE=profile_go.md \
    INVARIANTS_FILE=invariants.md

# Автоматические интеграционные тесты
make run-interactive-test-happy           # happy path: все 4 фазы до DONE
make run-interactive-test-pause-resume    # пауза на PLANNING, resume, завершение
make run-interactive-test                 # запустить оба теста
```

### Инварианты

```bash
# Показать/инициализировать файл инвариантов
make run-invariants-show INVARIANTS_FILE=invariants.md
make run-invariants-init INVARIANTS_FILE=invariants.md
```

### Профиль пользователя

```bash
make run-profile-init        PROFILE_FILE=my.md
make run-profile-set         PROFILE_FILE=my.md
make run-profile-list        PROFILE_FILE=my.md
make run-profile-delete  KEY=constraints PROFILE_FILE=my.md
make run-profile-demo

# Профильные интеграционные сценарии (WM + LTM + Profile)
make run-profile-go-dev      # Andrey — Golang/Telegram-bot, все 4 слоя памяти
make run-profile-ai-critic   # Maria  — AI-критик, все 4 слоя памяти
make run-profile-data-sci    # Alex   — ML-эксперимент, все 4 слоя памяти
make run-profile-all         # запустить все три последовательно
```

### 3-слойная память

```bash
make run-memory QUERY="Меня зовут Andrey, пишу Telegram-бота на Go"
make run-memory-demo          # 4 хода: накопление WM+LTM, 4-й ход с чистым STM
make run-memory-recall        # проверить recall без истории диалога
make run-memory-layers        # показать все 3 слоя рядом
```

### Стратегии контекста

```bash
make run-sliding-window                     # последние N сообщений
make run-sliding-window WINDOW_SIZE=3
make run-sticky-facts                       # KV-факты переживают ротацию окна
make run-branching                          # checkpoint → 2 ветки → переключения
make run-lion-book-test                     # 10-ходовая история × 3 стратегии + AI-критик
```

### История, токены, суммаризация

```bash
make run-history-all          # 4 теста истории
make run-summary-test         # авто-суммаризация при переполнении
make run-critic-test          # AI-критик: полная история vs. суммари
make run-tokens               # разбивка токенов
make run-tokens-cost          # разбивка + стоимость
make run-tokens-session       # накопленная статистика за 3 хода
make run-context-overflow     # поведение при переполнении контекста
```

### Основные

```bash
make build     # Собрать бинарь
make test      # Юнит-тесты (без API)
make vet       # go vet
make clean     # Удалить артефакты и временные файлы
make help      # Полный список целей
```
