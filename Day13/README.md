# Day 13: Interactive Agent with Task State Machine

Расширение Day12. Добавлен **интерактивный режим** (`--interactive`) с машиной состояний из 4 фаз и полной поддержкой **паузы/возобновления** на любом этапе.

## Что нового в Day13

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
| **Интерактивный режим (`--interactive`)** | **Day13** |
| **Машина состояний: planning → execution → validation → done** | **Day13** |
| **Пауза/возобновление на любой фазе** | **Day13** |

---

## Интерактивный режим

Запускается флагом `--interactive`. В этом режиме агент не принимает одиночный `--query`, а ведёт диалог через терминал, управляя задачей по фазам.

### Машина состояний

```
  ┌─────────┐   approve    ┌───────────┐   auto     ┌────────────┐   accept   ┌──────┐
  │PLANNING │ ──────────► │ EXECUTION │ ─────────► │ VALIDATION │ ─────────► │ DONE │
  └─────────┘             └───────────┘            └────────────┘            └──────┘
       ▲                                                   │ reject
       └───────────────────────────────────────────────────┘
```

| Фаза | Что происходит | Пользователь |
|---|---|---|
| **PLANNING** | LLM предлагает пошаговый план | `y` — одобрить, `pause` — приостановить, любой текст — отклонить с фидбэком |
| **EXECUTION** | LLM выполняет план (до 120 с) | После появления результата: `y` — перейти к валидации, `pause` — приостановить |
| **VALIDATION** | Пользователь проверяет результат | `y` — принять, `pause` — приостановить, любой текст — отклонить, вернуться к планированию |
| **DONE** | Задача завершена, состояние очищается | — |

### Пауза и возобновление

На любом пользовательском prompt можно ввести `pause` — агент сохранит текущее состояние на диск и завершится. При следующем запуске с тем же `--history` агент обнаружит сохранённое состояние и предложит продолжить.

```
[PAUSED TASK FOUND]
  Phase:     planning
  Iteration: 1
  Task:      Написать REST API на Go
Resume paused task? [y/yes | any other key to discard]
> y
[resuming] phase=planning  iteration=1
```

**Как поставить паузу на каждой фазе:**

- **PLANNING** — появляется подсказка после плана:
  ```
  Approve this plan? [y/yes = approve | "pause" = suspend | any other text = revise]
  > pause
  ```
- **EXECUTION** — LLM работает молча (блокирующий вызов). Пауза доступна **после** того как результат появился на экране:
  ```
  Continue to validation? [y/yes = proceed | "pause" = suspend]
  > pause
  ```
- **VALIDATION** — появляется подсказка после результата:
  ```
  Validate this result? [y/yes = accept | "pause" = suspend | any other text = reject]
  > pause
  ```

Состояние сохраняется в файл `<history>.task.json`. При отклонении плана или результата старый результат автоматически сбрасывается, и EXECUTION делает новый вызов к LLM с учётом фидбэка.

### Команды на основном prompt

```
Task> <текст задачи>   — начать новую задачу
Task> resume           — возобновить приостановленную задачу
Task> exit / quit      — завершить агент
```

---

## Профиль пользователя

Профиль (`--profile`) инжектируется как первый блок системного сообщения в каждый LLM-вызов — и в CLI-, и в интерактивном режиме. Хранится в `.md`-файле, читается и редактируется вручную.

```
[User Profile]
Name: Andrey
language: russian
style: concise
format: markdown
expertise: golang-developer
```

### Управление профилем

```bash
# Интерактивная инициализация (диалог в терминале)
./ai-adv-agent-day13 --profile my.md --profile-init

# Установить имя и выйти
./ai-adv-agent-day13 --profile my.md --profile-name "Andrey"

# Установить/обновить одно или несколько предпочтений
./ai-adv-agent-day13 --profile my.md \
    --profile-set "language=russian" \
    --profile-set "style=concise"

# Удалить предпочтение
./ai-adv-agent-day13 --profile my.md --profile-delete "constraints"

# Просмотреть текущий профиль
./ai-adv-agent-day13 --profile my.md --profile-list
```

---

## Флаги

```
# Режимы
--query           Запрос к LLM (обязателен в CLI-режиме)
--interactive     Запустить интерактивный режим с машиной состояний

# Общие
--system          Системное сообщение (опционально)
--format          markdown | json (умолч. markdown)
--format-hint     Кастомная инструкция форматирования
--max-tokens      Лимит токенов ответа (0 = без лимита)
--stop            Stop-последовательность (повторяемый)
--temperature     Температура 0.0–2.0 (умолч.: от провайдера)
--debug           Вывод JSON-payload в stderr
--show-tokens     Вывод разбивки токенов в stderr
--show-cost       Вывод оценки стоимости (подразумевает --show-tokens)

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

### Интерактивный режим

```bash
# Создать профиль (первый раз)
make run-profile-init PROFILE_FILE=/tmp/my.md

# Запустить агент (профиль обязателен)
make run-interactive AGENT_PROFILE_FILE=/tmp/my.md

