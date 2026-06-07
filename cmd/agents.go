// cmd/agents.go — emily agents
// Synthesizes agent activity from IDUNA Apples log. Groups by source_repo,
// shows last-seen timestamp, apple count, and most recent apple per agent.
// Gives a quick health view: which agents are alive and what they're doing.

package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

type agentSummary struct {
	Repo        string `json:"repo"`
	TotalApples int    `json:"total_apples"`
	LastSeenAt  string `json:"last_seen_at"`
	LastType    string `json:"last_apple_type"`
	LastTitle   string `json:"last_apple_title"`
	LastID      int64  `json:"last_apple_id"`
	AgeSecs     int64  `json:"age_seconds"`
}

func RunAgents(args []string) int {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	limit := fs.Int("n", 200, "apples to scan (higher = more complete history)")
	jsonOut := fs.Bool("json", false, "output JSON array")
	since := fs.Int("since", 0, "only show agents active in the last N minutes (0 = all)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set")
		return 2
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	apples, err := client.ListApples(iduna.AppleListFilters{Limit: *limit})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 4
	}

	// Group by source_repo — newest apple per repo is already first since IDUNA returns newest-first.
	seen := map[string]*agentSummary{}
	order := []string{} // insertion order = most-recently-active first

	for _, a := range apples {
		if a.SourceRepo == "" {
			continue
		}
		if _, exists := seen[a.SourceRepo]; !exists {
			seen[a.SourceRepo] = &agentSummary{
				Repo:       a.SourceRepo,
				LastSeenAt: a.RecordedAt,
				LastType:   a.AppleType,
				LastTitle:  a.Title,
				LastID:     a.ID,
			}
			order = append(order, a.SourceRepo)
		}
		seen[a.SourceRepo].TotalApples++
	}

	// Compute age and apply --since filter.
	now := time.Now().UTC()
	var agents []agentSummary
	for _, repo := range order {
		s := seen[repo]
		if t, err := time.Parse(time.RFC3339, s.LastSeenAt); err == nil {
			s.AgeSecs = int64(now.Sub(t).Seconds())
		}
		if *since > 0 && s.AgeSecs > int64(*since*60) {
			continue
		}
		agents = append(agents, *s)
	}

	// Sort: most-recently-active first (already in insertion order, but sort to be safe).
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].AgeSecs < agents[j].AgeSecs
	})

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(agents)
		return 0
	}

	// Human-readable table.
	fmt.Printf("\n◈ EMILY OS — AGENTS | %s\n", now.Format("2006-01-02 15:04"))
	fmt.Printf("  scanning last %d apples from %s\n\n", *limit, cfg.IDUNABaseURL)

	if len(agents) == 0 {
		fmt.Println("  (no agent activity found)")
		fmt.Println()
		return 0
	}

	fmt.Printf("  %-18s  %-12s  %-5s  %-20s  %s\n",
		"REPO", "LAST SEEN", "TOTAL", "LAST TYPE", "LAST TITLE")
	fmt.Println("  " + strings.Repeat("─", 90))

	for _, a := range agents {
		lastSeen := formatAge(a.AgeSecs)
		atype := truncate(a.LastType, 20)
		title := truncate(a.LastTitle, 42)
		fmt.Printf("  %-18s  %-12s  %5d  %-20s  %s\n",
			a.Repo, lastSeen, a.TotalApples, atype, title)
	}
	fmt.Println()
	return 0
}

// formatAge returns a human-readable age string (e.g. "2m ago", "3h ago", "just now").
func formatAge(secs int64) string {
	switch {
	case secs < 60:
		return "just now"
	case secs < 3600:
		return fmt.Sprintf("%dm ago", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh ago", secs/3600)
	default:
		return fmt.Sprintf("%dd ago", secs/86400)
	}
}
