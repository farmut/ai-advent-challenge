package petstore

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// PetSnapshot holds a single point-in-time reading of all sold pets.
type PetSnapshot struct {
	CollectedAt time.Time                `json:"collected_at"`
	SoldCount   int                      `json:"sold_count"`
	Pets        []map[string]interface{} `json:"pets"`
}

// SoldReport accumulates snapshots over time.
// IntervalSeconds records how often the agent intends to call report_collect_sold.
type SoldReport struct {
	IntervalSeconds int           `json:"interval_seconds"`
	Snapshots       []PetSnapshot `json:"snapshots"`
}

// CollectSoldReport fetches pets with status=sold, appends a new snapshot to
// the JSON report file (creating it if it doesn't exist yet), and returns a
// human-readable summary.
func CollectSoldReport(c *Client, reportFile string, intervalSec int) (string, error) {
	data, err := c.Get("/pet/findByStatus", url.Values{"status": {"sold"}})
	if err != nil {
		return "", fmt.Errorf("fetch sold pets: %w", err)
	}

	var rawPets []map[string]interface{}
	if err := json.Unmarshal(data, &rawPets); err != nil {
		// API may return something other than an array on error.
		return "", fmt.Errorf("parse sold pets response: %w", err)
	}

	// Load existing report so we can append to it.
	var report SoldReport
	if existing, readErr := os.ReadFile(reportFile); readErr == nil {
		_ = json.Unmarshal(existing, &report) // ignore parse errors — start fresh if corrupted
	}
	report.IntervalSeconds = intervalSec

	snap := PetSnapshot{
		CollectedAt: time.Now().UTC(),
		SoldCount:   len(rawPets),
		Pets:        rawPets,
	}
	report.Snapshots = append(report.Snapshots, snap)

	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(reportFile, out, 0o644); err != nil {
		return "", fmt.Errorf("write report file %q: %w", reportFile, err)
	}

	return fmt.Sprintf(
		"Snapshot #%d collected at %s: %d sold pet(s).\nCollection interval: %d second(s).\nReport saved to: %s",
		len(report.Snapshots),
		snap.CollectedAt.Format(time.RFC3339),
		snap.SoldCount,
		intervalSec,
		reportFile,
	), nil
}

// PrepareMarkdownData reads the JSON report, aggregates sales data across all
// snapshots, and returns a fully formatted Markdown document as a string.
// The returned string is intended to be passed directly to SaveMarkdownReport.
func PrepareMarkdownData(reportFile string) (string, error) {
	data, err := os.ReadFile(reportFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("report file not found: %q — call report_start_collection first", reportFile)
		}
		return "", fmt.Errorf("read report file: %w", err)
	}

	var report SoldReport
	if err := json.Unmarshal(data, &report); err != nil {
		return "", fmt.Errorf("parse report file: %w", err)
	}
	if len(report.Snapshots) == 0 {
		return "", fmt.Errorf("report file %q contains no snapshots yet", reportFile)
	}

	// Aggregate unique pets across all snapshots.
	type petStat struct {
		id       int64
		name     string
		category string
		tags     string
		seen     int
	}
	byID := map[int64]*petStat{}

	firstAt := report.Snapshots[0].CollectedAt
	lastAt := report.Snapshots[0].CollectedAt
	minCount, maxCount := report.Snapshots[0].SoldCount, report.Snapshots[0].SoldCount

	for _, snap := range report.Snapshots {
		if snap.CollectedAt.Before(firstAt) {
			firstAt = snap.CollectedAt
		}
		if snap.CollectedAt.After(lastAt) {
			lastAt = snap.CollectedAt
		}
		if snap.SoldCount < minCount {
			minCount = snap.SoldCount
		}
		if snap.SoldCount > maxCount {
			maxCount = snap.SoldCount
		}

		for _, p := range snap.Pets {
			id := int64(toFloat(p["id"]))
			if _, ok := byID[id]; !ok {
				byID[id] = &petStat{
					id:       id,
					name:     strField(p, "name"),
					category: categoryName(p),
					tags:     tagsStr(p),
				}
			}
			byID[id].seen++
		}
	}

	// Sort pets by ID for deterministic output.
	pets := make([]*petStat, 0, len(byID))
	for _, ps := range byID {
		pets = append(pets, ps)
	}
	sort.Slice(pets, func(i, j int) bool { return pets[i].id < pets[j].id })

	now := time.Now().UTC().Format(time.RFC3339)
	snapCount := len(report.Snapshots)

	var b strings.Builder

	fmt.Fprintf(&b, "# Sales Report\n\n")
	fmt.Fprintf(&b, "**Generated:** %s  \n", now)
	fmt.Fprintf(&b, "**Source file:** %s  \n", reportFile)
	fmt.Fprintf(&b, "**Collection interval:** %d second(s)  \n", report.IntervalSeconds)
	fmt.Fprintf(&b, "**Snapshots collected:** %d  \n", snapCount)
	fmt.Fprintf(&b, "**Period:** %s → %s  \n\n", firstAt.Format(time.RFC3339), lastAt.Format(time.RFC3339))

	fmt.Fprintf(&b, "## Summary\n\n")
	fmt.Fprintf(&b, "| Metric | Value |\n|--------|-------|\n")
	fmt.Fprintf(&b, "| Total snapshots | %d |\n", snapCount)
	fmt.Fprintf(&b, "| Unique pets sold | %d |\n", len(pets))
	fmt.Fprintf(&b, "| Min sold count (snapshot) | %d |\n", minCount)
	fmt.Fprintf(&b, "| Max sold count (snapshot) | %d |\n\n", maxCount)

	fmt.Fprintf(&b, "## Snapshot Trend\n\n")
	fmt.Fprintf(&b, "| # | Time | Sold count |\n|---|------|------------|\n")
	for i, snap := range report.Snapshots {
		fmt.Fprintf(&b, "| %d | %s | %d |\n", i+1, snap.CollectedAt.Format(time.RFC3339), snap.SoldCount)
	}
	fmt.Fprintf(&b, "\n")

	fmt.Fprintf(&b, "## Sold Pets\n\n")
	fmt.Fprintf(&b, "| ID | Name | Category | Tags | Seen in snapshots |\n|-----|------|----------|------|-------------------|\n")
	for _, ps := range pets {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %d |\n", ps.id, ps.name, ps.category, ps.tags, ps.seen)
	}
	fmt.Fprintf(&b, "\n")

	return b.String(), nil
}

