# Day 12: 3-Layer Memory Architecture

Расширение Day10 с архитектурой трёхслойной памяти агента: **кратковременная (STM)**, **рабочая (WM)** и **долгосрочная (LTM)** память хранятся в отдельных файлах и автоматически инжектируются в промпт.

## Что нового в Day11

| Возможность | День появления |
|---|---|
| Базовый HTTP-клиент, `--format`, `--debug` | Day2 |
| `--system` (системная роль) | Day3 |
| `--temperature` | Day4 |
| История диалога (`--history`), `--show-cost` | Day9 |
| `--summary` (авто-суммаризация, опциональна) | Day9 → Day10 |
| `--strategy sliding-window` | Day10 |
| `--strategy sticky-facts` | Day10 |
| `--strategy branching` + checkpoints | Day10 |
| `--memory-wm` (рабочая память, Layer 2) | **Day11** |
| `--memory-ltm` (долгосрочная память, Layer 3) | **Day11** |
| `--memory-update` (авто-обновление WM и LTM) | **Day11** |
| Clean Architecture (рефакторинг кода по слоям) | **Day11** |

## Архитектура памяти

Агент поддерживает три независимых слоя памяти, каждый в своём файле:

```
┌─────────────────────────────────────────────────────────┐
│  Layer 3 — LTM  (--memory-ltm)                          │
│  Долгосрочная: профиль пользователя, решения, знания    │
│  Файл: chat_history.ltm.json                            │
├─────────────────────────────────────────────────────────┤
│  Layer 2 — WM   (--memory-wm)                           │
│  Рабочая: факты по текущей задаче, ограничения, цели    │
│  Файл: chat_history.wm.json                             │
├─────────────────────────────────────────────────────────┤
│  Layer 1 — STM  (--history)                             │
│  Кратковременная: диалог (сообщения user/assistant)     │
│  Файл: chat_history.json                                │
└─────────────────────────────────────────────────────────┘
```

### Как слои попадают в промпт

При каждом вызове содержимое всех трёх слоёв собирается в системное сообщение в следующем порядке (от широкого к конкретному):

```
[Long-term Memory — Profile & Knowledge]
user_name: Андрей
preferred_language: Go
...

[Working Memory — Task Facts]
project: telegram-bot
constraint: только официальная Telegram-библиотека
requirement: статистика пользователей в отдельном файле
...

<пользовательский --system, если задан>

<история диалога (STM)>

[user query]
```

### Когда обновляются слои

| Слой | Обновляется | Механизм |
|---|---|---|
| STM | Всегда после каждого вызова | Дописывается пара user+assistant |
| WM | При `--memory-update` | LLM-вызов: извлечение фактов задачи |
| LTM | При `--memory-update` | LLM-вызов: извлечение профиля и знаний |

Без `--memory-update` слои WM и LTM только **читаются** (инжектируются в промпт), но не перезаписываются. Это позволяет использовать накопленную память в read-only режиме.

## Флаги

```
--query           Запрос к LLM (обязателен для чата)
--system          Системное сообщение (опционально)
--format          markdown | json (умолч. markdown)
--format-hint     Кастомная инструкция форматирования
--max-tokens      Лимит токенов ответа (0 = без лимита)
--stop            Stop-последовательность (повторяемый)
--temperature     Температура 0.0–2.0
--debug           Вывод JSON-payload в stderr
--show-tokens     Вывод разбивки токенов в stderr
--show-cost       Вывод оценки стоимости в stderr

# Layer 1 — STM
--history         Путь к файлу истории (умолч. chat_history.json; "" = отключено)
--history-limit   Макс. сообщений при --summary (умолч. 10; 0 = без лимита)
--summary         Включить авто-суммаризацию при переполнении

# Layer 2 — Working Memory
--memory-wm       Путь к файлу рабочей памяти (умолч. <history>.wm.json)

# Layer 3 — Long-term Memory
--memory-ltm      Путь к файлу долгосрочной памяти (умолч. <history>.ltm.json)

# Управление обновлением
--memory-update   Обновить WM и LTM после ответа (2 доп. LLM-вызова)

# Стратегии контекста (из Day10)
--strategy        sliding-window | sticky-facts | branching
--window-size     Размер окна для sliding-window / sticky-facts (умолч. 5)

# Управление ветками (только при --strategy branching, без --query)
--checkpoint      Сохранить checkpoint текущей ветки
--branch-new      Создать ветку и переключиться на неё
--from-checkpoint Checkpoint-источник для --branch-new
--branch-switch   Переключиться на существующую ветку
--branch-list     Показать все ветки и checkpoints
```

## Примеры использования

### Простой диалог с памятью

