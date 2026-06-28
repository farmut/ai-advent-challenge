package petstore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