// SaveMarkdownReport writes content to outputFile, creating parent directories
// as needed. Returns a short confirmation string suitable for tool output.
func SaveMarkdownReport(outputFile, content string) (string, error) {
	if outputFile == "" {
		return "", fmt.Errorf("'output_file' must not be empty")
	}
	if content == "" {
		return "", fmt.Errorf("'content' must not be empty")
	}
	if err := os.MkdirAll(dirOf(outputFile), 0o755); err != nil {
		return "", fmt.Errorf("create parent directory for %q: %w", outputFile, err)
	}
	if err := os.WriteFile(outputFile, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write markdown file %q: %w", outputFile, err)
	}
	lines := strings.Count(content, "\n")
	return fmt.Sprintf("Markdown report saved to: %s\n%d lines written.", outputFile, lines), nil
}

// ---------------------------------------------------------------------------
// report.go — private helpers
// ---------------------------------------------------------------------------

func toFloat(v interface{}) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func strField(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func categoryName(p map[string]interface{}) string {
	cat, ok := p["category"].(map[string]interface{})
	if !ok {
		return ""
	}
	return strField(cat, "name")
}

func tagsStr(p map[string]interface{}) string {
	raw, ok := p["tags"].([]interface{})
	if !ok || len(raw) == 0 {
		return ""
	}
	names := make([]string, 0, len(raw))
	for _, t := range raw {
		if m, ok := t.(map[string]interface{}); ok {
			if n := strField(m, "name"); n != "" {
				names = append(names, n)
			}
		}
	}
	return strings.Join(names, ", ")
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

// ShowSoldReport reads the report file and returns its contents as formatted JSON.
func ShowSoldReport(reportFile string) (string, error) {
	data, err := os.ReadFile(reportFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("report file not found: %q — call report_collect_sold first", reportFile)
		}
		return "", fmt.Errorf("read report file: %w", err)
	}

	// Re-marshal to get consistent pretty-printing even if the file was hand-edited.
	var report SoldReport
	if err := json.Unmarshal(data, &report); err != nil {
		return "", fmt.Errorf("parse report file: %w", err)
	}

	out, _ := json.MarshalIndent(report, "", "  ")

	snapshotCount := len(report.Snapshots)
	var latest string
	if snapshotCount > 0 {
		last := report.Snapshots[snapshotCount-1]
		latest = fmt.Sprintf("\nLatest snapshot: %s — %d sold pet(s)",
			last.CollectedAt.Format(time.RFC3339), last.SoldCount)
	}

	return fmt.Sprintf("Report file: %s\nSnapshots collected: %d\nInterval: %d second(s)%s\n\n%s",
		reportFile, snapshotCount, report.IntervalSeconds, latest, string(out)), nil
}
