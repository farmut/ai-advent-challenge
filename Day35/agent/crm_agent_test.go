//go:build integration

// Integration test that emulates a support agent working a CRM queue for the
// FigurVault store (collectible game figures). It replays 20 customer tickets
// (testdata/crm/tickets/*.json) through the grounded RAG pipeline against the
// store's knowledge-base index (testdata/crm/shop.db), exactly as the agent
// would answer a real ticket: retrieve store-policy context → grounded LLM
// answer with sources + quotes.
//
// For every on-topic ticket it asserts the three CRM-quality properties:
//
//  1. the answer is GROUNDED (built from the KB, not hallucinated) and cites SOURCES,
//  2. the answer covers the ticket's EXPECTED KEYWORDS (the concrete policy facts
//     a correct reply must mention — thresholds, day counts, terms),
//  3. the retrieved context actually came from the KB (Sources non-empty).
//
// It also checks the low-relevance guard on one deliberately off-topic ticket
// (a PC-build question): the agent must return the ungrounded "не знаю" reply
// rather than invent an answer.
//
// Build the index once (nomic embeddings via LM Studio in this example):
//
//	cd ../rag && ./rag index --input ../agent/testdata/crm/kb --db ../agent/testdata/crm/shop.db \
//	  --strategy structural --embed-url http://127.0.0.1:1234 \
//	  --embed-model text-embedding-nomic-embed-text-v2-moe
//
// Run (needs the embeddings endpoint AND an LLM — same env as the other RAG tests):
//
//	SHOP_DB=testdata/crm/shop.db EMBED_URL=http://127.0.0.1:1234 \
//	  EMBED_MODEL=text-embedding-nomic-embed-text-v2-moe \
//	  LLM_PROVIDER=openrouter LLM_API_KEY=... LLM_MODEL=openai/gpt-4o \
//	  go test -tags integration -run TestCRMAgentTickets -v -timeout 1200s .
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/usecase"
)

const (
	crmReport      = "crm_agent_result.txt"
	crmTicketsDir  = "testdata/crm/tickets"
	crmDefaultDB   = "testdata/crm/shop.db"
	crmMinKeywords = 0.5 // at least half of a ticket's expected keywords must appear
)

