package petstore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestClient builds a Client pointed at a test server.
func newTestClient(srv *httptest.Server) *Client {
	return &Client{
		base: srv.URL,
		http: srv.Client(),
	}
}

func TestToolDefinitions_AllHaveSchema(t *testing.T) {
	tools := ToolDefinitions()
	if len(tools) == 0 {
		t.Fatal("ToolDefinitions returned no tools")
	}
	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil InputSchema", tool.Name)
		}
	}
	t.Logf("total tools: %d", len(tools))
}

func TestToolDefinitions_Names(t *testing.T) {
	expected := []string{
		"pet_add", "pet_update", "pet_find_by_status", "pet_find_by_tags",
		"pet_get_by_id", "pet_update_with_form", "pet_delete",
		"store_get_inventory", "store_place_order", "store_get_order", "store_delete_order",
		"user_create", "user_create_with_list", "user_login", "user_logout",
		"user_get", "user_update", "user_delete",
		"report_start_collection", "report_stop_collection", "report_collection_status",
		"report_show_sold",
		"report_prepare_markdown_data", "report_save_markdown",
	}
	tools := ToolDefinitions()
	byName := make(map[string]bool, len(tools))
	for _, t := range tools {
		byName[t.Name] = true
	}
	for _, name := range expected {
		if !byName[name] {
			t.Errorf("expected tool %q is missing", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration-style tests against a mock HTTP server
// ---------------------------------------------------------------------------

func TestCallTool_PetGetByID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pet/42" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 42, "name": "Buddy", "status": "available",
			"photoUrls": []string{"http://example.com/buddy.jpg"},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := CallTool(c, "pet_get_by_id", map[string]interface{}{"petId": float64(42)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Buddy") {
		t.Errorf("expected 'Buddy' in result, got: %s", result)
	}
}

func TestCallTool_PetFindByStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pet/findByStatus" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		statuses := r.URL.Query()["status"]
		if len(statuses) == 0 {
			t.Error("expected status query param")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": 1, "name": "Rex", "status": statuses[0]},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := CallTool(c, "pet_find_by_status", map[string]interface{}{
		"status": []interface{}{"available"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Rex") {
		t.Errorf("expected 'Rex' in result, got: %s", result)
	}
}

func TestCallTool_PetAdd(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pet" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(received)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := CallTool(c, "pet_add", map[string]interface{}{
		"name":      "Kitty",
		"photoUrls": []interface{}{"http://example.com/cat.jpg"},
		"status":    "available",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Kitty") {
		t.Errorf("expected 'Kitty' in result, got: %s", result)
	}
	if received["name"] != "Kitty" {
		t.Errorf("body name = %v, want Kitty", received["name"])
	}
}

func TestCallTool_StoreGetInventory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/store/inventory" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"available": 5, "sold": 3})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := CallTool(c, "store_get_inventory", map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "available") {
		t.Errorf("expected 'available' in result, got: %s", result)
	}
}

func TestCallTool_UserLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/login" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		u := r.URL.Query().Get("username")
		p := r.URL.Query().Get("password")
		if u == "" || p == "" {
			t.Error("expected username and password query params")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode("logged-in-token-123")
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := CallTool(c, "user_login", map[string]interface{}{
		"username": "user1",
		"password": "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "logged-in-token-123") {
		t.Errorf("expected token in result, got: %s", result)
	}
}

func TestCallTool_UserGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/alice" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 1, "username": "alice", "email": "alice@example.com",
		})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := CallTool(c, "user_get", map[string]interface{}{"username": "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "alice") {
		t.Errorf("expected 'alice' in result, got: %s", result)
	}
}

func TestCallTool_PetDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/pet/7" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	result, err := CallTool(c, "pet_delete", map[string]interface{}{"petId": float64(7)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "succeeded") {
		t.Errorf("expected success message, got: %s", result)
	}
}

func TestCallTool_UnknownTool(t *testing.T) {
	c := NewClient()
	_, err := CallTool(c, "nonexistent_tool", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestCallTool_MissingRequiredArg(t *testing.T) {
	c := NewClient()
	_, err := CallTool(c, "pet_get_by_id", map[string]interface{}{}) // petId missing
	if err == nil {
		t.Fatal("expected error when required arg 'petId' is missing")
	}
}

// ---------------------------------------------------------------------------
// Handler + Collector tests
// ---------------------------------------------------------------------------

func newSoldPetsHandler(t *testing.T) (*httptest.Server, *Handler) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "findByStatus") {
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 1, "name": "Fido", "status": "sold"},
				{"id": 2, "name": "Luna", "status": "sold"},
			})
			return
		}
		w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)
	return srv, NewHandler(NewClientWithBase(srv.URL))
}

