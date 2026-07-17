//go:build integration

// Integration test for Day29: does prompt engineering (parameter optimization +
// a developer profile + hard invariants) actually improve the agent's output?
//
// The agent is asked ONE coding task — "write a Telegram bot that echoes every
// incoming message back 4 times in different letter cases" — under four
// configurations, in order:
//
//  1. baseline               — no profile, no invariants, provider-default
//     temperature / no max-tokens cap (no optimization)
//  2. optimized params       — tuned temperature + max-tokens (context budget),
//     still no profile / invariants
//  3. profile + invariants   — profile_go.md + invariants.md injected, but NO
//     temperature / max-tokens optimization
//  4. profile + invariants   — everything: profile + invariants AND the tuned
//     + optimized params        temperature / max-tokens
//
// Every generation runs ONLY on the local model (google/gemma-4-26b-a4b at
// http://127.0.0.1:1234) taken from the LLM_* env. After all four variants run,
// a separate PUBLIC judge model (openrouter deepseek/deepseek-v4-flash, from the
// JUDGE_* env) scores and compares them. The result — quality before vs. after
// optimization, plus a speed / token-consumption table — is written to
// telegram_opt_result.txt.
//
// Run via the Makefile:
//
//	make run-telegram-opt-local          # local gemma generation + openrouter judge
//
// or directly:
//
//	LLM_PROVIDER=openai LLM_BASE_URL=http://127.0.0.1:1234/v1 \
//	  LLM_MODEL=google/gemma-4-26b-a4b LLM_API_KEY=local \
//	  JUDGE_PROVIDER=openrouter JUDGE_MODEL=deepseek/deepseek-v4-flash \
//	  JUDGE_API_KEY=$OPENROUTER_KEY \
//	  go test -tags integration -run TestTelegramBotOptimization -v -timeout 1800s .
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/llm"
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

const telegramOptReport = "telegram_opt_result.txt"

// The single coding task every variant is asked to solve.
const telegramBotTask = "Напиши Telegram-бота на Go, который принимает входящие " +
	"текстовые сообщения и в ответ повторяет каждое полученное сообщение 4 раза, " +
	"каждый раз в другом регистре: 1) нижний регистр, 2) ВЕРХНИЙ РЕГИСТР, " +
	"3) Регистр Как В Заголовке (Title Case), 4) чередующийся рЕгИсТр. " +
	"Приведи полный компилируемый код и краткое пояснение."

// Tuned generation parameters for the "optimized" model. They are applied at
// MODEL-INIT time (llm.Config passed to NewClient), not per call: low temperature
// keeps code deterministic, a bounded completion budget + context window are the
// resource optimization for a single-shot code task.
const (
	optimizedTemperature   = 0.2
	optimizedMaxTokens     = 4096
	optimizedContextWindow = 12288
)

// variant describes one of the four configurations under test.
type variant struct {
	name          string
	useProfile    bool
	useInvariants bool
	optimized     bool // tuned temperature + max-tokens
}

// variantResult captures everything the report needs about one run.
type variantResult struct {
	variant
	system    string
	content   string
	usage     domain.Usage
	elapsed   time.Duration
	temp      float64
	maxTokens int
	err       error
}

