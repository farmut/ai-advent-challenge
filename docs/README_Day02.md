# Day 2: собственный HTTP-клиент и флаги вывода

Клиент переписан с SDK на **сырой `net/http`** — полный контроль над JSON-запросом к OpenAI-совместимому эндпоинту.

## Что нового
- Ручная сборка запроса `/chat/completions` (без `go-openai`).
- Флаги: `--format` (markdown | json), `--format-hint` (кастомная инструкция форматирования), `--max-tokens`, `--stop` (stop-последовательность), `--debug` (вывод JSON-payload в stderr).

## Запуск
```bash
cd Day2 && make build
./ai-adv-agent-day2 --query "..." --format json --max-tokens 500 --debug
```

## Далее
Day3 добавляет системную роль (`--system`).