func TestHandler_StartCollection_ImmediateSnapshot(t *testing.T) {
	_, h := newSoldPetsHandler(t)
	reportFile := filepath.Join(t.TempDir(), "report.json")

	result, err := h.CallTool("report_start_collection", map[string]interface{}{
		"report_file":      reportFile,
		"interval_seconds": float64(60), // long interval — won't fire again during test
	})
	if err != nil {
		t.Fatalf("start error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result), "started") {
		t.Errorf("expected 'started' in result, got: %s", result)
	}

	// First snapshot is taken immediately in background — give it time.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(reportFile); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(reportFile); err != nil {
		t.Fatalf("report file not created within 2s: %v", err)
	}

	// Cleanup: stop the goroutine.
	h.CallTool("report_stop_collection", map[string]interface{}{}) //nolint
}

func TestHandler_DoubleStart_Rejected(t *testing.T) {
	_, h := newSoldPetsHandler(t)
	reportFile := filepath.Join(t.TempDir(), "report.json")

	h.CallTool("report_start_collection", map[string]interface{}{ //nolint
		"report_file": reportFile, "interval_seconds": float64(60),
	})

	_, err := h.CallTool("report_start_collection", map[string]interface{}{
		"report_file": reportFile, "interval_seconds": float64(60),
	})
	if err == nil {
		t.Fatal("expected error when starting an already-running collection")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected 'already running' in error, got: %v", err)
	}

	h.CallTool("report_stop_collection", map[string]interface{}{}) //nolint
}

func TestHandler_StopCollection(t *testing.T) {
	_, h := newSoldPetsHandler(t)
	reportFile := filepath.Join(t.TempDir(), "report.json")

	h.CallTool("report_start_collection", map[string]interface{}{ //nolint
		"report_file": reportFile, "interval_seconds": float64(60),
	})
	time.Sleep(150 * time.Millisecond) // let the first snapshot land

	result, err := h.CallTool("report_stop_collection", map[string]interface{}{})
	if err != nil {
		t.Fatalf("stop error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result), "stopped") {
		t.Errorf("expected 'stopped' in result, got: %s", result)
	}
}

func TestHandler_StopWhenNotRunning(t *testing.T) {
	_, h := newSoldPetsHandler(t)

	result, err := h.CallTool("report_stop_collection", map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(strings.ToLower(result), "no active") {
		t.Errorf("expected 'no active' in result, got: %s", result)
	}
}

func TestHandler_CollectionStatus(t *testing.T) {
	_, h := newSoldPetsHandler(t)
	reportFile := filepath.Join(t.TempDir(), "report.json")

	// Not running initially.
	status, err := h.CallTool("report_collection_status", map[string]interface{}{})
	if err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !strings.Contains(strings.ToLower(status), "not running") {
		t.Errorf("expected 'not running' in status, got: %s", status)
	}

	// Start and check running status.
	h.CallTool("report_start_collection", map[string]interface{}{ //nolint
		"report_file": reportFile, "interval_seconds": float64(60),
	})
	defer h.CallTool("report_stop_collection", map[string]interface{}{}) //nolint

	status, err = h.CallTool("report_collection_status", map[string]interface{}{})
	if err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !strings.Contains(strings.ToLower(status), "running") {
		t.Errorf("expected 'running' in status, got: %s", status)
	}
	if !strings.Contains(status, reportFile) {
		t.Errorf("expected report file path in status, got: %s", status)
	}
}

func TestCallTool_ReportShowSold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": 5, "name": "Parrot", "status": "sold"},
		})
	}))
	defer srv.Close()

	reportFile := filepath.Join(t.TempDir(), "sold_report.json")
	c := newTestClient(srv)

	// Seed the file by calling CollectSoldReport directly (it is used internally
	// by the Collector goroutine; calling it here avoids starting a full Handler).
	if _, err := CollectSoldReport(c, reportFile, 300); err != nil {
		t.Fatalf("seed error: %v", err)
	}

	// Show via the package-level CallTool.
	result, err := CallTool(c, "report_show_sold", map[string]interface{}{
		"report_file": reportFile,
	})
	if err != nil {
		t.Fatalf("show error: %v", err)
	}
	if !strings.Contains(result, "Parrot") {
		t.Errorf("expected 'Parrot' in show result, got: %s", result)
	}
	if !strings.Contains(result, "Snapshots collected: 1") {
		t.Errorf("expected snapshot count in result, got: %s", result)
	}
}

