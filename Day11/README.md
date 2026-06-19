# Day 10: Context Management Strategies

Расширение Day9 с тремя стратегиями управления контекстом диалога: **Sliding Window**, **Sticky Facts** и **Branching** (ветки диалога с checkpoints).

## Что нового в Day10

| Возможность | День появления |
|---|---|
| Базовый HTTP-клиент, `--format`, `--debug` | Day2 |
| `--system` (системная роль) | Day3 |
| `--temperature` | Day4 |
| История диалога (`--history`), `--show-cost` | Day9 |
| `--summary` (авто-суммаризация, теперь **опциональна**) | Day9 → Day10 |
| `--strategy sliding-window` | **Day10** |
| `--strategy sticky-facts` | **Day10** |
| `--strategy branching` + checkpoints | **Day10** |

## Флаги

```
--query           Запрос к LLM (обязателен для чата)
--system          Системное сообщение (опционально)
--format          markdown | json (умолч. markdown)
--format-hint     Кастомная инструкция форматирования
--max-tokens      Лимит токенов ответа (0 = без лимита)
--stop            Stop-последовательность (можно указывать несколько)
--temperature     Температура 0.0–2.0 (умолч. — провайдер по умолчанию)
--debug           Вывод JSON-payload и usage в stderr
--show-tokens     Вывод разбивки токенов в stderr
--show-cost       Вывод оценки стоимости в stderr (включает --show-tokens)

--history         Путь к файлу истории (умолч. chat_history.json; "" = отключено)
--history-limit   Макс. сообщений при --summary (умолч. 10; 0 = без лимита)
--summary         Включить авто-суммаризацию при переполнении (умолч. выключена)

--strategy        Стратегия управления контекстом: sliding-window | sticky-facts | branching
--window-size     Кол-во последних сообщений в промпте для sliding-window / sticky-facts (умолч. 5)

# Команды управления ветками (только при --strategy branching, без --query):
--checkpoint      Сохранить текущую историю ветки как именованный checkpoint
--branch-new      Создать новую ветку и переключиться на неё
--from-checkpoint Checkpoint-источник для --branch-new
--branch-switch   Переключиться на существующую ветку
--branch-list     Показать все ветки и checkpoints
```

## Стратегии управления контекстом

По умолчанию ни одна стратегия не активна. Выбрать можно только одну.

### Без стратегии (умолч.)

История растёт до `--history-limit` сообщений. При достижении лимита:
- старые сообщения обрезаются
- если указан `--summary` — перед обрезкой LLM строит суммари и сохраняет его как системный блок

### `--strategy sliding-window`

Хранит только последние `--window-size` сообщений. Старые **отбрасываются без суммаризации**.

```
[msg1][msg2][msg3][msg4][msg5]  →  [msg3][msg4][msg5]  (window=3)
```

Суммаризация (`--summary`) при этой стратегии не применяется.

Файлы: только `chat_history.json`.

### `--strategy sticky-facts`

После каждого хода запускает дополнительный LLM-вызов, который обновляет **KV-хранилище фактов** (JSON-словарь). В следующем ходу факты инжектируются в системный блок `[Sticky Facts]` перед историей.

```
[Sticky Facts]
имя_героя = Симба
место_действия = саванна
количество_друзей = 2
---
[последние N сообщений]
```

Файлы: `chat_history.json` + `chat_history.facts.json`.

### `--strategy branching`

Независимые ветки диалога. Каждая ветка хранит свою историю в отдельном файле. По умолчанию активна ветка `main`.

**Команды управления** (без `--query`):

```bash
# Сохранить checkpoint
./ai-adv-agent-day10 --history h.json --strategy branching --checkpoint "after-ch3"

# Создать ветку от checkpoint
./ai-adv-agent-day10 --history h.json --strategy branching \
  --branch-new "dark-side" --from-checkpoint "after-ch3"

# Создать ветку от текущего состояния
./ai-adv-agent-day10 --history h.json --strategy branching \
  --branch-new "experiment"

# Переключиться на ветку
./ai-adv-agent-day10 --history h.json --strategy branching \
  --branch-switch "dark-side"

# Список веток и checkpoints
./ai-adv-agent-day10 --history h.json --strategy branching --branch-list
```

Файлы:
- `chat_history.json` — история ветки `main`
- `chat_history.branch-<name>.json` — история ветки `<name>`
- `chat_history.branch-state.json` — текущая ветка, список веток, checkpoints

## Суммаризация (`--summary`)

В Day9 суммаризация была включена по умолчанию. В Day10 она **выключена по умолчанию** и требует явного флага `--summary`.

Работает только с режимом без стратегии или с `branching`. При `sliding-window` и `sticky-facts` игнорируется.

## Примеры использования

