# Документация AI Advent Challenge

Собранная по-дневная документация проекта — источник для загрузки в RAG.

Каждый день — независимый Go-модуль, расширяющий предыдущий одной возможностью. Ниже — карта. Дни-чекпоинты (без изменений кода) помечены и ссылаются на предыдущий.

| День | Тема |
|---|---|
| [Day 1](README_Day01.md) | Базовый LLM-агент (go-openai + glamour) |
| [Day 2](README_Day02.md) | Собственный HTTP-клиент, флаги вывода |
| [Day 3](README_Day03.md) | Системная роль (`--system`) |
| [Day 4](README_Day04.md) | Температура (`--temperature`) |
| [Day 5](README_Day05.md) | Динамический выбор модели |
| [Day 6](README_Day06.md) | Персистентная история (`--history`) |
| [Day 7](README_Day07.md) | Чекпоинт (= Day 6) |
| [Day 8](README_Day08.md) | Токены и стоимость |
| [Day 9](README_Day09.md) | Лимит истории + авто-суммаризация |
| [Day 10](README_Day10.md) | Стратегии контекста (`--strategy`) |
| [Day 11](README_Day11.md) | 3-слойная память + Clean Architecture |
| [Day 12](README_Day12.md) | Профиль пользователя |
| [Day 13](README_Day13.md) | Интерактивный режим + FSM |
| [Day 14](README_Day14.md) | Инварианты |
| [Day 15](README_Day15.md) | Review-промпты, PendingPlan, CoT-compliance |
| [Day 16](README_Day16.md) | MCP-клиент |
| [Day 17](README_Day17.md) | Petstore MCP-сервер (18 инструментов) |
| [Day 18](README_Day18.md) | HTTP+SSE + фоновый сборщик (22 инструмента) |
| [Day 19](README_Day19.md) | Чекпоинт-рефакторинг сервера |
| [Day 20](README_Day20.md) | Multi-MCP + GigaChat |
| [Day 21](README_Day21.md) | Standalone RAG-конвейер (`rag/`) |
| [Day 22](README_Day22.md) | RAG в агенте |
| [Day 23](README_Day23.md) | RAG + реранкер |
| [Day 24](README_Day24.md) | Структурированный обоснованный ответ |
| [Day 25](README_Day25.md) | Диалоговая память RAG-сеанса |
| [Day 26](README_Day26.md) | Чекпоинт (= Day 25) |
| [Day 27](README_Day27.md) | Чекпоинт (= Day 26) |
| [Day 28](README_Day28.md) | Полностью локальный RAG |
| [Day 29](README_Day29.md) | Model-init дефолты генерации |
| [Day 30](README_Day30.md) | Self-signed HTTPS (LiteLLM) |
| [Day 31](README_Day31.md) | Конфиг-ориентированный оркестратор |

## Архитектура

[docs/architecture](architecture/README.md) — архитектура проекта на срезе Day31 (Clean Architecture, оркестратор, Toolbelt, память shared/ephemeral, config-контракт, TUI).

## Руководство пользователя

[docs/usage](usage/README.md) — как использовать агенты и компоненты: запуск оркестратора, настройка саб-агентов, RAG, MCP, память, TUI и slash-команды, типовые сценарии, диагностика.