func TestCallTool_ReportShowSold_FileNotFound(t *testing.T) {
	c := NewClient()
	_, err := CallTool(c, "report_show_sold", map[string]interface{}{
		"report_file": "/tmp/does_not_exist_report_day19.json",
	})
	if err == nil {
		t.Fatal("expected error for missing report file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// report_prepare_markdown_data
// ---------------------------------------------------------------------------

func seedReportFile(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	reportFile := filepath.Join(t.TempDir(), "sold_report.json")
	c := newTestClient(srv)
	if _, err := CollectSoldReport(c, reportFile, 30); err != nil {
		t.Fatalf("seed CollectSoldReport: %v", err)
	}
	return reportFile
}

func TestCallTool_PrepareMarkdownData_Basic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": float64(1), "name": "Fido", "status": "sold",
				"category": map[string]interface{}{"id": float64(1), "name": "Dogs"},
				"tags":     []interface{}{map[string]interface{}{"id": float64(1), "name": "friendly"}}},
			{"id": float64(2), "name": "Luna", "status": "sold"},
		})
	}))
	defer srv.Close()

	reportFile := seedReportFile(t, srv)
	c := newTestClient(srv)

	result, err := CallTool(c, "report_prepare_markdown_data", map[string]interface{}{
		"report_file": reportFile,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"# Sales Report", "Fido", "Luna", "Dogs", "friendly", "Snapshot Trend", "Sold Pets"} {
		if !strings.Contains(result, want) {
			t.Errorf("expected %q in markdown output, got:\n%s", want, result)
		}
	}
}

func TestCallTool_PrepareMarkdownData_FileNotFound(t *testing.T) {
	c := NewClient()
	_, err := CallTool(c, "report_prepare_markdown_data", map[string]interface{}{
		"report_file": "/tmp/does_not_exist_md_day19.json",
	})
	if err == nil {
		t.Fatal("expected error for missing report file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestCallTool_PrepareMarkdownData_MultipleSnapshots(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		call++
		// Second call returns one extra pet.
		pets := []map[string]interface{}{
			{"id": float64(1), "name": "Fido", "status": "sold"},
		}
		if call > 1 {
			pets = append(pets, map[string]interface{}{"id": float64(3), "name": "Buddy", "status": "sold"})
		}
		json.NewEncoder(w).Encode(pets)
	}))
	defer srv.Close()

	reportFile := filepath.Join(t.TempDir(), "multi.json")
	c := newTestClient(srv)
	if _, err := CollectSoldReport(c, reportFile, 30); err != nil {
		t.Fatalf("snapshot 1: %v", err)
	}
	if _, err := CollectSoldReport(c, reportFile, 30); err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}

	result, err := CallTool(c, "report_prepare_markdown_data", map[string]interface{}{
		"report_file": reportFile,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Snapshots collected:** 2") {
		t.Errorf("expected 2 snapshots in output, got:\n%s", result)
	}
	if !strings.Contains(result, "Fido") || !strings.Contains(result, "Buddy") {
		t.Errorf("expected both pets in output, got:\n%s", result)
	}
}

// ---------------------------------------------------------------------------
// report_save_markdown
// ---------------------------------------------------------------------------

func TestCallTool_SaveMarkdown_Basic(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "report.md")
	c := NewClient()

	content := "# Test Report\n\nHello world.\n"
	result, err := CallTool(c, "report_save_markdown", map[string]interface{}{
		"output_file": outFile,
		"content":     content,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, outFile) {
		t.Errorf("expected output path in result, got: %s", result)
	}

	saved, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}
	if string(saved) != content {
		t.Errorf("saved content mismatch: got %q, want %q", string(saved), content)
	}
}

func TestCallTool_SaveMarkdown_MissingArgs(t *testing.T) {
	c := NewClient()

	if _, err := CallTool(c, "report_save_markdown", map[string]interface{}{
		"content": "some markdown",
	}); err == nil {
		t.Fatal("expected error when output_file is missing")
	}

	if _, err := CallTool(c, "report_save_markdown", map[string]interface{}{
		"output_file": "/tmp/x.md",
	}); err == nil {
		t.Fatal("expected error when content is missing")
	}
}

func TestCallTool_SaveMarkdown_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": float64(5), "name": "Parrot", "status": "sold"},
		})
	}))
	defer srv.Close()

	reportFile := seedReportFile(t, srv)
	outFile := filepath.Join(t.TempDir(), "sales.md")
	c := newTestClient(srv)

	// Step 1: prepare markdown data.
	md, err := CallTool(c, "report_prepare_markdown_data", map[string]interface{}{
		"report_file": reportFile,
	})
	if err != nil {
		t.Fatalf("prepare error: %v", err)
	}

	// Step 2: save to file.
	saveResult, err := CallTool(c, "report_save_markdown", map[string]interface{}{
		"output_file": outFile,
		"content":     md,
	})
	if err != nil {
		t.Fatalf("save error: %v", err)
	}
	if !strings.Contains(saveResult, outFile) {
		t.Errorf("expected output path in save result, got: %s", saveResult)
	}

	saved, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}
	if !strings.Contains(string(saved), "Parrot") {
		t.Errorf("expected 'Parrot' in saved markdown, got:\n%s", string(saved))
	}
}
