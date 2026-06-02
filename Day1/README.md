# ai-adv-agent

Консольное приложение на Go для взаимодействия с LLM через API. Отправляет запросы к OpenAI и OpenRouter и выводит ответ в терминале с Markdown форматированием.

## Возможности

- Поддержка OpenAI API
- Поддержка OpenRouter API
- Рендеринг Markdown в терминале (жирный текст, списки, блоки кода)
- Настройка через переменные окружения
- Поддержка OpenAI-совместимых API endpoints

## Требования

- Go 1.21 или выше

## Сборка

```bash
go build -o ai-adv-agent .
```

Или запуск без сборки:

```bash
go run . --query "Ваш вопрос"
```

## Использование

```bash
./ai-adv-agent --query "Ваш вопрос к LLM"
```

### Переменные окружения

| Переменная | Описание | Обязательна | По умолчанию |
|------------|----------|-------------|--------------|
| `LLM_PROVIDER` | Провайдер LLM (`openai`, `openrouter`) | Да | - |
| `LLM_API_KEY` | API ключ провайдера | Да | - |
| `LLM_MODEL` | Название модели | Нет | `gpt-4o` |
| `LLM_BASE_URL` | Кастомный API endpoint | Нет | `https://api.openai.com/v1` или `https://openrouter.ai/api/v1` для OpenRouter |

## Примеры

### OpenAI

```bash
export LLM_PROVIDER=openai
export LLM_API_KEY=sk-your-api-key

# Использовать модель по умолчанию (gpt-4o)
./ai-adv-agent --query "Что такое REST API?"

# Использовать другую модель
export LLM_MODEL=gpt-3.5-turbo
./ai-adv-agent --query "Напиши функцию сортировки на Go"
```

### OpenAI-совместимые сервисы

Для работы с локальными LLM (например, Ollama) или другими провайдерами с OpenAI-совместимым API:

```bash
export LLM_PROVIDER=openai
export LLM_API_KEY=your-local-key
export LLM_BASE_URL=http://localhost:11434/v1
export LLM_MODEL=llama2

./ai-adv-agent --query "Объясни что такое микросервисы"
```

### OpenRouter

OpenRouter предоставляет доступ к множеству моделей через единый API:

```bash
export LLM_PROVIDER=openrouter
export LLM_API_KEY=sk-or-v1-your-api-key
export LLM_MODEL=openai/gpt-oss-120b:free

./ai-adv-agent --query "Напиши краткое содержание книги 'Война и Мир'"
```

Доступные модели можно найти на [openrouter.ai/models](https://openrouter.ai/models).

## Структура проекта

```
.
├── main.go      # Основной код приложения
├── go.mod       # Модуль Go
└── go.sum       # Контрольные суммы зависимостей
```

## Зависимости

- `github.com/sashabaranov/go-openai` — клиент OpenAI API
- `github.com/charmbracelet/glamour` — рендеринг Markdown в терминале

## Лицензия

MIT
