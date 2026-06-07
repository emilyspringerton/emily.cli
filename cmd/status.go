// cmd/status.go — emily status
// Cross-repo git state + last Apple per agent from IDUNA.

package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

var repoDefs = []struct {
	Name string
	Path string
}{
	{"TYLER", "/home/fatbaby/TYLER"},
	{"EMILY", "/home/fatbaby/EMILY"},
	{"IDUNA", "/home/fatbaby/IDUNA"},
	{"PRRJECT_FATBABY", "/home/fatbaby/PRRJECT_FATBABY"},
	{"SHANKPIT", "/home/fatbaby/SHANKPIT"},
}

// repoStatus is the machine-readable form of one repo's state.
type repoStatus struct {
	Name        string `json:"name"`
	Branch      string `json:"branch"`
	LastCommit  string `json:"last_commit"`
	DirtyCount  int    `json:"dirty_count"`
	BacklogDone int    `json:"backlog_done"`
	BacklogOpen int    `json:"backlog_open"`
}

type statusOutput struct {
	Timestamp   string            `json:"timestamp"`
	Repos       []repoStatus      `json:"repos"`
	IDUNAOnline bool              `json:"iduna_online"`
	IDUNABase   string            `json:"iduna_base_url"`
	TotalApples int               `json:"total_apples"`
	LastApples  []iduna.Apple     `json:"last_apples,omitempty"`
}

func RunStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	noGit := fs.Bool("no-git", false, "skip git checks")
	noIDUNA := fs.Bool("no-iduna", false, "skip IDUNA Apple query")
	jsonOut := fs.Bool("json", false, "output JSON")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, _ := config.Resolve()

	// Collect repo states
	var repos []repoStatus
	if !*noGit {
		for _, r := range repoDefs {
			repos = append(repos, collectRepoStatus(r.Name, r.Path))
		}
	}

	// Collect IDUNA Apple state
	var apples []iduna.Apple
	idunaOnline := false
	if !*noIDUNA && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		if a, err := client.ListApples(iduna.AppleListFilters{Limit: 100}); err == nil {
			apples = a
			idunaOnline = true
		}
	}

	if *jsonOut {
		out := statusOutput{
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			Repos:       repos,
			IDUNAOnline: idunaOnline,
			IDUNABase:   cfg.IDUNABaseURL,
			TotalApples: len(apples),
		}
		// Last Apple per source_repo
		seen := map[string]bool{}
		for _, a := range apples {
			if !seen[a.SourceRepo] {
				seen[a.SourceRepo] = true
				out.LastApples = append(out.LastApples, a)
			}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}

	// Human-readable output
	fmt.Printf("\n◈ EMILY OS — SYSTEM STATUS | %s\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Println("══════════════════════════════════════════════════════")

	if !*noGit {
		fmt.Println("\n  GIT REPOS")
		fmt.Println("  ─────────")
		for _, r := range repos {
			dirtyTag := ""
			if r.DirtyCount > 0 {
				dirtyTag = fmt.Sprintf(" [+%d dirty]", r.DirtyCount)
			}
			commit := r.LastCommit
			if len(commit) > 60 {
				commit = commit[:59] + "…"
			}
			fmt.Printf("  %-20s  %s%s\n", r.Name, r.Branch, dirtyTag)
			fmt.Printf("  %-20s  %s\n", "", commit)
			if r.BacklogDone+r.BacklogOpen > 0 {
				fmt.Printf("  %-20s  backlog: %d done / %d pending\n", "", r.BacklogDone, r.BacklogOpen)
			}
			fmt.Println()
		}
	}

	if !*noIDUNA {
		if cfg.IDUNAAgentSecret == "" {
			fmt.Printf("  IDUNA: (no credentials — set IDUNA_AGENT_SECRET)\n")
		} else if idunaOnline {
			fmt.Printf("  IDUNA: %s ✓\n", cfg.IDUNABaseURL)
			fmt.Println("  ──────")
			seen := map[string]bool{}
			fmt.Println("  Last Apple per repo:")
			for _, a := range apples {
				if seen[a.SourceRepo] {
					continue
				}
				seen[a.SourceRepo] = true
				ts := a.RecordedAt
				if len(ts) >= 16 {
					ts = ts[:16]
				}
				ts = strings.Replace(ts, "T", " ", 1)
				fmt.Printf("    [%-15s]  %s  #%d  %s\n", a.SourceRepo, ts, a.ID, truncate(a.Title, 45))
			}
			fmt.Printf("  Total apples: %d\n", len(apples))
		} else {
			fmt.Printf("  IDUNA: %s (offline or auth failed)\n", cfg.IDUNABaseURL)
		}
	}

	fmt.Println("\n──────────────────────────────────────────────────────")
	fmt.Println()
	return 0
}

func collectRepoStatus(name, path string) repoStatus {
	rs := repoStatus{Name: name}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return rs
	}
	rs.Branch = gitOutput(path, "rev-parse", "--abbrev-ref", "HEAD")
	rs.LastCommit = gitOutput(path, "log", "-1", "--pretty=%h %s")
	porcelain := strings.TrimSpace(gitOutput(path, "status", "--porcelain"))
	if porcelain != "" {
		rs.DirtyCount = strings.Count(porcelain, "\n") + 1
	}
	rs.BacklogDone = countLines(path, "BACKLOG.md", "- [x]")
	rs.BacklogOpen = countLines(path, "BACKLOG.md", "- [ ]")
	return rs
}

// ── git helpers ───────────────────────────────────────────────────────────────

func gitOutput(repoPath string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

func countLines(repoPath, fname, needle string) int {
	content, err := os.ReadFile(filepath.Join(repoPath, fname))
	if err != nil {
		return 0
	}
	return strings.Count(string(content), needle)
}
