package petstore

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
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
