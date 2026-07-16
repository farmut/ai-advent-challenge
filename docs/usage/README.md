# Руководство пользователя: агенты и компоненты (Day31)

Практический гайд: как запускать оркестратор, настраивать саб-агентов, подключать RAG и MCP, работать с памятью и TUI. Архитектура — в [docs/architecture](../architecture/README.md).

## Быстрый старт

```bash
cd Day31/agent
make build                       # бинарник ai-adv-agent-agent

export LLM_PROVIDER=openrouter   # openai | openrouter | gigachat
export LLM_API_KEY=sk-...
export LLM_MODEL=openai/gpt-4o-mini

# Разовая задача через оркестратор
./ai-adv-agent-agent --config agent.config.yaml --query "Проанализируй репозиторий"

# Интерактивная сессия (полноэкранный TUI)
./ai-adv-agent-agent --config agent.config.yaml --interactive
```

Или через Makefile:

```bash
make run-orchestrator AGENT_TASK="Опиши архитектуру проекта"
make run-orchestrator-interactive
make run-orchestrator-rag        # + RAG-флаги поверх конфига
make run-orchestrator-local      # локальный LLM (LM Studio / Ollama)
```

Секреты — только в окружении (`LLM_API_KEY`), в YAML их не кладём.

## Режимы запуска

| Команда | Режим |
|---|---|
| `--config … --query "…"` | Одна задача, ответ в stdout, выход |
| `--config … --interactive` | Полноэкранный TUI, диалог с историей |
| `--config … --interactive --no-tui` | Строчный REPL (без TUI; также автоматически при не-TTY) |
| без `--config` | Легаси-пути одиночного агента Day2–Day30 (все старые флаги работают) |

`--debug` печатает payload запросов и токены в stderr. Путь к конфигу можно задать через env `AGENT_CONFIG`.

## Как работает оркестратор

Главный процесс сам задачу не решает. Каждый раунд LLM-планировщик выдаёт одно действие:

- **spawn** — запустить саб-агента из ростера с задачей и контекстом (результаты прошлых агентов передаются дальше — так строится конвейер researcher → coder → reviewer);
- **ask_user** — пауза, показать план и вопрос; вы отвечаете: одобряете, комментируете или отправляете на доработку. В `--query`-режиме без интерактива агент продолжает автономно;
- **finish** — итоговый ответ.

Максимум раундов — `orchestrator.max_rounds` (дефолт 8). Прогресс (`[orchestrator]`, `[subagent …]`) стримится в лог по мере работы.

## Настройка саб-агентов

Ростер — секция `orchestrator.subagents` в `agent.config.yaml`. Новый агент = новая запись, код не трогается:

```yaml
orchestrator:
  enabled: true
  max_rounds: 8
  subagents:
    - name: researcher
      prompt: >
        Ты аналитик-исследователь. Собираешь факты и контекст по задаче.
      rag: true            # доступ к RAG (требует rag.enabled: true)
      mcp: []              # MCP-серверы: [] — нет, ["petstore"] — один, ["*"] — все
      memory: { wm: true } # эфемерная working memory саб-агента
```

Поля роли:

| Поле | Значение |
|---|---|
| `name` | Имя, которым оркестратор ссылается на агента в spawn |
| `prompt` | Системный промпт роли |
| `rag` | `true` — вход агента заземляется на RAG-контекст перед LLM-вызовом |
| `mcp` | Allow-list MCP-серверов; инструменты вне списка агенту недоступны |
| `memory.wm` | Включить эфемерную WM (in-memory, умирает с агентом) |

Саб-агент получает **эфемерную** память: свою STM (засеянную контекстом от оркестратора) и WM, но **read-only** доступ к общим LTM и профилю — он никогда не затирает память сессии.

Дефолтный ростер: `researcher` (RAG), `coder`, `reviewer`. Хорошая практика — узкие роли с явным контрактом выхода в промпте («возвращаешь вердикт и список правок»).

## Приоритет настроек

**defaults → YAML → явные CLI-флаги.** Пропущенная секция в YAML = встроенный дефолт (все слои памяти включены, RAG/MCP выключены). Флаг побеждает конфиг только если реально введён — пустой флаг ничего не затирает. Пустые LLM-поля добираются из `LLM_PROVIDER` / `LLM_MODEL` / `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_CA_CERT`.

## Память

Все слои включены по умолчанию; пути выводятся из `memory.dir`:

| Слой | Файл | Что хранит | Выключить |
|---|---|---|---|
| STM | `chat_history.json` | Историю диалога (окно `stm.limit`, дефолт 10) | `stm: { enabled: false }` |
| WM | `*.wm.json` | Факты текущей задачи | `wm: { enabled: false }` |
| LTM | `*.ltm.json` | Стабильные предпочтения, решения | `ltm: { enabled: false }` |
| Facts | `*.facts.json` | KV-факты | `facts: { enabled: false }` |
| Профиль | `*.profile.md` | Профиль пользователя (Markdown) | `profile: { enabled: false }` |
| Память задачи | `*.taskmem.json` | Цель / уточнено / ограничения | `task_memory: { enabled: false }` |

