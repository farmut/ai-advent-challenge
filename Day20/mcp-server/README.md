# Day 18: Petstore MCP Server

MCP (Model Context Protocol) сервер на Go, который оборачивает [Swagger Petstore API](https://petstore.swagger.io/) в 22 инструмента (tools). Поддерживает два транспорта: **stdio** (subprocess) и **HTTP+SSE** (постоянный сервис).

## Что нового в Day19

| Функция | Описание |
|---|---|
| **HTTP+SSE транспорт** | Режим постоянного сервиса; активируется флагом `-addr` |
| **Фоновый сборщик отчётов** | Собирает снимки проданных питомцев независимо от вызовов агента |
| `report_start_collection` | Запустить фоновый сбор: интервал + путь к файлу |
| `report_stop_collection` | Остановить фоновый сбор |
| `report_collection_status` | Статус сбора: запущен/остановлен, количество снимков |
| `report_show_sold` | Показать собранный отчёт из JSON-файла |
| **Авто-запуск в Makefile** | `run-agent` автоматически стартует MCP в SSE-режиме и пишет конфиг |

---

## Инструменты (22 штуки)

### Pet (7)
| Tool | Описание |
|---|---|
| `pet_add` | Добавить питомца |
| `pet_update` | Обновить питомца |
| `pet_find_by_status` | Найти по статусу (`available`, `pending`, `sold`) |
| `pet_find_by_tags` | Найти по тегам |
| `pet_get_by_id` | Получить по ID |
| `pet_update_with_form` | Обновить через form-данные |
| `pet_delete` | Удалить питомца |

### Store (4)
| Tool | Описание |
|---|---|
| `store_get_inventory` | Инвентарь магазина |
| `store_place_order` | Разместить заказ |
| `store_get_order` | Получить заказ по ID |
| `store_delete_order` | Удалить заказ |

### User (7)
| Tool | Описание |
|---|---|
| `user_create` | Создать пользователя |
| `user_create_with_list` | Создать список пользователей |
| `user_login` | Войти в систему |
| `user_logout` | Выйти из системы |
| `user_get` | Получить пользователя по имени |
| `user_update` | Обновить пользователя |
| `user_delete` | Удалить пользователя |

### Reports (4)
| Tool | Описание |
|---|---|
| `report_start_collection` | Запустить фоновый сбор снимков проданных питомцев |
| `report_stop_collection` | Остановить фоновый сбор |
| `report_collection_status` | Статус сбора (запущен/остановлен, кол-во снимков, путь к файлу) |
| `report_show_sold` | Вывести собранный отчёт из JSON-файла |

---

## Транспорты

### stdio (по умолчанию)

Сервер запускается как subprocess; MCP-клиент общается через stdin/stdout с помощью newline-delimited JSON-RPC 2.0. Подходит для агентов, которые запускают сервер на каждую сессию.

```bash
./petstore-mcp-server
```

> **Ограничение**: фоновый сборщик (`report_start_collection`) не переживёт перезапуск subprocess. Для постоянного сбора используйте HTTP+SSE режим.

### HTTP+SSE

Сервер работает как долгоживущий процесс. Клиенты подключаются по HTTP с использованием SSE-транспорта из спецификации MCP (2024-11-05).

```bash
./petstore-mcp-server -addr :8080
```

#### Эндпоинты

| Эндпоинт | Метод | Описание |
|---|---|---|
| `/sse` | GET | Открыть постоянный SSE-поток; сервер присылает `event: endpoint` с URL сессии |
| `/message?sessionId=<id>` | POST | Отправить JSON-RPC запрос; ответ приходит через SSE как `event: message` |
| `/health` | GET | Liveness probe; возвращает `{"status":"ok","active_sessions":N}` |

#### Протокол подключения

```
Клиент                               Сервер
  │                                    │
  │── GET /sse ──────────────────────► │  открыть SSE-поток
  │◄── event: endpoint ────────────── │  data: /message?sessionId=<id>
  │                                    │
  │── POST /message?sessionId=<id> ──► │  initialize
  │◄── event: message ─────────────── │  initialize response
  │                                    │
  │── POST /message?sessionId=<id> ──► │  tools/list
  │◄── event: message ─────────────── │  массив 22 инструментов
  │                                    │
  │── POST /message?sessionId=<id> ──► │  tools/call → report_start_collection
  │◄── event: message ─────────────── │  "Collection started"
  │                                    │
  │     (горутина собирает снимки по таймеру)
  │                                    │
  │── POST /message?sessionId=<id> ──► │  tools/call → report_stop_collection
  │◄── event: message ─────────────── │  "Collection stopped. 5 snapshots."
```

---

## Фоновый сборщик отчётов

Сборщик (`Collector`) — это горутина внутри процесса MCP-сервера. Работает независимо от вызовов агента, собирая данные о проданных питомцах между сессиями.

```
Агент                    MCP-сервер (HTTP+SSE)
  │                            │
  │── report_start_collection ►│── горутина: тикер каждые N секунд
  │◄── "Collection started"    │      │
  │                            │   doCollect() → GET /pet/findByStatus?status=sold
  │                            │      │         записать снимок в JSON-файл
  │                            │   doCollect() → ...
  │── report_collection_status►│
  │◄── "Running. 3 snapshots." │
  │                            │
  │── report_stop_collection ──│── close(stopCh) → горутина завершается
  │◄── "Stopped. 3 snapshots." │
  │                            │
  │── report_show_sold ────────│── читать JSON-файл → форматировать отчёт
  │◄── текст отчёта            │
```

`Start`/`Stop`/`Status` потокобезопасны: используют mutex. Каналы `stopCh`/`doneCh` обеспечивают graceful shutdown без дедлоков.

### Формат JSON-отчёта

```json
{
  "interval_seconds": 30,
  "snapshots": [
    {
      "collected_at": "2024-01-15T10:00:00Z",
      "sold_count": 3,
      "pets": [
        {"id": 1, "name": "Buddy", "status": "sold"},
        ...
      ]
    }
  ]
}
```

---

## Сборка и запуск

```bash
cd Day19/mcp-server

# Сборка
make build              # или: go build -o petstore-mcp-server .

# Тесты (сеть не нужна — используется httptest mock)
make test               # или: go test -v ./...

# Статический анализ
make vet                # или: go vet ./...

# Запуск в HTTP+SSE режиме
make run-http           # слушает на :8080

# Запуск с агентом (авто-старт MCP сервера в SSE-режиме)
make run-agent          # нужен LLM_API_KEY
```

---

## Makefile targets

| Target | Описание |
|---|---|
| `make build` | Сборка бинаря |
| `make test` | Unit-тесты (без сети, httptest mock) |
| `make vet` | Статический анализ |
| `make run-http` | Запуск в HTTP+SSE режиме (`:8080` по умолчанию) |
| `make run-http-smoke` | Smoke-тест HTTP+SSE: initialize round-trip |
| `make run-smoke` | MCP handshake + tools/list через stdio |
| `make run-live-get-pet` | Получить питомца id=1 из реального Petstore API |
| `make run-live-inventory` | Инвентарь из реального Petstore API |
| `make run-agent` | Свежая сессия: авто-старт HTTP+SSE + запуск агента (нужен `LLM_API_KEY`) |
| `make run-agent-persist` | Как `run-agent`, но история сохраняется между запусками |
| `make run-gigachat-agent` | Как `run-agent` с GigaChat провайдером |
| `make run-gigachat-persist` | Как `run-agent-persist` с GigaChat провайдером |
| `make run-gigachat-tools` | Интеграционный тест вызова инструментов через GigaChat |
| `make help` | Полный список targets |

### Поведение `run-agent`

`make run-agent` и все agent-targets:

1. Собирают бинарь MCP-сервера
2. Запускают `./petstore-mcp-server -addr :8080` в фоне
3. Health-check до готовности (до 3 с, 15 × 200 мс)
4. Записывают SSE-конфиг в `$SESSION_CFG`:
   ```yaml
   servers:
     - name: petstore
       type: sse
       url: http://localhost:8080/sse
   ```
5. Запускают агент с `--mcp-config $SESSION_CFG`
6. Убивают MCP-сервер при выходе через `trap … EXIT INT TERM`

Переопределить порт: `make run-agent HTTP_ADDR=:9090`

---

## Структура файлов

```
mcp-server/
├── main.go                  — точка входа; флаг -addr; dispatch runHTTP/runStdio
├── http_transport.go        — HTTP+SSE сервер; sessionStore; /sse /message /health
├── http_transport_test.go   — 9 тестов HTTP+SSE (health, endpoint event, initialize, tools/list и др.)
├── go.mod
├── Makefile
├── scripts/
│   ├── show_tools.sh
│   └── show_result.sh
└── petstore/
    ├── client.go            — HTTP-клиент Petstore API; NewClientWithBase для тестов
    ├── tools.go             — 22 определения инструментов; package-level CallTool
    ├── report.go            — CollectSoldReport, ShowSoldReport, типы SoldReport/PetSnapshot
    ├── collector.go         — Collector: фоновая горутина; Start / Stop / Status
    ├── handler.go           — Handler: объединяет Client + Collector; маршрутизирует вызовы
    └── tools_test.go        — unit-тесты инструментов и Handler/Collector
```

### Зависимости пакетов

```
main.go
  ├── http_transport.go  (runHTTP, sessionStore)
  │     └── petstore.Handler
  └── runStdio
        └── petstore.Handler

petstore/
  ├── handler.go  (Handler)
  │     ├── collector.go  (Collector — фоновая горутина)
  │     └── client.go + tools.go  (CallTool — все не-collector инструменты)
  └── report.go  (CollectSoldReport, ShowSoldReport)
```

---

## Тестирование вручную (stdio)

```bash
# Список инструментов
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
| ./petstore-mcp-server 2>/dev/null

# Вызов инструмента
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"store_get_inventory","arguments":{}}}' \
| ./petstore-mcp-server 2>/dev/null
```

---

## Переменные окружения

| Переменная | Обязательна | По умолчанию |
|---|---|---|
| `LLM_PROVIDER` | Да (для агента) | — (`openai` или `openrouter`) |
| `LLM_API_KEY` | Да (для агента) | — |
| `LLM_MODEL` | Нет | `openai/gpt-4o-mini` |
| `LLM_BASE_URL` | Нет | URL провайдера по умолчанию |

MCP-сервер не требует переменных окружения. Вся конфигурация — через CLI-флаги.
