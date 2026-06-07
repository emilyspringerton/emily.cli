// cmd/status.go — emily status
// Cross-repo git state + last Apple per agent from IDUNA.

package cmd

import (
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
	Name   string
	Path   string
	Branch string
}{
	{"TYLER", "/home/fatbaby/TYLER", "main"},
	{"EMILY", "/home/fatbaby/EMILY", "main"},
	{"IDUNA", "/home/fatbaby/IDUNA", "main"},
	{"PRRJECT_FATBABY", "/home/fatbaby/PRRJECT_FATBABY", "main"},
	{"SHANKPIT", "/home/fatbaby/SHANKPIT", "master"},
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

	fmt.Printf("\n◈ EMILY OS — SYSTEM STATUS | %s\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Println("══════════════════════════════════════════════════════")

	if !*noGit {
		fmt.Println("\n  GIT REPOS")
		fmt.Println("  ─────────")
		for _, r := range repoDefs {
			printRepoStatus(r.Name, r.Path, *jsonOut)
		}
	}

	if !*noIDUNA && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		apples, err := client.ListApples(iduna.AppleListFilters{Limit: 100})
		if err != nil {
			fmt.Printf("  IDUNA: %s (offline or auth failed)\n", cfg.IDUNABaseURL)
		} else {
			fmt.Printf("  IDUNA: %s ✓\n", cfg.IDUNABaseURL)
			fmt.Println("  ──────")
			printLastApplePerRepo(apples)
			fmt.Printf("  Total apples: %d\n", len(apples))
		}
	} else if !*noIDUNA {
		fmt.Printf("  IDUNA: (no credentials — set IDUNA_AGENT_SECRET)\n")
	}

	fmt.Println("\n──────────────────────────────────────────────────────")
	fmt.Println()
	return 0
}

func printRepoStatus(name, path string, _ bool) {
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		fmt.Printf("  %-20s  (no git)\n\n", name)
		return
	}

	branch := gitOutput(path, "rev-parse", "--abbrev-ref", "HEAD")
	lastCommit := gitOutput(path, "log", "-1", "--pretty=%h %s")
	if len(lastCommit) > 60 {
		lastCommit = lastCommit[:59] + "…"
	}
	porcelain := strings.TrimSpace(gitOutput(path, "status", "--porcelain"))
	dirtyTag := ""
	if porcelain != "" {
		n := strings.Count(porcelain, "\n") + 1
		dirtyTag = fmt.Sprintf(" [+%d dirty]", n)
	}

	pending := countLines(path, "BACKLOG.md", "- [ ]")
	done := countLines(path, "BACKLOG.md", "- [x]")

	fmt.Printf("  %-20s  %s%s\n", name, branch, dirtyTag)
	fmt.Printf("  %-20s  %s\n", "", lastCommit)
	if done+pending > 0 {
		fmt.Printf("  %-20s  backlog: %d done / %d pending\n", "", done, pending)
	}
	fmt.Println()
}

func printLastApplePerRepo(apples []iduna.Apple) {
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
		title := truncate(a.Title, 45)
		fmt.Printf("    [%-15s]  %s  #%d  %s\n", a.SourceRepo, ts, a.ID, title)
	}
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