```bash
# Первый разговор — передаём контекст, обновляем память
./ai-adv-agent-day11 \
  --query "Меня зовут Андрей, я разрабатываю Telegram-бота на Go." \
  --history dialog.json \
  --memory-update

# Второй разговор — добавляем факты
./ai-adv-agent-day11 \
  --query "Бот должен использовать только официальную библиотеку Telegram." \
  --history dialog.json \
  --memory-update

# Новая сессия — чистый диалог, но память сохранилась
./ai-adv-agent-day11 \
  --query "Что ты знаешь о моём проекте?" \
  --history new_session.json
```

### Кастомные файлы для каждого слоя

```bash
./ai-adv-agent-day11 \
  --query "Начнём проектирование API." \
  --history      ./sessions/api.json \
  --memory-wm    ./memory/api_task.wm.json \
  --memory-ltm   ./memory/user_profile.ltm.json \
  --memory-update
```

### Только чтение памяти (без обновления)

```bash
# WM и LTM инжектируются в промпт, но не перезаписываются
./ai-adv-agent-day11 \
  --query "Напомни, какой стек мы выбрали?" \
  --history session.json \
  --memory-wm  project.wm.json \
  --memory-ltm profile.ltm.json
```

### Комбинация с контекстными стратегиями

```bash
# Sliding window + рабочая память
./ai-adv-agent-day11 \
  --query "Следующий шаг?" \
  --history h.json --strategy sliding-window --window-size 4 \
  --memory-wm task.wm.json --memory-ltm profile.ltm.json \
  --memory-update

# Branching + долгосрочная память
./ai-adv-agent-day11 \
  --query "Вариант А." \
  --history book.json --strategy branching \
  --memory-ltm author.ltm.json --memory-update
```

## Структура файлов на диске

| Файл | Слой | Когда создаётся |
|---|---|---|
| `chat_history.json` | STM (Layer 1) | Всегда (если `--history` не пустой) |
| `chat_history.wm.json` | WM (Layer 2) | При `--memory-update` |
| `chat_history.ltm.json` | LTM (Layer 3) | При `--memory-update` |
| `chat_history.stats.json` | — | При `--show-tokens` / `--show-cost` |
| `chat_history.summary.txt` | — | При `--summary` + переполнении |
| `chat_history.facts.json` | — | При `--strategy sticky-facts` |
| `chat_history.branch-state.json` | — | При `--strategy branching` |
| `chat_history.branch-<name>.json` | — | При создании ветки `<name>` |

Пути всех файлов по умолчанию выводятся из `--history`. Каждый можно переопределить явным флагом.

## Переменные окружения

| Переменная | Обязательна | Умолч. |
|---|---|---|
| `LLM_PROVIDER` | Да | — (`openai` или `openrouter`) |
| `LLM_API_KEY` | Да | — |
| `LLM_MODEL` | Нет | `openai/gpt-4o-mini` (openrouter) |
| `LLM_BASE_URL` | Нет | Стандартный URL провайдера |

## Архитектура кода (Clean Architecture)

Day11 применяет паттерн **Clean Architecture** (Чистая Архитектура). Код разделён на независимые слои с однонаправленными зависимостями: от внешнего (HTTP, файлы) к внутреннему (бизнес-логика, доменные типы).

```
┌────────────────────────────────────────────────────────────────┐
│  main.go  (Composition Root)                                   │
│  Парсинг флагов, переменных окружения, создание зависимостей   │
└───────────────────────────┬────────────────────────────────────┘
                            │ создаёт
       ┌────────────────────▼────────────────────┐
       │  internal/usecase/                      │
       │  Бизнес-логика (chat, branch, history,  │
       │  memory, summary) — зависит от портов   │
       └────────────┬───────────────────-─────── ┘
                    │ зависит от интерфейсов
       ┌────────────▼──────────────────────────-─┐
       │  internal/port/                         │
       │  Интерфейсы: LLMClient, HistoryRepo,   │
       │  StatsRepo, WMRepo, LTMRepo, BranchRepo │
       └────────────▲──────────────────────────-─┘
                    │ реализует
       ┌────────────┴──────────────────────────-─┐
       │  internal/adapter/                      │
       │  llm/client.go  — HTTP OpenAI-клиент   │
       │  storage/file.go — файловые репозитории │
       └─────────────────────────────────────────┘

       ┌─────────────────────────────────────────┐
       │  internal/domain/                       │  ← нет внешних зависимостей
       │  Чистые типы: Message, WorkingMemory,   │
       │  LongTermMemory, FactsStore, BranchState│
       │  SessionStats, ModelPrice               │
       │  + EstimateTokens, PricingFor, Context- │
       │    Status                               │
       └─────────────────────────────────────────┘
```