func TestTelegramBotOptimization(t *testing.T) {
	// Generation: local model only (google/gemma-4-26b-a4b), read from LLM_* env.
	// Two clients differ ONLY by their init-time config: baseClient has no tuned
	// params; optClient bakes temperature / max-tokens / context-window into the
	// model at construction. No generation parameter is passed per call.
	baseClient, optClient, genModel := buildGenClientsFromEnv(t)

	// Build the profile + invariants blocks once — injected in variants 3 and 4.
	profileBlock := loadProfileBlock(t, "profile_go.md")
	invariantsBlock := loadInvariantsBlock(t, "invariants.md")

	variants := []variant{
		{name: "1-baseline", useProfile: false, useInvariants: false, optimized: false},
		{name: "2-optimized-params", useProfile: false, useInvariants: false, optimized: true},
		{name: "3-profile+invariants", useProfile: true, useInvariants: true, optimized: false},
		{name: "4-profile+invariants+optimized", useProfile: true, useInvariants: true, optimized: true},
	}

	f, err := os.Create(telegramOptReport)
	if err != nil {
		t.Fatalf("create report: %v", err)
	}
	defer f.Close()
	log := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		t.Log(line)
		fmt.Fprintln(f, line)
	}

	log("# Telegram-bot: влияние оптимизации на качество ответа")
	log("Дата: %s", time.Now().Format("2006-01-02 15:04:05"))
	log("Модель генерации (локальная): %s", genModel)
	log("Задача: %s", telegramBotTask)
	log("")

	results := make([]variantResult, 0, len(variants))
	for _, v := range variants {
		system := buildSystem(v, profileBlock, invariantsBlock)

		// The optimization lives in the model instance, not in the call: pick the
		// client whose init-time config carries the tuned params.
		client := baseClient
		temp, maxTok, ctxWin := -1.0, 0, 0 // baseline: provider default / no caps
		if v.optimized {
			client = optClient
			temp, maxTok, ctxWin = optimizedTemperature, optimizedMaxTokens, optimizedContextWindow
		}

		log("## Вариант %s", v.name)
		log("  profile=%t  invariants=%t  optimized=%t (модель инициализирована: temperature=%s, max_tokens=%s, context_window=%s)",
			v.useProfile, v.useInvariants, v.optimized, tempStr(temp), maxTokStr(maxTok), maxTokStr(ctxWin))

		// No generation params on the request — they come from the client config.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		start := time.Now()
		resp, callErr := client.Chat(ctx, port.LLMRequest{
			Model:       genModel,
			Messages:    buildMessages(system, telegramBotTask),
			Temperature: -1, // unset — defer to the client's configured default
		})
		elapsed := time.Since(start)
		cancel()

		res := variantResult{
			variant: v, system: system, content: resp.Content, usage: resp.Usage,
			elapsed: elapsed, temp: temp, maxTokens: maxTok, err: callErr,
		}
		results = append(results, res)

		if callErr != nil {
			log("  ОШИБКА генерации: %v", callErr)
			log("")
			continue
		}
		log("  время: %s | токены: prompt=%d completion=%d total=%d",
			elapsed.Round(time.Millisecond), resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		log("  --- полный ответ ---")
		log("%s", resp.Content)
		log("  --- конец ответа ---")
		log("")
	}

	// ---- Resource table: speed + token consumption (requirement 3) ----
	log("## Скорость и потребление ресурсов (токены)")
	log("%-34s %10s %8s %10s %6s", "вариант", "время", "prompt", "completion", "total")
	for _, r := range results {
		if r.err != nil {
			log("%-34s %10s  (ошибка)", r.name, r.elapsed.Round(time.Millisecond))
			continue
		}
		log("%-34s %10s %8d %10d %6d", r.name, r.elapsed.Round(time.Millisecond),
			r.usage.PromptTokens, r.usage.CompletionTokens, r.usage.TotalTokens)
	}
	log("")

	// ---- LLM judge: quality before vs. after (requirements 1 & 2) ----
	judgeClient, judgeModel, ok := buildJudgeFromEnv(t)
	if !ok {
		log("## Сравнение качества")
		log("ПРОПУЩЕНО: judge-модель не сконфигурирована (JUDGE_PROVIDER/JUDGE_API_KEY). " +
			"Задай их, чтобы получить сравнение качества через openrouter deepseek.")
		t.Log("judge model not configured — resource table written, quality comparison skipped")
		return
	}
	log("## Сравнение качества (судья: %s)", judgeModel)
	verdict, jErr := judgeVariants(judgeModel, judgeClient, results)
	if jErr != nil {
		log("ОШИБКА судьи: %v", jErr)
		t.Fatalf("judge failed: %v", jErr)
	}
	log("%s", verdict)

	// Sanity: every variant that ran must have produced non-empty code.
	for _, r := range results {
		if r.err == nil && strings.TrimSpace(r.content) == "" {
			t.Errorf("variant %s produced empty output", r.name)
		}
	}
	t.Logf("report written to %s", telegramOptReport)
}

