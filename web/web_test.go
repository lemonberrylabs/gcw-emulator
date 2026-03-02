package web

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/lemonberrylabs/gcw-emulator/pkg/store"
)

func setupTestApp(t *testing.T) (*fiber.App, *store.Store) {
	t.Helper()
	s := store.New()
	h := New(s, "test-project", "us-central1")
	app := fiber.New()
	h.Register(app)
	return app, s
}

func TestDashboardEmpty(t *testing.T) {
	app, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/ui", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !containsStr(html, "Dashboard") {
		t.Error("expected Dashboard in response")
	}
	if !containsStr(html, "GCW") {
		t.Error("expected GCW brand in response")
	}
	if !containsStr(html, "No workflows deployed") {
		t.Error("expected empty state message")
	}
}

func TestDashboardWithData(t *testing.T) {
	app, s := setupTestApp(t)

	_, err := s.CreateWorkflow("projects/test-project/locations/us-central1", "hello-world",
		"main:\n  steps:\n    - say_hello:\n        return: \"Hello\"", "A test workflow")
	if err != nil {
		t.Fatalf("failed to create workflow: %v", err)
	}

	req := httptest.NewRequest("GET", "/ui", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !containsStr(html, "hello-world") {
		t.Error("expected workflow name in response")
	}
}

func TestWorkflowList(t *testing.T) {
	app, s := setupTestApp(t)

	s.CreateWorkflow("projects/test-project/locations/us-central1", "wf-one",
		"main:\n  steps:\n    - s1:\n        return: 1", "First workflow")
	s.CreateWorkflow("projects/test-project/locations/us-central1", "wf-two",
		"main:\n  steps:\n    - s1:\n        return: 2", "Second workflow")

	req := httptest.NewRequest("GET", "/ui/workflows", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !containsStr(html, "wf-one") {
		t.Error("expected wf-one in response")
	}
	if !containsStr(html, "wf-two") {
		t.Error("expected wf-two in response")
	}
}

func TestWorkflowDetail(t *testing.T) {
	app, s := setupTestApp(t)

	source := "main:\n  steps:\n    - say_hello:\n        return: \"Hello\""
	s.CreateWorkflow("projects/test-project/locations/us-central1", "my-wf", source, "Test desc")

	req := httptest.NewRequest("GET", "/ui/workflows/my-wf", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !containsStr(html, "my-wf") {
		t.Error("expected workflow ID in response")
	}
	if !containsStr(html, "Test desc") {
		t.Error("expected description in response")
	}
	if !containsStr(html, "say_hello") {
		t.Error("expected source content in response")
	}
	if !containsStr(html, "Trigger Execution") {
		t.Error("expected trigger button in response")
	}
}

func TestWorkflowNotFound(t *testing.T) {
	app, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/ui/workflows/nonexistent", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !containsStr(html, "Not Found") {
		t.Error("expected not found message")
	}
}

func TestRootRedirect(t *testing.T) {
	app, _ := setupTestApp(t)

	req := httptest.NewRequest("GET", "/", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 302 {
		t.Fatalf("expected 302 redirect, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/ui" {
		t.Fatalf("expected redirect to /ui, got %s", loc)
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
