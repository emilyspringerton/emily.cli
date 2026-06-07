package iduna_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// mockServer builds a minimal IDUNA-compatible HTTP server.
func mockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("/api/v1/auth/agent", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AgentName   string `json:"agent_name"`
			AgentSecret string `json:"agent_secret"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.AgentSecret != "correct-secret" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"test-jwt","expires_in":3600}`)
	})

	// List apples
	mux.HandleFunc("/api/v1/apples", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{"apples":[
				{"id":10,"agent_id":"1","source_repo":"TYLER","run_id":"r1","apple_type":"rsi_iteration","title":"Build 0019","body":"body text","recorded_at":"2026-06-07T08:00:00Z"},
				{"id":9,"agent_id":"1","source_repo":"EMILY","run_id":"r2","apple_type":"signal_observation","title":"obs","body":"","recorded_at":"2026-06-07T07:00:00Z"}
			]}`)
			return
		}
		// POST apple
		var payload iduna.ApplePayload
		json.NewDecoder(r.Body).Decode(&payload)
		if payload.Title == "" || payload.AppleType == "" {
			http.Error(w, `{"code":"BAD_REQUEST","message":"required fields missing"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintln(w, `{"id":42}`)
	})

	// Get single apple
	mux.HandleFunc("/api/v1/apples/10", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"id":10,"agent_id":"1","source_repo":"TYLER","run_id":"r1","apple_type":"rsi_iteration","title":"Build 0019","body":"full body here","recorded_at":"2026-06-07T08:00:00Z"}`)
	})

	mux.HandleFunc("/api/v1/apples/9999", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})

	return httptest.NewServer(mux)
}

func TestAuth_success(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	if err := c.Auth(); err != nil {
		t.Fatalf("Auth: %v", err)
	}
}

func TestAuth_wrongSecret(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "wrong-secret")
	if err := c.Auth(); err == nil {
		t.Fatal("Auth should fail with wrong secret")
	}
}

func TestListApples_all(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	apples, err := c.ListApples(iduna.AppleListFilters{Limit: 20})
	if err != nil {
		t.Fatalf("ListApples: %v", err)
	}
	if len(apples) != 2 {
		t.Errorf("ListApples: got %d want 2", len(apples))
	}
	if apples[0].ID != 10 {
		t.Errorf("first apple ID: got %d want 10", apples[0].ID)
	}
}

func TestListApples_typeFilter(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	apples, err := c.ListApples(iduna.AppleListFilters{Limit: 20, AppleType: "rsi_iteration"})
	if err != nil {
		t.Fatalf("ListApples: %v", err)
	}
	if len(apples) != 1 {
		t.Errorf("type filter: got %d want 1", len(apples))
	}
	if apples[0].AppleType != "rsi_iteration" {
		t.Errorf("type filter: got %q want rsi_iteration", apples[0].AppleType)
	}
}

func TestPostApple_success(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	id, err := c.PostApple(iduna.ApplePayload{
		AppleType:  "backlog_completion",
		Title:      "test apple",
		Body:       "some body",
		SourceRepo: "CLI",
		RunID:      "run-001",
	})
	if err != nil {
		t.Fatalf("PostApple: %v", err)
	}
	if id != 42 {
		t.Errorf("PostApple id: got %d want 42", id)
	}
}

func TestPostApple_emptyTitle(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	_, err := c.PostApple(iduna.ApplePayload{
		AppleType: "rsi_iteration",
		// Title intentionally empty
		SourceRepo: "CLI",
		RunID:      "run-001",
	})
	if err == nil {
		t.Fatal("PostApple with empty title should fail")
	}
}

func TestGetApple_found(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	a, err := c.GetApple(10)
	if err != nil {
		t.Fatalf("GetApple: %v", err)
	}
	if a.ID != 10 {
		t.Errorf("GetApple ID: got %d want 10", a.ID)
	}
	if a.Body != "full body here" {
		t.Errorf("GetApple body: got %q", a.Body)
	}
}

func TestGetApple_notFound(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	_, err := c.GetApple(9999)
	if err == nil {
		t.Fatal("GetApple 9999 should return error")
	}
}
