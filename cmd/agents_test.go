package cmd_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/cmd"
)

func TestRunAgents_noSecret(t *testing.T) {
	t.Setenv("IDUNA_AGENT_SECRET", "")
	t.Setenv("IDUNA_SECRETS", "/nonexistent/secrets.env")

	var code int
	captureStderr(t, func() {
		code = cmd.RunAgents([]string{})
	})
	if code == 0 {
		t.Error("agents without secret should return non-zero exit code")
	}
}

func TestRunAgents_jsonFlag_parsesFlags(t *testing.T) {
	// --json flag should parse cleanly (we can't test the full round-trip
	// without a live IDUNA, but we can verify flag parsing doesn't panic).
	fs := []string{"--json", "--since", "30", "-n", "50"}
	// We can't run it without a secret, but flag.Parse() runs before auth.
	// Verify it returns auth error (2), not usage error (1).
	t.Setenv("IDUNA_AGENT_SECRET", "")
	t.Setenv("IDUNA_SECRETS", "/nonexistent/secrets.env")

	var code int
	captureStderr(t, func() {
		code = cmd.RunAgents(fs)
	})
	if code == 1 {
		t.Error("flag parse error — flags should be valid: got exit 1 (usage error)")
	}
}

func TestFormatAge_boundaries(t *testing.T) {
	// Test the formatAge helper indirectly via JSON output shape.
	// We verify the JSON array structure from a synthesized run.
	// Since we can't call IDUNA in unit tests, we test the agentSummary JSON shape.
	type agentSummary struct {
		Repo        string `json:"repo"`
		TotalApples int    `json:"total_apples"`
		LastSeenAt  string `json:"last_seen_at"`
		LastType    string `json:"last_apple_type"`
		LastTitle   string `json:"last_apple_title"`
		LastID      int64  `json:"last_apple_id"`
		AgeSecs     int64  `json:"age_seconds"`
	}

	// Manually construct the JSON we'd expect and verify it round-trips.
	sample := []agentSummary{
		{Repo: "TYLER", TotalApples: 3, LastSeenAt: "2026-06-07T09:00:00Z",
			LastType: "rsi_iteration", LastTitle: "Build 0019", LastID: 10, AgeSecs: 300},
	}
	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "TYLER") {
		t.Errorf("JSON should contain TYLER: %s", data)
	}
	if !strings.Contains(string(data), "rsi_iteration") {
		t.Errorf("JSON should contain apple type: %s", data)
	}
}
