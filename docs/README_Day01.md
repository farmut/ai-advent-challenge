# Day 1: базовый LLM-агент

Первый шаг challenge — минимальный CLI, который отправляет запрос в LLM API и печатает ответ.

## Что нового
- Запрос к LLM через SDK `go-openai` (`sashabaranov/go-openai`).
- Рендер ответа в Markdown терминала через `charmbracelet/glamour`.
- Читает конфиг из окружения (`LLM_PROVIDER`, `LLM_API_KEY`, `LLM_MODEL`, `LLM_BASE_URL`).

## Запуск
```bash
cd Day1 && make build
LLM_PROVIDER=openai LLM_API_KEY=sk-... ./ai-adv-agent-day1 --query "Привет"
```

## Далее
Day2 переписывает клиент на чистый `net/http` без SDK.
