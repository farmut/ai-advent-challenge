# Day 15: Review Prompts, PendingPlan и Chain-of-Thought Compliance

Расширение Day14. Три улучшения надёжности: единый механизм подтверждения `/yes` на всех точках FSM, сохранение плана до показа пользователю, и chain-of-thought при проверке инвариантов.

## Что нового в Day15

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
| Slash-команды `/exit` `/restart` `/pause` | Day14 |
| **`/yes`-only review prompts на всех gate'ах FSM** | **Day15** |
| **`PendingPlan`: план сохраняется до одобрения** | **Day15** |
| **Chain-of-thought compliance + парсер последней строки** | **Day15** |

---

## `/yes`-only review prompts

До Day15 разные точки FSM использовали разные функции подтверждения: Planning и Validation принимали `/yes`, а Execution continue и стартовый prompt «Resume paused task?» принимали `y`/`yes`. Это создавало несогласованный интерфейс.

В Day15 все точки переведены на единый `reviewPrompt()`:

| Точка FSM | Подтверждение | Пауза | Отклонение |
|---|---|---|---|
| Старт: «Resume paused task?» | `/yes` | Enter / `/no` | Enter / `/no` |
| Planning: «Approve this plan?» | `/yes` | Enter | `/no` или текст фидбэка |
| Execution: «Continue to validation?» | `/yes` | Enter / любой ввод | Enter / любой ввод |
| Validation: «Validate this result?» | `/yes` | Enter | `/no` или текст фидбэка |

```
╔══════════════════════════════════════╗
║   Interactive Agent  —  Day 15       ║
║   Phases: planning → execution →     ║
║           validation → done          ║
╠══════════════════════════════════════╣
║  All review prompts:                 ║
║    /yes  — approve / proceed         ║
║    /no   — reject without comment    ║
║    text  — revision comment          ║
║    Enter — pause                     ║
╠══════════════════════════════════════╣
║  /exit     — quit the agent          ║
║  /restart  — discard task, start over║
║  /resume   — resume a paused task    ║
║  /pause    — suspend at any prompt   ║
╚══════════════════════════════════════╝
```

### Поведение при разных вводах

**Planning / Validation** (`reviewPrompt`):

| Ввод | Результат |
|---|---|
| `/yes` | Одобрить → следующая фаза |
| `/no` | Отклонить без комментария → переплан |
| Текст | Текст фидбэка → переплан с комментарием |
| Enter (пусто) | Пауза, состояние сохраняется |
| `/exit` | Завершить агент |
| `/restart` | Удалить задачу, вернуться к `Task>` |

**Execution continue / Resume prompt**:

| Ввод | Результат |
|---|---|
| `/yes` | Перейти к следующей фазе |
| Всё остальное | Пауза |

---

## PendingPlan: план сохраняется до одобрения

### Проблема (Day14)

При паузе на этапе review плана FSM сохранял состояние с `Phase=planning`. При возобновлении агент заново вызывал LLM и генерировал **новый план**, хотя пользователь ожидал увидеть тот же.

### Решение (Day15)

Добавлено поле `PendingPlan` в `TaskState`. Логика:

```
LLM генерирует план
      │
      ▼
ts.PendingPlan = planContent   ← сохранить ДО показа пользователю
_ = a.task.Save(ts)
      │
      ▼
показать план пользователю
      │
      ├── /yes → ts.Plan = planContent; ts.PendingPlan = ""   ← одобрен
      ├── /no  → ts.PendingPlan = ""                          ← отклонён, переплан
      └── Enter → ts сохраняется с PendingPlan != ""          ← пауза
                  return ErrTaskPaused

при resumeTask:
      if ts.PendingPlan != "" → восстановить план, пропустить LLM-вызов
```

```json
// Пример сохранённого состояния при паузе на review
{
  "phase": "planning",
  "pending_plan": "1. Создать структуру...\n2. Реализовать...",
  "pending_feedback": ""
}
```

---

## Chain-of-thought compliance

### Проблема (Day14)

