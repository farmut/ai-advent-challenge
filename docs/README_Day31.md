# Day 31: конфиг-ориентированный оркестратор саб-агентов

Разрозненные флаги одиночного агента рефакторятся в **единый оркестратор, управляемый одним YAML-конфигом**. Главный процесс не решает задачу сам — ведёт LLM-цикл планирования, спаунит саб-агентов из ростера и маршрутизирует их результаты, пока не сможет ответить.

```
пользователь → [оркестратор: цикл, ≤ max_rounds]
                 spawn    → запустить саб-агента (task + context)
                 ask_user → согласовать план (human-in-the-loop)
                 finish   → итоговый ответ
                 ▼
             саб-агенты (researcher / coder / reviewer …) → результат в транскрипт
```

## Что нового
- **`internal/config`** — YAML-документ (`agent.config.yaml`): каждый слой памяти включён по умолчанию (выключается per-layer), RAG/MCP opt-in. Приоритет defaults → YAML → явные CLI-флаги (`config.Load` / `Default` / `ResolveEnv` / `Overrides` / `Validate`).
- **`internal/app`** — композиционный корень (держит `usecase` свободным от адаптеров): `Toolbelt` (общий LLM, RAG-пайплайн, пул MCP-инструментов + tool→server-маршрутизация + per-role `ToolExecutor`/`MCPToolsFor` по allow-list, `"*"` = все серверы); `MemoryFactory` (**shared** файловая память для диалога оркестратора vs **ephemeral** in-memory STM+WM саб-агентов + read-only shared LTM/профиль).
- **`Orchestrator`** — цикл планирования, JSON-экшены `spawn` / `ask_user` / `finish`, спаун `SubAgent` из ростера, стоп по finish или `max_rounds`; `ask_user` — human-in-the-loop через `UserPrompter` (согласовать/прокомментировать/вернуть на доработку); инжектит profile/LTM/WM/task-memory + подгружает историю STM; персистит каждый ход.
- **`SubAgent`** — тот же движок (`ChatUseCase.ExecuteWithTools` + опциональный RAG через `RAGUseCase.BuildPrompt`) на эфемерной памяти, доступ к инструментам по роли.
- `main.go`: `--config` (env `AGENT_CONFIG`) включает оркестратор (once для `--query`, REPL для `--interactive`); легаси-пути одиночного агента без конфига не тронуты.
- **Slash-команды** (`commands.go`, двуязычные): `/help`, `/agents`, `/memory`, `/mcp`, `/tools`, `/copy`, `/select`, `/clear`, `/exit`.
- **Полноэкранный TUI** (`tui.go`, gotui v5): шапка + прокручиваемый лог + многострочный ввод; задача в фоне, прогресс стримится в лог. Ввод: **Enter — новая строка, Ctrl+S — отправка (/keys — диагностика нажатий)**; ↑/↓ — курсор/история. **Тачпад-прокрутка лога работает по умолчанию** (мышь захвачена); выделение — Option+drag (iTerm2) / Fn+drag (Terminal.app), либо `/select` — нативное выделение без модификаторов. `/copy` — копия ответа в буфер (pbcopy/wl-copy/xclip/xsel + OSC 52). Fallback на строчный REPL при `--no-tui` / не-TTY.
- **Рендеринг** (`tui_render.go`): цветные теги стадий, markdown-lite (заголовки/списки/`**bold**`/`` `code` ``/правила), фенсы verbatim за гаттером; разметка защищена от порчи кодом.
- Makefile слим (2799 → ~590 строк): build/test/vet/clean + RAG-набор + MCP-config-тесты + telegram-opt + `run-orchestrator` / `run-orchestrator-interactive` / `run-orchestrator-rag` / `run-orchestrator-local`.
- Юнит-тесты: контракт config merge/override/validate, изоляция ephemeral vs shared памяти, цикл spawn→route→finish на mock-LLM, guard валидности shipped-конфига.
- **`git-mcp-server/`** — четвёртый компонент: read-only git MCP-сервер (stdio). Инструменты `git_current_branch` / `git_list_files` (changed/staged/untracked/all) / `git_diff` (staged, path); репозиторий фиксируется при старте, `exec.Command` без shell, вывод ≤ 64 КБ. В shipped-конфиге включён и выдан ролям `researcher` и `reviewer`.
- **Режим консультанта по документации** — `/help` в интерактиве переключает сессию в заземлённый Q&A по индексу документации (`consultant:` в конфиге: свой RAG-пайплайн на `../rag/docs.db` + MCP allow-list `["git"]`); `/end` возвращает оркестратор. Консультант строится лениво при входе, история режима эфемерна (сырые Q/A, окно 12), заземляющий промпт tool-friendly — вопросы о ветке/файлах/diff уходят в git-инструменты.

## Запуск
```bash
cd Day31/agent && make build
export LLM_PROVIDER=openrouter LLM_API_KEY=sk-... LLM_MODEL=openai/gpt-4o-mini
make run-orchestrator AGENT_TASK="Проанализируй репозиторий и предложи улучшения"
make run-orchestrator-interactive
```

Подробности — [`Day31/README.md`](../Day31/README.md), [`Day31/agent/README.md`](../Day31/agent/README.md), архитектура — [docs/architecture](architecture/README.md).