// buildSystem assembles the system prompt for a variant: the profile block and
// the invariants block are prepended only when the variant enables them.
func buildSystem(v variant, profileBlock, invariantsBlock string) string {
	var parts []string
	if v.useProfile && profileBlock != "" {
		parts = append(parts, profileBlock)
	}
	if v.useInvariants && invariantsBlock != "" {
		parts = append(parts, invariantsBlock)
	}
	return strings.Join(parts, "\n\n---\n\n")
}

func buildMessages(system, task string) []domain.Message {
	msgs := make([]domain.Message, 0, 2)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, domain.Message{Role: domain.RoleSystem, Content: system})
	}
	msgs = append(msgs, domain.Message{Role: domain.RoleUser, Content: task})
	return msgs
}

// loadProfileBlock loads profile_go.md and renders it the same way the agent
// injects it into a system prompt (usecase.ProfileSystemBlock).
func loadProfileBlock(t *testing.T, path string) string {
	t.Helper()
	p, err := storage.NewProfileFile(path).Load()
	if err != nil {
		t.Fatalf("load profile %s: %v", path, err)
	}
	return usecase.ProfileSystemBlock(p)
}

// loadInvariantsBlock loads invariants.md (raw Markdown) and wraps it in the
// header the agent uses so the model treats them as hard constraints.
func loadInvariantsBlock(t *testing.T, path string) string {
	t.Helper()
	raw, err := storage.NewInvariantsFile(path).Load()
	if err != nil {
		t.Fatalf("load invariants %s: %v", path, err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return "[Invariants — абсолютные ограничения, которые нельзя нарушать]\n" + raw
}

// buildGenClientsFromEnv reads the local generation model config from LLM_* env
// and returns two clients that differ ONLY by init-time config: base (no tuned
// params) and optimized (temperature / max-tokens / context-window baked in at
// NewClient). Skips when LLM creds are missing.
func buildGenClientsFromEnv(t *testing.T) (base, optimized port.LLMClient, model string) {
	t.Helper()
	provider := os.Getenv("LLM_PROVIDER")
	apiKey := os.Getenv("LLM_API_KEY")
	if provider == "" || apiKey == "" {
		t.Skip("LLM_PROVIDER/LLM_API_KEY not set — skipping generation")
	}
	model = os.Getenv("LLM_MODEL")
	baseURL := strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	if baseURL == "" {
		switch provider {
		case llm.ProviderOpenRouter:
			baseURL = "https://openrouter.ai/api/v1"
		case llm.ProviderGigaChat:
			baseURL = llm.DefaultGigaChatBaseURL
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}
	if provider == llm.ProviderOpenAI && model == "" {
		model = "gpt-4o"
	}
	if provider == llm.ProviderGigaChat && model == "" {
		model = "GigaChat"
	}
	baseCfg := llm.Config{
		Provider:      provider,
		APIKey:        apiKey,
		BaseURL:       baseURL,
		GigaChatScope: os.Getenv("GIGACHAT_SCOPE"),
	}
	optTemp := optimizedTemperature
	optCfg := baseCfg
	optCfg.Temperature = &optTemp
	optCfg.MaxTokens = optimizedMaxTokens
	optCfg.ContextWindow = optimizedContextWindow
	return llm.NewClient(baseCfg), llm.NewClient(optCfg), model
}

// buildJudgeFromEnv builds the public comparison model (openrouter
// deepseek/deepseek-v4-flash by default) from the JUDGE_* env. Returns ok=false
// when no credentials are configured so the caller can still emit the report.
func buildJudgeFromEnv(t *testing.T) (port.LLMClient, string, bool) {
	t.Helper()
	provider := os.Getenv("JUDGE_PROVIDER")
	apiKey := os.Getenv("JUDGE_API_KEY")
	if provider == "" {
		provider = llm.ProviderOpenRouter
	}
	if apiKey == "" {
		return nil, "", false
	}
	model := os.Getenv("JUDGE_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4-flash"
	}
	baseURL := strings.TrimRight(os.Getenv("JUDGE_BASE_URL"), "/")
	if baseURL == "" {
		switch provider {
		case llm.ProviderOpenRouter:
			baseURL = "https://openrouter.ai/api/v1"
		case llm.ProviderGigaChat:
			baseURL = llm.DefaultGigaChatBaseURL
		default:
			baseURL = "https://api.openai.com/v1"
		}
	}
	return llm.NewClient(llm.Config{
		Provider:      provider,
		APIKey:        apiKey,
		BaseURL:       baseURL,
		GigaChatScope: os.Getenv("GIGACHAT_SCOPE"),
	}), model, true
}

// judgeVariants asks the public judge model to score every variant's code and
// write a comparison focused on quality before vs. after optimization and on
// whether the profile / invariants were respected.
func judgeVariants(model string, judge port.LLMClient, results []variantResult) (string, error) {
	var sb strings.Builder
	sb.WriteString("Ты — строгий Go-ревьюер. Ниже 4 ответа одной и той же локальной LLM ")
	sb.WriteString("на одну задачу (Telegram-бот, повторяющий сообщение 4 раза в разном регистре), ")
	sb.WriteString("сгенерированные при разной конфигурации.\n\n")
	sb.WriteString("Конфигурации:\n")
	sb.WriteString("- 1-baseline: без профиля, без инвариантов, без оптимизации параметров.\n")
	sb.WriteString("- 2-optimized-params: с оптимизацией temperature/max_tokens, без профиля и инвариантов.\n")
	sb.WriteString("- 3-profile+invariants: с профилем Go-разработчика и инвариантами, без оптимизации параметров.\n")
	sb.WriteString("- 4-profile+invariants+optimized: профиль + инварианты + оптимизация параметров.\n\n")
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("=== Вариант %s ===\n", r.name))
		if r.err != nil {
			sb.WriteString(fmt.Sprintf("(ошибка генерации: %v)\n\n", r.err))
			continue
		}
		sb.WriteString(r.content)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Задачи:\n")
	sb.WriteString("1) Оцени КАЖДЫЙ вариант по шкале 1-10 (корректность решения задачи с регистрами, ")
	sb.WriteString("качество и идиоматичность Go-кода, полнота). Дай короткое обоснование оценки.\n")
	sb.WriteString("2) Отдельно сравни КАЧЕСТВО ДО оптимизации (варианты 1 и 3) и ПОСЛЕ оптимизации ")
	sb.WriteString("(варианты 2 и 4): помогла ли оптимизация параметров?\n")
	sb.WriteString("3) Для вариантов 3 и 4 проверь соблюдение инвариантов (только стандартная библиотека Go + ")
	sb.WriteString("официальная Telegram-библиотека, комментарии к функциям, log/slog, Clean Architecture) ")
	sb.WriteString("и профиля (краткость, code-first, русский язык).\n")
	sb.WriteString("4) В конце выведи итоговую таблицу: вариант -> оценка -> один вывод.\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	resp, err := judge.Chat(ctx, port.LLMRequest{
		Model:       model,
		Messages:    []domain.Message{{Role: domain.RoleUser, Content: sb.String()}},
		Temperature: 0,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func tempStr(t float64) string {
	if t < 0 {
		return "provider-default"
	}
	return fmt.Sprintf("%.2f", t)
}

func maxTokStr(m int) string {
	if m == 0 {
		return "без ограничения"
	}
	return fmt.Sprintf("%d", m)
}