// crmTicket mirrors the ticket JSON files in testdata/crm/tickets.
type crmTicket struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Channel   string `json:"channel"`
	Priority  string `json:"priority"`
	Category  string `json:"category"`
	Customer  struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"customer"`
	OrderID          string   `json:"order_id"`
	Subject          string   `json:"subject"`
	Body             string   `json:"body"`
	KBTopic          string   `json:"kb_topic"`
	OnTopic          bool     `json:"on_topic"`
	ExpectedKeywords []string `json:"expected_keywords"`
}

// question is what the agent actually reads from a ticket: subject + body.
func (t crmTicket) question() string {
	return strings.TrimSpace(t.Subject + "\n" + t.Body)
}

func loadCRMTickets(t *testing.T) []crmTicket {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(crmTicketsDir, "ticket_*.json"))
	if err != nil {
		t.Fatalf("glob tickets: %v", err)
	}
	if len(paths) != 20 {
		t.Fatalf("expected 20 ticket files, found %d", len(paths))
	}
	sort.Strings(paths)
	tickets := make([]crmTicket, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		var tk crmTicket
		if err := json.Unmarshal(raw, &tk); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		if tk.ID == "" || tk.Body == "" {
			t.Fatalf("%s: ticket missing id/body", p)
		}
		tickets = append(tickets, tk)
	}
	return tickets
}

func TestCRMAgentTickets(t *testing.T) {
	tickets := loadCRMTickets(t)

	db := os.Getenv("SHOP_DB")
	if db == "" {
		db = crmDefaultDB
	}
	if _, err := os.Stat(db); err != nil {
		t.Skipf("store index %s not found — build it with `rag index` (see file header); skipping", db)
	}

	embedURL := os.Getenv("EMBED_URL")
	if embedURL == "" {
		embedURL = "http://localhost:11434"
	}
	embedModel := os.Getenv("EMBED_MODEL")
	if embedModel == "" {
		embedModel = "nomic-embed-text"
	}

	// The agent needs an LLM to phrase the grounded answer; without it there is
	// no CRM reply to judge.
	llmClient, llmModel := buildLLMFromEnv(t) // skips when creds missing

	retriever, err := ragadapter.NewRetriever(ragadapter.Config{
		DBPath:     db,
		EmbedURL:   embedURL,
		EmbedModel: embedModel,
	})
	if err != nil {
		t.Skipf("cannot open retriever (%v) — skipping", err)
	}
	defer retriever.Close()

	uc := usecase.NewRAGUseCase(retriever, nil)
	// Threshold 0.45 is the water-line for this KB + nomic-v2-moe embeddings:
	// on-topic policy chunks score ~0.48-0.52, off-topic noise ~0.33, so the
	// off-topic guard short-circuits to the ungrounded "не знаю" reply while
	// relevant chunks still pass. TopKFinal 5 keeps enough context for answers
	// that span two KB sections.
	cfg := usecase.RAGConfig{TopKRetrieve: 8, Threshold: 0.45, TopKFinal: 5}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	var report strings.Builder
	fmt.Fprintf(&report, "CRM agent simulation — FigurVault store\nindex: %s  embed: %s  model: %s\n\n",
		db, embedModel, llmModel)

	var failures int
	for _, tk := range tickets {
		ans, res, err := uc.Answer(ctx, llmClient, llmModel, 0, tk.question(), cfg)
		if err != nil {
			t.Errorf("[%s] answer error: %v", tk.ID, err)
			failures++
			continue
		}

		fmt.Fprintf(&report, "=== %s [%s/%s] %s\n", tk.ID, tk.Category, tk.Priority, tk.Subject)
		fmt.Fprintf(&report, "  retrieved=%d final=%d grounded=%t sources=%d\n",
			len(res.Retrieved), len(res.Final), ans.Grounded, len(ans.Sources))
		fmt.Fprintf(&report, "  answer: %s\n", snippet(ans.Answer, 300))

		if !tk.OnTopic {
			// Guard: an off-topic ticket must NOT be answered from the store KB.
			if ans.Grounded {
				t.Errorf("[%s] off-topic ticket was answered as grounded (expected 'не знаю'): %s",
					tk.ID, snippet(ans.Answer, 160))
				failures++
				fmt.Fprintf(&report, "  RESULT: FAIL (off-topic answered as grounded)\n\n")
				t.Logf("FAIL %s (%s): off-topic answered as grounded", tk.ID, tk.Category)
			} else {
				fmt.Fprintf(&report, "  RESULT: OK (guard held — ungrounded reply)\n\n")
				t.Logf("OK   %s (%s): guard held — ungrounded 'не знаю'", tk.ID, tk.Category)
			}
			continue
		}

		var problems []string
		if !ans.Grounded {
			problems = append(problems, "not grounded")
		}
		if len(ans.Sources) == 0 {
			problems = append(problems, "no sources")
		}
		// Keyword coverage: the concrete policy facts a correct reply must carry.
		if len(tk.ExpectedKeywords) > 0 {
			missing := missingKeywords(strings.ToLower(ans.Answer), tk.ExpectedKeywords)
			hit := len(tk.ExpectedKeywords) - len(missing)
			coverage := float64(hit) / float64(len(tk.ExpectedKeywords))
			fmt.Fprintf(&report, "  keywords: %d/%d covered (missing: %v)\n",
				hit, len(tk.ExpectedKeywords), missing)
			if coverage < crmMinKeywords {
				problems = append(problems, fmt.Sprintf("keyword coverage %.0f%% < %.0f%%",
					coverage*100, crmMinKeywords*100))
			}
		}

		if len(problems) > 0 {
			t.Errorf("[%s] %s", tk.ID, strings.Join(problems, "; "))
			failures++
			fmt.Fprintf(&report, "  RESULT: FAIL (%s)\n\n", strings.Join(problems, "; "))
			t.Logf("FAIL %s (%s): %s | %s", tk.ID, tk.Category, strings.Join(problems, "; "), snippet(ans.Answer, 120))
		} else {
			fmt.Fprintf(&report, "  RESULT: OK\n\n")
			t.Logf("OK   %s (%s): grounded, %d src | %s", tk.ID, tk.Category, len(ans.Sources), snippet(ans.Answer, 120))
		}
	}

	fmt.Fprintf(&report, "SUMMARY: %d/%d tickets passed\n", len(tickets)-failures, len(tickets))
	if err := os.WriteFile(crmReport, []byte(report.String()), 0o644); err != nil {
		t.Logf("could not write %s: %v", crmReport, err)
	}
	t.Logf("report written to %s (%d/%d passed)", crmReport, len(tickets)-failures, len(tickets))
}
