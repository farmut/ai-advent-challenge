# Day 28: полностью локальный RAG и укреплённый реранкер

Весь RAG-стек можно гонять локально; реранкер устойчивее.

## Что нового
1. **Fallback реранка**: `usecase.RAGResult` получает поле `RerankErr` — при провале реранка пайплайн деградирует до similarity-порядка (`Reranked=false`), не падает; `main.go` печатает `[rag] rerank failed, falling back…` (fallback виден, не тихий).
2. **Line-format реранкер**: chat-реранкер (`reranker.go`) сменил промпт с JSON на одну строку `"N score"` на пассаж — purpose-built реранкеры через chat-эндпоинт (Ollama `MedAIBase/Qwen3-VL-Reranker:2b`) не instruction-tuned и манглят JSON, но надёжно выдают нумерованные строки; `parseRerankScores` их читает и игнорирует хвостовую прозу; `MaxTokens` кап `len(chunks)*8 + 24`.
3. **Локальные `*-local` Makefile-цели**: `run-rag-local`, `run-rag-interactive-local`, `run-rag-eval-local`, `run-rag-grounded-local`, `run-rag-compare-local`, `run-rag-rerank-compare-local`, `run-rag-tests-local` через `LOCAL_OVERRIDES` — по умолчанию LM Studio (`google/gemma-4-26b-a4b` LLM + `text-embedding-nomic-embed-text-v2-moe` эмбеддинги @ `127.0.0.1:1234`) + Ollama-реранкер в chat-режиме @ `127.0.0.1:11434/v1`.

## Далее
Day29 добавляет model-init дефолты генерации.