# Свежая сессия для ручного тестирования (история и задача сбрасываются)
make run-interactive-manual INTERACTIVE_PROFILE_FILE=/tmp/my.md
```

Пример сессии:

```
╔══════════════════════════════════════╗
║   Interactive Agent  —  Day 13       ║
║   Phases: planning → execution →     ║
║           validation → done          ║
╚══════════════════════════════════════╝

Task> Написать функцию на Go, которая форматирует байты в человекочитаемый вид

┌─ [PLANNING] 1. PLANNING
┌── Proposed Plan
────────────────────────────────────────
1. Определить пороговые значения (B, KB, MB, GB, TB)
2. Реализовать функцию FormatBytes(n uint64) string
3. Написать тест-таблицу для крайних случаев
────────────────────────────────────────

Approve this plan? [y/yes = approve | "pause" = suspend | any other text = revise]
> y

┌─ [EXECUTION] 2. EXECUTION

┌── Execution Result
────────────────────────────────────────
func FormatBytes(n uint64) string { ... }
────────────────────────────────────────

Continue to validation? [y/yes = proceed | "pause" = suspend]
> y

┌─ [VALIDATION] 3. VALIDATION

Validate this result? [y/yes = accept | "pause" = suspend | any other text = reject]
> y

┌─ [DONE] 4. DONE
Task completed after 1 planning iteration(s).
```

### CLI-режим (без изменений)

```bash
# Разовый вопрос
./ai-adv-agent-day13 --query "Что такое goroutine?"

# С историей и профилем
./ai-adv-agent-day13 \
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
| `chat_history.task.json` | Состояние задачи FSM | В интерактивном режиме; удаляется по завершении задачи |
| `chat_history.stats.json` | Накопленная статистика токенов | При `--show-tokens` / `--show-cost` |
| `chat_history.summary.txt` | Суммаризация истории | При `--summary` + переполнении |
| `chat_history.facts.json` | KV-факты sticky-facts | При `--strategy sticky-facts` |
| `chat_history.branch-state.json` | Состояние веток | При `--strategy branching` |
| `chat_history.branch-<name>.json` | История конкретной ветки | При создании ветки |

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
       │  ProfileRepository, TaskRepository      │
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

### Машина состояний задачи (Day13)

```go
// internal/domain/task.go
type TaskPhase string  // "planning" | "execution" | "validation" | "done"

type TaskState struct {
    ID, Task, Plan, Result  string
    Phase                   TaskPhase
    Iteration               int    // увеличивается при каждом новом туре планирования
    PendingFeedback         string // сохраняется при паузе, восстанавливается при resume
    CreatedAt, UpdatedAt    string
}
```

Переходы: `planning→execution`, `execution→validation`, `validation→done`, `validation→planning` (отклонение). При переходе `validation→planning` `ts.Result` сбрасывается, чтобы EXECUTION сделал новый LLM-вызов с переработанным планом.

---

## Сборка и тесты

```bash
cd Day13

go build -o ai-adv-agent-day13 .   # или: make build
go test -v ./...                    # или: make test
go vet ./...                        # или: make vet
```

Тесты распределены по пакетам:

| Пакет | Что тестируется |
|---|---|
| `internal/domain` | `TaskState` FSM, переходы, `RetryPlanning`; ценообразование, токены, `ContextStatus` |
| `internal/usecase` | `AgentUseCase` FSM (happy path, все 3 точки паузы, resume, reject+re-plan, очистка результата); `ChatUseCase`, `ProfileUseCase`, `HistoryUseCase` |
| `internal/adapter/llm` | HTTP-payload (токены, stop, temperature, system message) |
| `internal/adapter/storage` | Path-хелперы, Load/Save для всех 8 типов репозиториев |

---

## Makefile targets

### Интерактивный режим

```bash
# Ручной запуск (профиль обязателен)
make run-interactive AGENT_PROFILE_FILE=/tmp/my.md

# Свежая сессия для ручного тестирования
make run-interactive-manual INTERACTIVE_PROFILE_FILE=/tmp/my.md

# Автоматические интеграционные тесты (pipe stdin, профиль создаётся автоматически)
make run-interactive-test-happy           # happy path: все 4 фазы до DONE
make run-interactive-test-pause-resume    # пауза на PLANNING, resume, завершение
make run-interactive-test                 # запустить оба теста
```

### Профиль пользователя

```bash
make run-profile-init        PROFILE_FILE=my.md   # диалог инициализации
make run-profile-set         PROFILE_FILE=my.md   # установить имя + пример предпочтений
make run-profile-list        PROFILE_FILE=my.md   # вывести текущий профиль
make run-profile-delete  KEY=constraints PROFILE_FILE=my.md
make run-profile-demo                             # создать профиль + запрос + проверить инъекцию

# Профильные интеграционные сценарии (WM + LTM + Profile)
make run-profile-go-dev      # Andrey — Golang/Telegram-bot, все 4 слоя памяти
make run-profile-ai-critic   # Maria  — AI-критик, все 4 слоя памяти
make run-profile-data-sci    # Alex   — ML-эксперимент, все 4 слоя памяти
make run-profile-all         # запустить все три последовательно

# Profile-Only (без WM/LTM)
make run-profile-only-go-dev
make run-profile-only-ai-critic
make run-profile-only-data-sci
make run-profile-only-all
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
