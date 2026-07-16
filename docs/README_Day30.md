# Day 30: self-signed HTTPS (приватный LiteLLM)

Агент достаёт OpenAI-совместимый эндпоинт за self-signed сертификатом.

## Что нового
- Флаг `--ca-cert` / env `LLM_CA_CERT` → `llm.Config.CACertFile`: PEM-сертификат **добавляется в trust pool** (системные корни + этот сертификат), TLS-проверка **остаётся включённой** (без `InsecureSkipVerify`); нечитаемый/невалидный PEM выдаёт предупреждение и откатывается на системные корни.
- LiteLLM-интеграция в Makefile: `litellm-issue-key` (SSH + master key, только владелец — минтит user-ключ), `litellm-fetch-cert` (скачать `litellm-ca.crt`, без SSH), `litellm-ping`, `run-litellm` (публичный прокси по HTTPS с `--ca-cert`).
- Ships `litellm-ca.crt`.

> См. заметку про gotcha opentelemetry 401 у прокси.

## Далее
Day31 — рефакторинг в конфиг-ориентированный оркестратор.