| Пакет | Роль | Зависит от |
|---|---|---|
| `internal/domain` | Сущности и чистые функции | ничего |
| `internal/port` | Интерфейсы входящих/исходящих портов | `domain` |
| `internal/usecase` | Оркестрация бизнес-логики | `domain`, `port` |
| `internal/adapter/llm` | HTTP-клиент для LLM API | `domain`, `port` |
| `internal/adapter/storage` | Файловые репозитории + path-хелперы | `domain`, `port` |
| `main.go` | DI-контейнер, CLI, презентация | все слои |

Благодаря этому разделению:
- **Тесты use cases** не требуют реальных HTTP-запросов или файлов — достаточно mock-репозиториев
- **Адаптеры** взаимозаменяемы: можно заменить файловое хранилище на Redis, не трогая бизнес-логику
- **Domain** полностью изолирован — все типы и функции проверяются без внешних зависимостей

## Сборка и тесты

```bash
go build -o ai-adv-agent-day11 .   # или make build
go test -v ./...                    # или make test  (тесты во всех пакетах)
go vet ./...                        # или make vet
```

Тесты распределены по пакетам:

| Пакет | Тестируемое |
|---|---|
| `internal/domain` | Ценообразование, оценка токенов, `ContextStatus` |
| `internal/usecase` | `TrimHistory`, `SlidingWindow`, system-блоки памяти, `StripJSONFences` |
| `internal/adapter/llm` | HTTP-payload (токены, stop, temperature, system message) |
| `internal/adapter/storage` | Path-хелперы, Load/Save для всех 7 типов репозиториев |

## Makefile targets

### Основные

```bash
make build        # Собрать бинарь
make test         # Юнит-тесты (без API)
make vet          # go vet
make clean        # Удалить артефакты и временные файлы памяти
```

### 3-Layer Memory (Day11)

```bash
# Одиночный вызов с памятью — WM и LTM всегда записываются
make run-memory QUERY="Меня зовут Андрей, пишу Telegram-бота на Go"
make run-memory QUERY_FILE=query.txt
make run-memory QUERY="Уточни требования" SYSTEM_FILE=system.txt

# Переопределить файлы памяти
make run-memory QUERY="Привет" \
  MEMORY_STM_FILE=./project.stm.json \
  MEMORY_WM_FILE=./project.wm.json \
  MEMORY_LTM_FILE=./project.ltm.json

# 4-ходовый demo-сценарий (запросы из project.stm.json):
# Turn 1–3: накопление WM+LTM, Turn 4: recall с чистым STM
make run-memory-demo

# Проверить recall без истории диалога (только WM+LTM)
make run-memory-recall

# Посмотреть содержимое всех 3 слоёв рядом
make run-memory-layers
```

Переменные для переопределения путей:

| Переменная | Умолч. |
|---|---|
| `MEMORY_STM_FILE` | `/tmp/memory_stm_day11.json` |
| `MEMORY_WM_FILE` | `/tmp/memory_wm_day11.json` |
| `MEMORY_LTM_FILE` | `/tmp/memory_ltm_day11.json` |

### Интеграционные запуски (из Day10)

```bash
# Одиночный запрос с настройками
make run-integration QUERY="Привет"
make run-integration QUERY_FILE=query.txt
make run-integration QUERY="Привет" STRATEGY=sliding-window WINDOW_SIZE=3
make run-integration QUERY="Привет" STRATEGY=sticky-facts
make run-integration QUERY="Привет" SUMMARY=1 HISTORY_LIMIT=6
make run-integration QUERY="Привет" SYSTEM_FILE=system.txt

# Батч: несколько Qwen-моделей → оценка экспертной моделью
make run-integration-batch QUERY="Что такое Go?"
```

### Демо стратегий контекста (из Day10)

```bash
make run-sliding-window               # последние N сообщений
make run-sliding-window WINDOW_SIZE=3
make run-sticky-facts                 # KV-факты обновляются каждый ход
make run-branching                    # checkpoint → 2 ветки → переключения
```

### История, суммаризация, токены

```bash
make run-history-all          # 4 проверки работы истории
make run-summary-test         # авто-суммаризация при переполнении
make run-critic-test          # AI-критик: полная история vs. суммари
make run-tokens               # разбивка токенов
make run-tokens-cost          # разбивка + стоимость
make run-tokens-session       # накопленная статистика за 3 хода
make run-context-overflow     # поведение при переполнении контекста
```

### Lion Book Test

Полный тест сравнения стратегий на многоходовом сценарии: **sliding-window**, **sticky-facts**, **branching** (общий ствол → ветки `dark-side` / `happy-end`) — с AI-критиком.

```bash
make run-lion-book-test
make run-lion-book-test LION_WINDOW_SIZE=3   # тесное окно
make run-lion-book-test DEBUG=1

# Результаты:
#   /tmp/lion_sw_result.txt   — sliding-window
#   /tmp/lion_sf_result.txt   — sticky-facts
#   /tmp/lion_br_result.txt   — branching
```
