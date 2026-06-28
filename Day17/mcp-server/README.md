# Day 17 — Petstore MCP Server

MCP-сервер на Go, который оборачивает публичное REST API [Swagger Petstore](https://petstore.swagger.io/) в 18 инструментов (tools) Model Context Protocol.

## Инструменты (18 шт.)

### Pet
| Tool | Метод | Эндпоинт |
|------|-------|-----------|
| `pet_add` | POST | `/pet` |
| `pet_update` | PUT | `/pet` |
| `pet_find_by_status` | GET | `/pet/findByStatus` |
| `pet_find_by_tags` | GET | `/pet/findByTags` |
| `pet_get_by_id` | GET | `/pet/{petId}` |
| `pet_update_with_form` | POST | `/pet/{petId}` |
| `pet_delete` | DELETE | `/pet/{petId}` |

### Store
| Tool | Метод | Эндпоинт |
|------|-------|-----------|
| `store_get_inventory` | GET | `/store/inventory` |
| `store_place_order` | POST | `/store/order` |
| `store_get_order` | GET | `/store/order/{orderId}` |
| `store_delete_order` | DELETE | `/store/order/{orderId}` |

### User
| Tool | Метод | Эндпоинт |
|------|-------|-----------|
| `user_create` | POST | `/user` |
| `user_create_with_list` | POST | `/user/createWithList` |
| `user_login` | GET | `/user/login` |
| `user_logout` | GET | `/user/logout` |
| `user_get` | GET | `/user/{username}` |
| `user_update` | PUT | `/user/{username}` |
| `user_delete` | DELETE | `/user/{username}` |

## Сборка

```bash
cd Day17
make build          # собрать бинарь ./petstore-mcp-server
make test           # запустить unit-тесты (httptest, без сети)
make run-smoke      # smoke-тест протокола MCP (без сети)
make run-live-inventory   # живой запрос к реальному Petstore API
make run-live-get-pet     # получить питомца id=1
```

---

## Подключение MCP-сервера к агенту (Day16+)

### Шаг 1. Собрать бинарь

```bash
cd Day17
make build
# результат: ./petstore-mcp-server
```

### Шаг 2. Добавить сервер в конфиг агента

Агент (начиная с Day16) хранит конфигурацию MCP-серверов в YAML-файле.
Добавьте сервер командой `--mcp-add`:

```bash
cd Day16   # или более поздний день

./ai-adv-agent-day16 \
  --mcp-config /tmp/mcp_petstore.yaml \
  --mcp-add \
  --mcp-name petstore \
  --mcp-type stdio \
  --mcp-command /абсолютный/путь/до/Day17/petstore-mcp-server
```

Или отредактируйте YAML-файл вручную:

```yaml
# /tmp/mcp_petstore.yaml
servers:
  - name: petstore
    type: stdio
    command: /абсолютный/путь/до/Day17/petstore-mcp-server
```

### Шаг 3. Проверить список инструментов

```bash
./ai-adv-agent-day16 \
  --mcp-config /tmp/mcp_petstore.yaml \
  --mcp-tools
```

Ожидаемый вывод: 18 инструментов (pet_add, pet_get_by_id, store_get_inventory, …).

### Шаг 4. Использовать инструменты в интерактивном режиме

```bash
./ai-adv-agent-day16 \
  --mcp-config /tmp/mcp_petstore.yaml \
  --interactive \
  --history /tmp/petstore_session.json \
  --profile /tmp/my_profile.md \
  --show-cost
```

Теперь агент будет автоматически вызывать инструменты Petstore, когда LLM решит воспользоваться ими. Например:

```
Task> Find all available pets and create an order for pet id 1
```

### Быстрая проверка вручную (без агента)

Протокол — newline-delimited JSON-RPC 2.0 через stdin/stdout.
Можно тестировать напрямую:

```bash
# Получить список инструментов
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
| ./petstore-mcp-server

# Вызвать инструмент
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"store_get_inventory","arguments":{}}}' \
| ./petstore-mcp-server
```

---

## Архитектура

```
Day17/
  main.go                  — MCP-сервер: stdin/stdout JSON-RPC 2.0 loop
  petstore/
    client.go              — HTTP-клиент для Petstore REST API
    tools.go               — определения инструментов + dispatch
    tools_test.go          — unit-тесты (httptest mock)
  scripts/
    show_tools.py / .sh    — вспомогательный парсер для Makefile
    show_result.py / .sh   — вспомогательный парсер для Makefile
  go.mod
  Makefile
```

### Протокол MCP (stdio)

```
client → stdin  → [MCP server] → stdout → client
                      ↓
                   stderr (диагностика)
```

Каждое сообщение — одна строка JSON (newline-delimited).

Последовательность при старте:
1. `initialize` → сервер отвечает capabilities
2. `notifications/initialized` → без ответа (уведомление)
3. `tools/list` → список 18 инструментов
4. `tools/call` × N → вызов конкретного инструмента

Ответы `tools/call` имеют формат:
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [{"type": "text", "text": "...JSON от Petstore API..."}],
    "isError": false
  }
}
```
