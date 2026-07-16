# Day 29: model-init дефолты генерации

Параметры генерации привязываются к экземпляру модели.

## Что нового
- LLM-клиент (`adapter/llm`): `Config` получает `Temperature *float64`, `MaxTokens int`, `ContextWindow int` — дефолты модели, перекрываются только когда per-call `LLMRequest` задаёт их явно.
- `resolveGen` мержит request-over-config.
- `capContextWindow` обрезает старейшие **не-системные** сообщения (system и новейшее всегда сохраняются), пока оценка промпта не влезет в `window - reserve` токенов.
- Интеграционный тест `TestTelegramBotOptimization` (`telegram_opt_test.go`, `//go:build integration`, `run-telegram-opt` / `run-telegram-opt-local`): Telegram-bot задача под 4 конфигами (baseline vs gen-params + профиль + инварианты) на локальной модели, качество оценивает отдельная публичная `JUDGE_*` модель; отчёт `telegram_opt_result.txt`.

## Далее
Day30 добавляет доступ к эндпоинту за self-signed сертификатом.