```bash
# Простой диалог с историей
./ai-adv-agent-day10 --query "Привет!" --history dialog.json

# Sliding window — помним только последние 3 сообщения
./ai-adv-agent-day10 --query "Что было раньше?" \
  --history sw.json --strategy sliding-window --window-size 3

# Sticky facts — KV-память между ходами
./ai-adv-agent-day10 --query "Зовут меня Алексей, я программист." \
  --history sf.json --strategy sticky-facts

# Branching — разветвление сюжета
./ai-adv-agent-day10 --query "Напиши первую главу книги." \
  --history book.json --strategy branching --history-limit 0

./ai-adv-agent-day10 --history book.json --strategy branching \
  --checkpoint "после-главы-1"

./ai-adv-agent-day10 --history book.json --strategy branching \
  --branch-new "трагичный-финал" --from-checkpoint "после-главы-1"

./ai-adv-agent-day10 --query "Напиши трагичную вторую главу." \
  --history book.json --strategy branching --history-limit 0

./ai-adv-agent-day10 --history book.json --strategy branching \
  --branch-switch "main"

./ai-adv-agent-day10 --query "Напиши счастливую вторую главу." \
  --history book.json --strategy branching --history-limit 0

# С суммаризацией (опционально)
./ai-adv-agent-day10 --query "Продолжи" \
  --history long.json --history-limit 10 --summary
```

## Структура файлов на диске

| Файл | Когда создаётся |
|---|---|
| `chat_history.json` | Всегда (если `--history` не пустой) |
| `chat_history.stats.json` | При `--show-tokens` / `--show-cost` |
| `chat_history.summary.txt` | При `--summary` + переполнении |
| `chat_history.facts.json` | При `--strategy sticky-facts` |
| `chat_history.branch-state.json` | При `--strategy branching` |
| `chat_history.branch-<name>.json` | При создании ветки `<name>` |

## Переменные окружения

| Переменная | Обязательна | Умолч. |
|---|---|---|
| `LLM_PROVIDER` | Да | — (`openai` или `openrouter`) |
| `LLM_API_KEY` | Да | — |
| `LLM_MODEL` | Нет | `openai/gpt-4o-mini` (openrouter) |
| `LLM_BASE_URL` | Нет | Стандартный URL провайдера |

## Сборка и тесты

```bash
go build -o ai-adv-agent-day10 .   # или make build
go test -v ./...                    # или make test
go vet ./...                        # или make vet
```

## Makefile targets

### Основные

```bash
make build                    # Собрать бинарь
make test                     # Юнит-тесты (без API)
make vet                      # go vet
make clean                    # Удалить бинарь
```

### Интеграционные запуски

```bash
# Одиночный запрос с настройками
make run-integration QUERY="Привет"
make run-integration QUERY_FILE=query.txt
make run-integration QUERY="Привет" STRATEGY=sliding-window WINDOW_SIZE=3
make run-integration QUERY="Привет" STRATEGY=sticky-facts
make run-integration QUERY="Привет" SUMMARY=1 HISTORY_LIMIT=6
make run-integration QUERY="Привет" SYSTEM_FILE=system.txt HISTORY_FILE=my.json

# Батч: несколько Qwen-моделей → оценка экспертной моделью
make run-integration-batch QUERY="Что такое Go?"
```

### Демо стратегий

```bash
# Sliding window: 6 ходов, хранит последние WINDOW_SIZE сообщений
make run-sliding-window
make run-sliding-window WINDOW_SIZE=3   # тесный контекст

# Sticky facts: 5 ходов, KV-память обновляется каждый ход
make run-sticky-facts

# Branching: checkpoint → 2 ветки → переключения → branch-list
make run-branching
```

### История, суммаризация, токены

```bash
make run-history-all          # 4 проверки: создание, накопление, отключение, контекст
make run-summary-test         # авто-суммаризация при переполнении
make run-critic-test          # AI-критик сравнивает полную историю и суммари
make run-tokens               # разбивка токенов одного вызова
make run-tokens-cost          # разбивка + оценка стоимости
make run-tokens-session       # накопленная статистика за 3 хода
make run-context-overflow     # демо поведения при переполнении контекста
```

### Lion Book Test

Полный тест сравнения стратегий на многоходовом сценарии:

- **sliding-window** и **sticky-facts**: 10 ходов (9 глав + вопрос о 2-й главе)
- **branching**: общий ствол (главы 1–3, 2 checkpoint) → 2 ветки с разным тоном (`dark-side` / `happy-end`) → переключения → финальная глава в каждой ветке

Результаты всех трёх стратегий передаются AI-критику (`openai/gpt-oss-120b:free`), который сравнивает их по критериям: качество, стабильность контекста, расход токенов, удобство.

```bash
make run-lion-book-test
make run-lion-book-test LION_WINDOW_SIZE=3   # тесное окно для контраста
make run-lion-book-test DEBUG=1              # с полными JSON-payload

# Результаты записываются в:
#   /tmp/lion_sw_result.txt   — sliding-window
#   /tmp/lion_sf_result.txt   — sticky-facts
#   /tmp/lion_br_result.txt   — branching
```
