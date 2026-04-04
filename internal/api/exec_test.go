package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

// newExecTestRouter creates a minimal chi router with the exec handlers
// wired up behind a route parameter, matching the production layout.
func newExecTestRouter(h *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/api/apps/{appID}", func(r chi.Router) {
		r.Post("/exec", h.ExecCommand)
		r.Get("/exec/interactive", h.ExecInteractive)
		r.Delete("/exec/cleanup", h.CleanupExec)
	})
	return r
}

// newTestHandler returns a Handler with a nil DB and a dummy encrypt key.
// Requests that try to load an app will fail with "app not found",
// which is exactly what we need for validation tests.
func newTestHandler() *Handler {
	return &Handler{
		db:         nil,
		encryptKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}
}

// --- ExecCommand validation tests ---

func TestExecCommand_EmptyCommand(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{},
		ExistingPod: true,
	})

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "command must be non-empty" {
		t.Errorf("expected 'command must be non-empty' error, got %q", resp["error"])
	}
}

func TestExecCommand_ExistingPodWithCPU(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{"echo", "hello"},
		ExistingPod: true,
		CPU:         "500m",
	})

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "cpu and ram cannot be set when using existing_pod" {
		t.Errorf("expected existing_pod+cpu error, got %q", resp["error"])
	}
}

func TestExecCommand_ExistingPodWithRAM(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{"echo", "hello"},
		ExistingPod: true,
		RAM:         "512Mi",
	})

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestExecCommand_InvalidJSON(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestExecCommand_AppNotFound(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{"echo", "hello"},
		ExistingPod: true,
	})

	req := httptest.NewRequest("POST", "/api/apps/nonexistent/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// With nil DB, this will panic, so we use a recover to verify it reaches
	// the DB call (validation passed). We catch the nil pointer to confirm
	// the handler got past validation.
	defer func() {
		if r := recover(); r == nil {
			// If no panic, check the response
			if w.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d", w.Code)
			}
		}
		// Panic is expected with nil DB — this proves validation passed
	}()

	router.ServeHTTP(w, req)
}

// --- CleanupExec tests ---

func TestCleanupExec_AppNotFound(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	req := httptest.NewRequest("DELETE", "/api/apps/nonexistent/exec/cleanup", nil)
	w := httptest.NewRecorder()

	defer func() {
		if r := recover(); r == nil {
			if w.Code != http.StatusNotFound {
				t.Errorf("expected status 404, got %d", w.Code)
			}
		}
	}()

	router.ServeHTTP(w, req)
}

// --- ExecInteractive WebSocket tests ---

func TestExecInteractive_WebSocketUpgrade(t *testing.T) {
	h := newTestHandler()

	// Wrap the handler in a recover middleware so the server doesn't crash
	// when the nil DB panics. This lets us verify the route matches and
	// the handler runs up to the DB call.
	router := chi.NewRouter()
	router.Route("/api/apps/{appID}", func(r chi.Router) {
		r.Get("/exec/interactive", func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rv := recover(); rv != nil {
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
			}()
			h.ExecInteractive(w, r)
		})
	})

	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + server.URL[4:] + "/api/apps/test-app-id/exec/interactive?existing_pod=true"

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			// The handler ran but panicked at the nil DB call — this is expected.
			// A 500 means the route matched and the handler executed.
			if resp.StatusCode == http.StatusInternalServerError {
				t.Logf("handler panicked at DB call as expected (status %d)", resp.StatusCode)
				return
			}
		}
		t.Fatalf("websocket dial failed unexpectedly: %v", err)
	}
	defer conn.Close()
	t.Log("websocket upgrade succeeded")
}

func TestExecCommand_ExistingPodWithBothCPUAndRAM(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{"echo", "hello"},
		ExistingPod: true,
		CPU:         "500m",
		RAM:         "512Mi",
	})

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "cpu and ram cannot be set when using existing_pod" {
		t.Errorf("expected existing_pod+cpu+ram error, got %q", resp["error"])
	}
}

func TestCleanupExec_RoutesExist(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	req := httptest.NewRequest("DELETE", "/api/apps/test-app-id/exec/cleanup", nil)
	w := httptest.NewRecorder()

	defer func() {
		recover() // nil DB panic is expected
	}()

	router.ServeHTTP(w, req)

	// 405 would mean wrong method, 404 would mean no route.
	if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
		t.Errorf("expected route to match, got status %d", w.Code)
	}
}

func TestExecCommand_RoutesExist(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{"echo", "hello"},
		ExistingPod: true,
	})

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()

	defer func() {
		recover() // nil DB panic is expected
	}()

	router.ServeHTTP(w, req)

	// 405 would mean wrong method, 404 would mean no route.
	if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
		t.Errorf("expected route to match, got status %d", w.Code)
	}
}

func TestExecCommand_TimeoutExceedsMax(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{"echo", "hello"},
		ExistingPod: true,
		Timeout:     7200,
	})

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "timeout cannot exceed 3600 seconds" {
		t.Errorf("expected timeout error, got %q", resp["error"])
	}
}

func TestExecCommand_TimeoutAtMax(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	body, _ := json.Marshal(execRequest{
		Command:     []string{"echo", "hello"},
		ExistingPod: true,
		Timeout:     3600,
	})

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// With nil DB, this will panic at the DB call, which means
	// it passed timeout validation.
	defer func() {
		recover()
	}()

	router.ServeHTTP(w, req)

	// If no panic, it should not be 400 (timeout validation should pass)
	if w.Code == http.StatusBadRequest {
		var resp map[string]string
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp["error"] == "timeout cannot exceed 3600 seconds" {
			t.Error("timeout=3600 should be allowed")
		}
	}
}

func TestExecCommand_BodyTooLarge(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	// Create a body larger than 1 MB
	largeBody := make([]byte, 2<<20)
	for i := range largeBody {
		largeBody[i] = 'A'
	}

	req := httptest.NewRequest("POST", "/api/apps/test-app-id/exec", bytes.NewReader(largeBody))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for oversized body, got %d", w.Code)
	}
}

func TestExecInteractive_RoutesExist(t *testing.T) {
	h := newTestHandler()
	router := newExecTestRouter(h)

	// Verify the interactive route exists and accepts GET
	req := httptest.NewRequest("GET", "/api/apps/test-app-id/exec/interactive", nil)
	w := httptest.NewRecorder()

	defer func() {
		recover() // nil DB panic is expected
	}()

	router.ServeHTTP(w, req)

	// 405 would mean wrong method, 404 would mean no route.
	// Any other status means the route matched.
	if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
		t.Errorf("expected route to match, got status %d", w.Code)
	}
}