- История **переживает перезапуск**: оркестратор подгружает окно STM в планирующий транскрипт, поэтому «продолжи, что делал» работает между запусками.
- `auto_update: true` — после каждого хода дополнительный LLM-вызов обновляет WM и LTM (дороже, но память живёт сама).
- `/memory` в интерактиве показывает текущее состояние (задача / WM / LTM).

## RAG

Включение — в конфиге:

```yaml
rag:
  enabled: true
  db: rag.db                     # индекс, построенный компонентом rag/
  embed_url: http://localhost:11434
  embed_model: nomic-embed-text
  top_k: 20                      # пул retrieval до фильтрации
  threshold: 0.5                 # отсечка по score
  top_k_final: 10                # сколько чанков идёт в промпт
  rerank:
    enabled: true
    model: cohere/rerank-4-fast  # пусто → основная LLM-модель
    mode: auto                   # api | chat | auto (auto: имя содержит "rerank" → api)
```

**Важно:** модель эмбеддингов запроса обязана совпадать с той, которой строился индекс — иначе поиск вернёт мусор без ошибки.

Индекс строится компонентом `rag/`:

```bash
cd Day31/rag
make build
./rag index --input ../lib.txt --db rag.db     # индексация корпуса
./rag search --db rag.db --query "..."          # проверка поиска
./rag stats --db rag.db                          # статистика индекса
```

### Поиск по документации проекта

Каталог `docs/` проиндексирован в `Day31/rag/docs.db` (structural chunking, эмбеддинги `text-embedding-nomic-embed-text-v2-moe` @ LM Studio). Переиндексация после правок документации:

```bash
cd Day31/rag
make run-index-docs EMBED_URL=http://127.0.0.1:1234 EMBED_MODEL=text-embedding-nomic-embed-text-v2-moe
```

Поиск из CLI:

```bash
./rag search --db docs.db --query "как добавить саб-агента" \
  --embed-url http://127.0.0.1:1234 --embed-model text-embedding-nomic-embed-text-v2-moe
```

Обоснованный ответ через агента (источники + цитаты):

```bash
cd Day31/agent
LLM_PROVIDER=openai LLM_BASE_URL=http://127.0.0.1:1234/v1 \
LLM_MODEL=google/gemma-4-26b-a4b LLM_API_KEY=local \
./ai-adv-agent-agent --rag --rag-db ../rag/docs.db \
  --rag-embed-url http://127.0.0.1:1234 \
  --rag-embed-model text-embedding-nomic-embed-text-v2-moe \
  --query "Как добавить нового саб-агента в оркестратор?"
```

Модель эмбеддингов при поиске обязана совпадать с той, что при индексации — она «вшита» в БД.

Пайплайн в агенте: retrieve (top_k) → rerank (опц.) → фильтр по threshold → top_k_final чанков в промпт. Если реранкер упал — деградация на similarity-порядок с заметкой `[rag] rerank failed, falling back…`, run не роняется. Если ни один чанк не прошёл порог — честное «Не знаю» без LLM-вызова.

## MCP

Инструменты внешних серверов:

```yaml
mcp:
  enabled: true
  servers:
    - { name: git, type: stdio, command: ../git-mcp-server/git-mcp-server, args: ["-repo", "."] }
    - { name: petstore, type: sse, url: "http://127.0.0.1:8931/sse" }
  # либо готовый YAML: file: chat_history.mcp.yaml
```

Затем раздать доступ ролям через их `mcp`-список (`[]` — нет доступа, `["git"]` — один сервер, `["*"]` — все).

### git MCP-сервер (в комплекте, включён в примере конфига)

`Day31/git-mcp-server/` — read-only доступ к локальному git (stdio, репозиторий фиксируется при старте, мутирующих операций нет):

| Инструмент | Что даёт |
|---|---|
| `git_current_branch` | Имя текущей ветки (detached HEAD — хеш коммита) |
| `git_list_files` | Файлы: `changed` (дефолт) / `staged` / `untracked` / `all` |
| `git_diff` | Diff изменений (`staged=true` — застейдженных), опционально по пути; вывод обрезается на 64 КБ |

```bash
cd Day31/git-mcp-server && make build
# дальше агент сам поднимет сервер сабпроцессом по конфигу
```

В дефолтном `agent.config.yaml` доступ к `git` выдан ролям `researcher` и `reviewer`. Пример вопроса оркестратору: «Какая текущая git-ветка и какие файлы изменены?» — researcher вызовет `git_current_branch` + `git_list_files` и вернёт ответ.

### Petstore MCP-сервер (пример SSE)

```bash
cd Day31/mcp-server && make build
./ai-adv-mcp-server -addr 127.0.0.1:8931 &      # SSE-режим
```

Проверка в интерактиве: `/mcp` — подключённые серверы и число инструментов, `/tools` — все инструменты по серверам.

## TUI и slash-команды

Полноэкранный интерфейс: шапка + прокручиваемый лог + многострочный ввод.

