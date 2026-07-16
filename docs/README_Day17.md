# Day 17: собственный Petstore MCP-сервер

Появляется свой MCP-сервер — день становится двухкомпонентным workspace.

## Что нового
- Go-сервер `mcp-server/`, оборачивающий публичный [Swagger Petstore](https://petstore.swagger.io/) REST API в **18 MCP-инструментов** (`pet_*`, `store_*`, `user_*`), транспорт **stdio**.
- Структура дня: `agent/` (MCP-клиент из Day16) + `mcp-server/`.

## Далее
Day18 добавляет серверу HTTP+SSE-транспорт и фоновый сборщик отчётов (22 инструмента).
