# git-mcp-server — MCP-сервер для локального git

Минимальный MCP-сервер (stdio, JSON-RPC 2.0), дающий агенту **read-only** доступ к локальному git-репозиторию. Мутирующих операций нет — сервер не может изменить репозиторий.

## Инструменты

| Инструмент | Аргументы | Что возвращает |
|---|---|---|
| `git_current_branch` | — | Имя текущей ветки (при detached HEAD — хеш коммита) |
| `git_list_files` | `filter`: `changed` (дефолт) \| `staged` \| `untracked` \| `all` | Список файлов: изменённые со статус-кодами / staged / неотслеживаемые / все отслеживаемые |
| `git_diff` | `staged`: bool, `path`: string (опц.) | Diff незастейдженных (или `--cached` при `staged=true`) изменений, опционально по одному пути |

Вывод инструментов ограничен 64 КБ (огромный diff обрезается с пометкой) — защита контекста LLM.

## Запуск

```bash
make build
./git-mcp-server                # репозиторий = текущая директория
./git-mcp-server -repo /path    # явный путь (валидируется при старте)
```

Репозиторий фиксируется при старте (`git rev-parse --show-toplevel`); аргументы инструментов сменить его не могут. Команды git выполняются через `exec.Command` (argv, без shell) — инъекции через аргументы невозможны; `path`, похожий на опцию (`-…`), отклоняется.

## Подключение к агенту (Day31)

В `agent/agent.config.yaml`:

```yaml
mcp:
  enabled: true
  servers:
    - name: git
      type: stdio
      command: ../git-mcp-server/git-mcp-server
      args: ["-repo", "."]

orchestrator:
  subagents:
    - name: reviewer
      mcp: ["git"]     # выдать роли доступ к git-инструментам
```

Проверка в интерактиве: `/mcp` (серверы), `/tools` (инструменты).

## Тесты

```bash
make test    # юнит-тесты на временном git-репозитории (init → commit → изменения)
make vet
```