| Клавиша | Действие |
|---|---|
| Enter | Новая строка (многострочный ввод, вставка абзацев работает) |
| Ctrl+S | Отправить (не доходит? `/keys` — диагностика нажатий) |
| ↑ / ↓ | Курсор по строкам ввода; на краю — история ввода |
| PgUp/PgDn, Home/End | Прокрутка лога (всегда работает) |
| колесо / тачпад | **Прокрутка лога — по умолчанию**; выделение: Option+drag (iTerm2) / Fn+drag (Terminal.app) или `/select` |
| Ctrl+C | Выход |

Slash-команды (двуязычные, строка с `/` никогда не уходит в LLM):

| Команда | Действие |
|---|---|
| `/help` `/помощь` | Список команд + **вход в режим консультанта по документации** |
| `/end` `/конец` | Выход из режима консультанта |
| `/agents` `/агенты` | Саб-агенты из конфига |
| `/memory` `/память` | Память оркестратора (задача/WM/LTM) |
| `/mcp` `/mcp-list` | Подключённые MCP-серверы |
| `/tools` `/инструменты` | MCP-инструменты по серверам |
| `/copy` `/копировать` | Последний ответ в буфер обмена (pbcopy/xclip, OSC 52 — работает и по SSH) |
| `/select` `/выделение` | Переключить: тачпад-прокрутка ↔ нативное выделение (или F2; на Mac F2 требует fn) |
| `/clear` `/очистить` | Очистить экран |
| `/exit` `/quit` `/выход` | Выход |

## Режим консультанта по документации

`/help` в интерактивной сессии переключает её в консультанта по проекту; `/end` возвращает оркестратор. В этом режиме ввод не уходит оркестратору — вопрос отвечается заземлённым Q&A:

- **RAG по документации** — секция `consultant.rag` конфига (по умолчанию `../rag/docs.db`); embed-модель обязана совпадать с той, которой построен индекс;
- **MCP-инструменты** — allow-list `consultant.mcp` (по умолчанию `["git"]`): вопросы о текущем состоянии репозитория (ветка, изменённые файлы, diff) отвечаются вызовом git-инструментов, не по документации;
- **история режима эфемерна** — сырые пары вопрос/ответ (окно 12 сообщений), RAG-контекст в историю не пишется, общая история сессии не затрагивается;
- индекс документации открывается лениво при входе в режим: если `docs.db` отсутствует, агент стартует нормально, а ошибка показывается при `/help`.

```
agent> /help                ← список команд + вход в режим
docs>  В каком дне появился реранкер?      ← ответ по docs.db с [n]-ссылками
docs>  Какая сейчас ветка и что изменено?  ← вызовет git_current_branch + git_list_files
docs>  /end                 ← назад к оркестратору
agent>
```

Настройка — секция `consultant:` в `agent.config.yaml` (`enabled`, `rag.*`, `mcp`, опциональный `prompt` — свой системный промпт).

## Переменные окружения

| Переменная | Назначение |
|---|---|
| `LLM_PROVIDER` | `openai` \| `openrouter` \| `gigachat` |
| `LLM_API_KEY` | Ключ API (только env, не YAML) |
| `LLM_MODEL` | Модель |
| `LLM_BASE_URL` | Кастомный endpoint (LM Studio, LiteLLM-прокси) |
| `LLM_CA_CERT` | PEM-сертификат для self-signed HTTPS |
| `AGENT_CONFIG` | Путь к YAML-конфигу (альтернатива `--config`) |

## Типовые сценарии

**Локальный запуск без облака** (LM Studio + Ollama):

```bash
make run-orchestrator-local LOCAL_LLM_BASE_URL=http://127.0.0.1:1234/v1 LLM_MODEL=google/gemma-4-26b-a4b
```

**Оркестратор с RAG поверх конфига** (флаги побеждают YAML):

```bash
make run-orchestrator-rag RAG_DB=rag.db RAG_TOP_K=20 RAG_THRESHOLD=0.5 RAG_TOP_K_FINAL=10
```

**Продолжение вчерашней сессии** — просто запустить снова с тем же `memory.dir`: история, LTM и память задачи подхватятся из файлов.

## Диагностика

| Симптом | Причина / решение |
|---|---|
| `LLM_API_KEY is not set` | Экспортировать ключ; `check-env` в Makefile проверяет до запуска |
| RAG находит мусор | Модель эмбеддингов не совпадает с индексом — проверить `embed_model` |
| `[rag] rerank failed, falling back…` | Реранкер недоступен/ошибка; ответ построен по similarity — проверить `rerank.model`/endpoint |
| Саб-агент «не видит» инструмент | Сервер не в `mcp`-списке его роли |
| MCP git-сервер не подключается | Бинарь не собран: `cd Day31/git-mcp-server && make build` |
| TUI не стартует | stdout не TTY → автоматический REPL; либо явно `--no-tui` |
| Не выделяется текст мышью | Мышь захвачена (дефолт): Option+drag / Fn+drag, либо `/select` |

## Тесты

```bash
cd Day31/agent
make test    # unit: конфиг-мерж, изоляция ephemeral/shared памяти, spawn→route→finish цикл
make vet
```

Интеграционные (нужен LLM + эмбеддинги): `make run-rag-eval`, `run-rag-grounded`, `run-rag-rerank-compare` и их `*-local` варианты.