LLM-чекер возвращал вердикт одной строкой. При этом:
1. Модель делала поверхностную проверку и могла доверять само-декларациям плана («## Invariant Compliance: всё хорошо»).
2. Новые промпты с инструкцией «HOW TO CHECK» заставляли LLM писать аналитику **перед** вердиктом, а парсер проверял только **префикс** всего ответа — вердикт оказывался в конце и игнорировался.

### Решение (Day15)

**Промпты** теперь требуют структурированного разбора:

```
Work through EVERY invariant above one by one. For each one write:

CHECKING: <copy the invariant text exactly>
EVIDENCE: <what you observe in the result that relates to this invariant>
STATUS: PASS  — or —  FAIL: <quote the specific element that violates it>

After ALL checks, write the final verdict as the very last line:
COMPLIANT
or
VIOLATION: <for each FAIL: quote invariant, quote violating element, explain>
```

**Парсер** (`runComplianceCheck`) теперь читает **последнюю непустую строку**, а не весь префикс:

```go
// До (Day14): проверяет начало всего ответа
if strings.HasPrefix(strings.ToUpper(answer), "VIOLATION") { ... }

// После (Day15): читает только финальный вердикт
lines := strings.Split(strings.TrimSpace(answer), "\n")
verdict := ""
for i := len(lines) - 1; i >= 0; i-- {
    if line := strings.TrimSpace(lines[i]); line != "" {
        verdict = line; break
    }
}
if strings.HasPrefix(strings.ToUpper(verdict), "VIOLATION") { ... }
```

Это устраняет ложные срабатывания когда слово «VIOLATION» встречается в аналитической части ответа, и ложные «COMPLIANT» когда вердикт оказывался не в начале.

---

## Автоматическая проверка compliance (унаследовано от Day14, улучшено в Day15)

```
Пользователь вводит задачу
         │
         ▼
 ┌─────────────────────────────────────────────────┐
 │  1. TASK GATE (только итерация 1)               │
 │  Нарушает ли задача инварианты по природе?      │
 │  VIOLATION → ⛔ Task Refused, вернуться к Task> │
 │  COMPLIANT → продолжить                         │
 └─────────────────────────────────────────────────┘
         │
         ▼
 ┌─────────────────────────────────────────────────┐
 │  2. PLAN GATE (до 3 попыток, тихо)             │
 │  VIOLATION (попытки 1–2) → тихий ретрай        │
 │  VIOLATION (попытка 3)   → ⚑ Warning + план   │
 │  COMPLIANT → показать план → /yes для одобрения│
 └─────────────────────────────────────────────────┘
         │ /yes
         ▼
 ┌─────────────────────────────────────────────────┐
 │  3. VALIDATION GATE (авто)                      │
 │  VIOLATION → ⚑ Violation box, авто-возврат     │
 │              в PLANNING с targeted-fix          │
 │  COMPLIANT → показать prompt → /yes для приёма │
 └─────────────────────────────────────────────────┘
         │ /yes
         ▼
        DONE
```

Каждая проверка — отдельный `DirectCall` к LLM (без истории, без памяти, только system prompt + инварианты + проверяемый контент). Вердикт — последняя строка ответа.

---

## Машина состояний (FSM)

```
  ┌─────────┐  /yes    ┌───────────┐  /yes    ┌────────────┐  /yes  ┌──────┐
  │PLANNING │ ───────► │ EXECUTION │ ───────► │ VALIDATION │ ─────► │ DONE │
  └─────────┘          └───────────┘          └────────────┘        └──────┘
       ▲                                              │ violation (авто)
       └──────────────────────────────────────────────┘  или /no / текст
```

| Фаза | Что происходит | Пользователь |
|---|---|---|
| **PLANNING** | Task Gate → Plan Gate (авто) → показ плана | `/yes` — одобрить; `/no` — переплан; текст — переплан с фидбэком; Enter — пауза |
| **EXECUTION** | LLM выполняет план | После результата: `/yes` — к валидации; всё остальное — пауза |
| **VALIDATION** | Validation Gate (авто) → prompt | `/yes` — принять; `/no` — переплан без комментария; текст — переплан с фидбэком; Enter — пауза |
| **DONE** | Задача завершена, состояние очищается | — |

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

## Структура файлов на диске

| Файл | Назначение | Когда создаётся |
|---|---|---|
| `chat_history.json` | STM — диалог (Layer 1) | Всегда (если `--history` не пустой) |
| `chat_history.wm.json` | WM — рабочая память (Layer 2) | При `--memory-update` |
| `chat_history.ltm.json` | LTM — долгосрочная память (Layer 3) | При `--memory-update` |
| `chat_history.profile.md` | Профиль пользователя | При `--profile-init` / `--profile-name` / `--profile-set` |
| `chat_history.invariants.md` | Инварианты | Создаётся вручную; путь задаётся `--invariants` |
| `chat_history.task.json` | Состояние задачи FSM | В интерактивном режиме; удаляется по завершении |
| `chat_history.stats.json` | Накопленная статистика токенов | При `--show-tokens` / `--show-cost` |
| `chat_history.summary.txt` | Суммаризация истории | При `--summary` + переполнении |
| `chat_history.facts.json` | KV-факты sticky-facts | При `--strategy sticky-facts` |
| `chat_history.branch-state.json` | Состояние веток | При `--strategy branching` |

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

### Compliance pipeline (Day14+, улучшено в Day15)

| Функция | Когда | Что проверяет |
|---|---|---|
| `checkTaskDescription` | Итерация 1, до планирования | Задача пользователя |
| `checkPlanCompliance` | После каждой генерации плана | Предложенный план |
| `checkInvariantsCompliance` | После execution, до user prompt | Результат выполнения |

Все три: `DirectCall` → chain-of-thought по каждому инварианту → финальный вердикт последней строкой → парсер читает только последнюю строку.

### Prompt helpers

| Функция | Одобрение | Используется |
|---|---|---|
| `prompt()` | `y` / `yes` | — (устаревший, сохранён для обратной совместимости внутри кода) |
| `reviewPrompt()` | `/yes` | Все точки FSM: Planning, Execution, Validation, Resume |

---

## Сборка и тесты

```bash
cd Day15

go build -o ai-adv-agent-day15 .   # или: make build
go test -v ./...                    # или: make test
go vet ./...                        # или: make vet
```

Тесты распределены по пакетам:

| Пакет | Что тестируется |
|---|---|
| `internal/domain` | `TaskState` FSM, переходы, `RetryPlanning`, `PendingPlan` |
| `internal/usecase` | `AgentUseCase`: happy path, все точки паузы, resume + PendingPlan restore, `/yes`/`/no`/текст/Enter на каждой фазе; compliance chain-of-thought (pass/fail/last-line parser); slash-команды на всех фазах; memory injection |
| `internal/adapter/llm` | HTTP-payload |
| `internal/adapter/storage` | Все репозитории включая `InvariantsRepository` |

---

## Makefile targets

```bash
# Сборка и тесты
make build
make test
make vet
make clean

# Ручной запуск
make run-interactive \
    AGENT_PROFILE_FILE=profile_go.md \
    INVARIANTS_FILE=invariants.md

make run-interactive-manual \
    INTERACTIVE_PROFILE_FILE=profile_go.md \
    INVARIANTS_FILE=invariants.md

# Автоматические интеграционные тесты
make run-interactive-test-happy        # happy path: все 4 фазы до DONE
make run-interactive-test-pause-resume # пауза на PLANNING, resume, завершение
make run-interactive-test              # запустить оба

# Полный список
make help
```

---

## Переменные окружения

| Переменная | Обязательна | Умолч. |
|---|---|---|
| `LLM_PROVIDER` | Да | — (`openai` или `openrouter`) |
| `LLM_API_KEY` | Да | — |
| `LLM_MODEL` | Нет | `gpt-4o` (openai) / `openai/gpt-4o-mini` (openrouter) |
| `LLM_BASE_URL` | Нет | Стандартный URL провайдера |
