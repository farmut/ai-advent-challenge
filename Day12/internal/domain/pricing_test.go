package domain_test

import (
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
)

// ---- PricingFor ----

func TestPricingFor_KnownModel(t *testing.T) {
	p, ok := domain.PricingFor("gpt-4o-mini")
	if !ok {
		t.Fatal("gpt-4o-mini should be a known model")
	}
	if p.InputPer1M != 0.15 {
		t.Errorf("gpt-4o-mini input price = %v, want 0.15", p.InputPer1M)
	}
	if p.OutputPer1M != 0.60 {
		t.Errorf("gpt-4o-mini output price = %v, want 0.60", p.OutputPer1M)
	}
	if p.ContextWindow != 128_000 {
		t.Errorf("gpt-4o-mini context window = %d, want 128000", p.ContextWindow)
	}
}

func TestPricingFor_OpenRouterPrefixed(t *testing.T) {
	p, ok := domain.PricingFor("openai/gpt-4o")
	if !ok {
		t.Fatal("openai/gpt-4o should be a known model")
	}
	if p.InputPer1M != 2.50 {
		t.Errorf("openai/gpt-4o input price = %v, want 2.50", p.InputPer1M)
	}
	if p.ContextWindow != 128_000 {
		t.Errorf("openai/gpt-4o context window = %d, want 128000", p.ContextWindow)
	}
}

func TestPricingFor_FreeSuffix(t *testing.T) {
	p, ok := domain.PricingFor("openai/gpt-4o-mini:free")
	if !ok {
		t.Fatal("openai/gpt-4o-mini:free should resolve via suffix stripping")
	}
	if p.ContextWindow != 128_000 {
		t.Errorf("context window should survive suffix strip, got %d", p.ContextWindow)
	}
}

func TestPricingFor_UnknownModel(t *testing.T) {
	_, ok := domain.PricingFor("unicorn/model-xyz-9000")
	if ok {
		t.Error("unknown model should not be found in pricing table")
	}
}

func TestPricingFor_CostCalculation(t *testing.T) {
	p, _ := domain.PricingFor("gpt-4o-mini")
	cost := float64(1000)/1_000_000*p.InputPer1M + float64(200)/1_000_000*p.OutputPer1M
	if cost <= 0 {
		t.Errorf("cost should be positive, got %v", cost)
	}
}

func TestPricingFor_ContextWindowVariety(t *testing.T) {
	cases := []struct {
		model   string
		wantCtx int
	}{
		{"gpt-3.5-turbo", 16_385},
		{"o1", 200_000},
		{"anthropic/claude-3-5-sonnet", 200_000},
		{"qwen/qwen3.5-9b", 32_768},
	}
	for _, tc := range cases {
		p, ok := domain.PricingFor(tc.model)
		if !ok {
			t.Errorf("model %q should be known", tc.model)
			continue
		}
		if p.ContextWindow != tc.wantCtx {
			t.Errorf("model %q: context window = %d, want %d", tc.model, p.ContextWindow, tc.wantCtx)
		}
	}
}

// ---- ContextStatus ----

func TestContextStatus_OK(t *testing.T) {
	if s := domain.ContextStatus(10.0); s != "[OK]" {
		t.Errorf("10%% should be OK, got %q", s)
	}
	if s := domain.ContextStatus(49.9); s != "[OK]" {
		t.Errorf("49.9%% should be OK, got %q", s)
	}
}

func TestContextStatus_Note(t *testing.T) {
	if s := domain.ContextStatus(50.0); s != "[NOTE: context half full]" {
		t.Errorf("50%% should be NOTE, got %q", s)
	}
	if s := domain.ContextStatus(79.9); s != "[NOTE: context half full]" {
		t.Errorf("79.9%% should be NOTE, got %q", s)
	}
}

func TestContextStatus_Warn(t *testing.T) {
	if s := domain.ContextStatus(80.0); s != "[WARN: context filling up]" {
		t.Errorf("80%% should be WARN, got %q", s)
	}
	if s := domain.ContextStatus(94.9); s != "[WARN: context filling up]" {
		t.Errorf("94.9%% should be WARN, got %q", s)
	}
}

func TestContextStatus_Critical(t *testing.T) {
	if s := domain.ContextStatus(95.0); s != "[CRITICAL: context almost full!]" {
		t.Errorf("95%% should be CRITICAL, got %q", s)
	}
	if s := domain.ContextStatus(100.0); s != "[CRITICAL: context almost full!]" {
		t.Errorf("100%% should be CRITICAL, got %q", s)
	}
}

// ---- EstimateTokens ----

func TestEstimateTokens_Basic(t *testing.T) {
	text := strings.Repeat("a", 400)
	est := domain.EstimateTokens(text)
	if est < 80 || est > 120 {
		t.Errorf("EstimateTokens(%d chars) = %d, expected ~100", len(text), est)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	if est := domain.EstimateTokens(""); est < 1 {
		t.Errorf("EstimateTokens(\"\") = %d, expected >= 1", est)
	}
}

func TestEstimateTokens_Unicode(t *testing.T) {
	text := strings.Repeat("ж", 100)
	est := domain.EstimateTokens(text)
	if est < 15 || est > 35 {
		t.Errorf("EstimateTokens(100 Cyrillic runes) = %d, expected ~25", est)
	}
}

// ---- EstimateMessagesTokens ----

func TestEstimateMessagesTokens_Empty(t *testing.T) {
	if got := domain.EstimateMessagesTokens(nil); got != 0 {
		t.Errorf("empty messages = %d, want 0", got)
	}
}

func TestEstimateMessagesTokens_Single(t *testing.T) {
	msgs := []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("a", 400)}}
	got := domain.EstimateMessagesTokens(msgs)
	if got < 100 || got > 110 {
		t.Errorf("single 400-char message = %d tokens, expected ~104", got)
	}
}

func TestEstimateMessagesTokens_Multiple(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleSystem, Content: strings.Repeat("a", 400)},
		{Role: domain.RoleUser, Content: strings.Repeat("b", 400)},
		{Role: domain.RoleAssistant, Content: strings.Repeat("c", 400)},
	}
	got := domain.EstimateMessagesTokens(msgs)
	if got < 300 || got > 325 {
		t.Errorf("three 400-char messages = %d tokens, expected ~312", got)
	}
}

func TestEstimateMessagesTokens_OverflowDetection(t *testing.T) {
	bigContent := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 12000)
	msgs := []domain.Message{{Role: domain.RoleUser, Content: bigContent}}
	got := domain.EstimateMessagesTokens(msgs)
	p, _ := domain.PricingFor("gpt-4o-mini")
	if got <= p.ContextWindow {
		t.Errorf("expected estimate (%d) to exceed context window (%d)", got, p.ContextWindow)
	}
}
