package petstore

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// Collector runs a periodic background goroutine that appends sold-pet snapshots
// to a JSON report file.  Only one collection can be active at a time.
//
// The collector is stateful and must outlive individual tool calls, so it is
// housed inside a Handler that is created once per MCP server process.  This
// means background collection only works when the MCP server runs persistently
// (HTTP+SSE mode via -addr).  In stdio mode the server process is killed after
// each tool call, so collection effectively stops after the first snapshot.
type Collector struct {
	mu          sync.Mutex
	running     bool
	stopCh      chan struct{}
	doneCh      chan struct{} // closed by the goroutine when it exits
	reportFile  string
	intervalSec int
	collected   int       // snapshots appended since last Start
	lastAt      time.Time // UTC time of the last successful snapshot
}

// NewCollector returns a stopped Collector ready to use.
func NewCollector() *Collector { return &Collector{} }

// Start launches the background collection goroutine.
// The first snapshot is taken immediately; subsequent ones follow every
// intervalSec seconds.  Returns an error if a collection is already running —
// call Stop first to change parameters.
func (col *Collector) Start(c *Client, reportFile string, intervalSec int) (string, error) {
	col.mu.Lock()
	defer col.mu.Unlock()

	if col.running {
		return "", fmt.Errorf(
			"collection already running (interval=%ds, file=%q) — call report_stop_collection first",
			col.intervalSec, col.reportFile,
		)
	}

	col.reportFile = reportFile
	col.intervalSec = intervalSec
	col.collected = 0
	col.lastAt = time.Time{}
	col.stopCh = make(chan struct{})
	col.doneCh = make(chan struct{})
	col.running = true

	go col.loop(c)

	return fmt.Sprintf(
		"Collection started: snapshot every %d second(s), writing to %q.\n"+
			"First snapshot is taken immediately in the background.",
		intervalSec, reportFile,
	), nil
}

// Stop signals the background goroutine to exit and waits for it to finish.
func (col *Collector) Stop() (string, error) {
	col.mu.Lock()

	if !col.running {
		col.mu.Unlock()
		return "No active collection to stop.", nil
	}

	stopCh := col.stopCh
	doneCh := col.doneCh
	collected := col.collected
	lastAt := col.lastAt
	reportFile := col.reportFile
	col.running = false

	col.mu.Unlock()

	close(stopCh)
	<-doneCh // wait for the goroutine to finish its current snapshot

	var lastStr string
	if lastAt.IsZero() {
		lastStr = "none yet"
	} else {
		lastStr = lastAt.Format(time.RFC3339)
	}

	return fmt.Sprintf(
		"Collection stopped.\nSnapshots collected this run: %d\nLast snapshot: %s\nReport file: %s",
		collected, lastStr, reportFile,
	), nil
}

// Status returns a human-readable summary of the current collector state.
func (col *Collector) Status() string {
	col.mu.Lock()
	defer col.mu.Unlock()

	if !col.running {
		return "Collection status: not running."
	}

	var lastStr string
	if col.lastAt.IsZero() {
		lastStr = "pending (first snapshot not yet completed)"
	} else {
		lastStr = col.lastAt.Format(time.RFC3339)
	}

	return fmt.Sprintf(
		"Collection status: running\nInterval: %d second(s)\nReport file: %s\nSnapshots this run: %d\nLast snapshot: %s",
		col.intervalSec, col.reportFile, col.collected, lastStr,
	)
}

// ---------------------------------------------------------------------------
// internal
// ---------------------------------------------------------------------------

func (col *Collector) loop(c *Client) {
	defer close(col.doneCh)

	col.doCollect(c) // immediate first snapshot

	ticker := time.NewTicker(time.Duration(col.intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			col.doCollect(c)
		case <-col.stopCh:
			return
		}
	}
}

func (col *Collector) doCollect(c *Client) {
	// reportFile and intervalSec are written once during Start and read-only
	// afterwards, so no lock is needed to read them here.
	_, err := CollectSoldReport(c, col.reportFile, col.intervalSec)

	col.mu.Lock()
	defer col.mu.Unlock()

	if err != nil {
		log.Printf("[collector] snapshot error: %v", err)
		return
	}
	col.collected++
	col.lastAt = time.Now().UTC()
	log.Printf("[collector] snapshot #%d at %s → %s",
		col.collected, col.lastAt.Format(time.RFC3339), col.reportFile)
}
