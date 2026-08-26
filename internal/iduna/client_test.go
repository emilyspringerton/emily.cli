package iduna_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

	// Kanban cards -- a real in-memory store, not canned JSON, so
	// list/add/move/delete all actually round-trip against each other.
	kanbanCards := map[int64]*iduna.KanbanCard{}
	var kanbanNextID int64 = 1
	mux.HandleFunc("/api/v1/kanban/cards", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			queue := r.URL.Query().Get("queue")
			out := []iduna.KanbanCard{}
			for _, c := range kanbanCards {
				if queue == "" || c.Queue == queue {
					out = append(out, *c)
				}
			}
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var body struct {
				BacklogItemID string `json:"backlog_item_id"`
				Title         string `json:"title"`
				Queue         string `json:"queue"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.BacklogItemID == "" || body.Title == "" {
				http.Error(w, `{"error":"backlog_item_id and title required"}`, http.StatusBadRequest)
				return
			}
			if body.Queue == "" {
				body.Queue = "backlog"
			}
			id := kanbanNextID
			kanbanNextID++
			kanbanCards[id] = &iduna.KanbanCard{ID: id, BacklogItemID: body.BacklogItemID, Title: body.Title, Queue: body.Queue}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]int64{"id": id})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/kanban/cards/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/kanban/cards/")
		var id int64
		fmt.Sscanf(idStr, "%d", &id)
		card, ok := kanbanCards[id]
		if !ok {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		switch r.Method {
		case http.MethodPatch:
			var body struct {
				Queue string `json:"queue"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			card.Queue = body.Queue
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			delete(kanbanCards, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
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

func TestAuth_AlwaysFetchesFreshEvenWithinExpiryWindow(t *testing.T) {
	// Founder, real-time: "again it ended in an UNAUTHENTICATED error...
	// ok yea we are gonna need to auto refresh the token like every api
	// call or what." Auth() used to skip re-fetching as long as the
	// cached token claimed >5 minutes of life left -- that's exactly the
	// case a token invalidated some OTHER way (e.g. mid-run across an
	// IDUNA restart) couldn't self-heal from. It must hit the auth
	// endpoint every single call now, not just on the first one.
	authCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/agent", func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"access_token":"test-jwt","expires_in":3600}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	if err := c.Auth(); err != nil {
		t.Fatalf("first Auth: %v", err)
	}
	if err := c.Auth(); err != nil {
		t.Fatalf("second Auth: %v", err)
	}
	if authCalls != 2 {
		t.Errorf("expected 2 real auth requests (no caching/skip), got %d", authCalls)
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

func TestGetPromptOVerseNodeBySlug_Found(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/promptoverse/nodes/lil-wayne-paper-craft", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"slug":"lil-wayne-paper-craft","label":"paper-craft","subject":"Lil Wayne","kind":"surreal","ez_prompt":"paper-craft Lil Wayne","expanded_prompt":"grey hoodie"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	n, err := c.GetPromptOVerseNodeBySlug("lil-wayne-paper-craft")
	if err != nil {
		t.Fatalf("GetPromptOVerseNodeBySlug: %v", err)
	}
	if n.Subject != "Lil Wayne" || n.ExpandedPrompt != "grey hoodie" {
		t.Errorf("unexpected node: %+v", n)
	}
}

func TestGetPromptOVerseNodeBySlug_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/promptoverse/nodes/does-not-exist", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	_, err := c.GetPromptOVerseNodeBySlug("does-not-exist")
	if err != iduna.ErrPromptOVerseNodeNotFound {
		t.Errorf("expected ErrPromptOVerseNodeNotFound, got %v", err)
	}
}

func TestAddPromptOVerseVariant_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/agent", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"access_token":"test-jwt","expires_in":3600}`)
	})
	mux.HandleFunc("/api/v1/promptoverse/nodes/lil-wayne-paper-craft/variants", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		fmt.Fprintln(w, `{"status":"variant added","url":"https://okemily.com/prompt-o-verse/lil-wayne-paper-craft/"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	url, err := c.AddPromptOVerseVariant("lil-wayne-paper-craft", iduna.PromptOVerseVariant{
		EZPrompt: "p", ExpandedPrompt: "red hoodie", ImageBase64: "ZmFrZQ==", Note: "red hoodie instead of grey",
	})
	if err != nil {
		t.Fatalf("AddPromptOVerseVariant: %v", err)
	}
	if url != "https://okemily.com/prompt-o-verse/lil-wayne-paper-craft/" {
		t.Errorf("unexpected url: %q", url)
	}
}

func TestAddPromptOVerseVariant_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/agent", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"access_token":"test-jwt","expires_in":3600}`)
	})
	mux.HandleFunc("/api/v1/promptoverse/nodes/does-not-exist/variants", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"node not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")
	_, err := c.AddPromptOVerseVariant("does-not-exist", iduna.PromptOVerseVariant{
		EZPrompt: "p", ExpandedPrompt: "p", ImageBase64: "ZmFrZQ==",
	})
	if err != iduna.ErrPromptOVerseNodeNotFound {
		t.Errorf("expected ErrPromptOVerseNodeNotFound, got %v", err)
	}
}

func TestKanban_AddListMoveDelete_RealRoundTrip(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()
	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")

	id, err := c.AddKanbanCard("S202-27", "Body blocking", "")
	if err != nil {
		t.Fatalf("AddKanbanCard: %v", err)
	}

	cards, err := c.ListKanbanCards("")
	if err != nil {
		t.Fatalf("ListKanbanCards: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != id || cards[0].Queue != "backlog" {
		t.Fatalf("unexpected cards after add: %+v", cards)
	}

	if err := c.MoveKanbanCard(id, "priority"); err != nil {
		t.Fatalf("MoveKanbanCard: %v", err)
	}
	priorityCards, err := c.ListKanbanCards("priority")
	if err != nil {
		t.Fatalf("ListKanbanCards(priority): %v", err)
	}
	if len(priorityCards) != 1 || priorityCards[0].ID != id {
		t.Fatalf("expected the card in the priority queue after move, got %+v", priorityCards)
	}

	if err := c.DeleteKanbanCard(id); err != nil {
		t.Fatalf("DeleteKanbanCard: %v", err)
	}
	remaining, err := c.ListKanbanCards("")
	if err != nil {
		t.Fatalf("ListKanbanCards after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no cards after delete, got %+v", remaining)
	}
}

func TestKanban_AddWithExplicitQueue(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()
	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")

	id, err := c.AddKanbanCard("S202-28", "Ant hero rig", "cruise")
	if err != nil {
		t.Fatalf("AddKanbanCard: %v", err)
	}
	cards, _ := c.ListKanbanCards("cruise")
	if len(cards) != 1 || cards[0].ID != id {
		t.Fatalf("expected the new card directly in the cruise queue, got %+v", cards)
	}
}

func TestKanban_AddRejectsEmptyFields(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()
	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")

	if _, err := c.AddKanbanCard("", "no id", ""); err == nil {
		t.Error("expected an error for an empty backlog_item_id")
	}
}

func TestKanban_DeleteUnknownCardErrors(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()
	c := iduna.New(srv.URL, "EMILY-PRIME", "correct-secret")

	if err := c.DeleteKanbanCard(999); err == nil {
		t.Error("expected an error deleting a card id that doesn't exist")
	}
}
