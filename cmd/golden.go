// cmd/golden.go — emily golden {list|enable|disable|status}
//
// S200-04: real CI/build-status polling for the "golden repos" — the small
// set of repos whose CI health is worth checking at the end of a work
// iteration. Founder real-time, built up across several messages: "we need
// a tool to poll all our golden repo statuses at the end of a work
// iteration" -> "we already have a tool for checking that status that needs
// to be upgraded to be written in pure parena now if possible (maybe not
// from tls but can we have more of the code for that in parena?" -> "and
// then build the mod into emily cli to check our golden repos" -> "we will
// need a way to turn repos on and off".
//
// Scope decision, flagged not silently made: this stays pure Go, not a
// PARENA mod. The founder's own carve-out ("maybe not from tls") already
// puts the actual HTTPS/JSON-over-the-wire work outside PARENA's own
// current maturity — but the harder blocker is architectural: every other
// PARENA-mod integration in this monorepo (EmilyOS's fsaclmod, PITVIPER's
// scrollmod) links compiled PARENA C in via cgo, and emily.cli's own CI
// (S202-20, same session) just started cross-compiling real distributable
// releases for linux/darwin/windows as a *pure* Go binary specifically
// because it has no cgo dependency anywhere — adding one here for a single
// JSON-parsing helper would regress that cross-compile for the sake of a
// tool this item's own founder-quote already calls "maybe not" for the
// networking half anyway. Not this item's call to reverse unilaterally
// (same class of decision SECTION 195's own S195-03 already flagged rather
// than acted on).
//
// Real per-repo enable/disable config lives in
// EMILY/context/golden-repos.json, edited via `emily golden enable/disable`
// rather than by hand.
package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

type goldenRepo struct {
	Name    string `json:"name"`
	GitHub  string `json:"github"`
	Enabled bool   `json:"enabled"`
}

type goldenRepoConfig struct {
	Comment string       `json:"_comment,omitempty"`
	Repos   []goldenRepo `json:"repos"`
}

// RunGolden dispatches emily golden subcommands.
func RunGolden(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "emily golden: missing subcommand. Try: list, enable <name>, disable <name>, status")
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	path := goldenRepoConfigPath(cfg)

	switch args[0] {
	case "list":
		return runGoldenList(path)
	case "enable":
		return runGoldenToggle(path, args[1:], true)
	case "disable":
		return runGoldenToggle(path, args[1:], false)
	case "status":
		return runGoldenStatus(path)
	default:
		fmt.Fprintf(os.Stderr, "emily golden: unknown subcommand %q — try: list, enable <name>, disable <name>, status\n", args[0])
		return 1
	}
}

func goldenRepoConfigPath(cfg *config.Config) string {
	return filepath.Join(cfg.EmilyRoot, "context", "golden-repos.json")
}

func loadGoldenRepoConfig(path string) (*goldenRepoConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c goldenRepoConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}

func saveGoldenRepoConfig(path string, c *goldenRepoConfig) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func runGoldenList(path string) int {
	c, err := loadGoldenRepoConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily golden list: %v\n", err)
		return 1
	}
	for _, r := range c.Repos {
		mark := "off"
		if r.Enabled {
			mark = "ON "
		}
		fmt.Printf("  [%s] %-20s %s\n", mark, r.Name, r.GitHub)
	}
	return 0
}

func runGoldenToggle(path string, args []string, enable bool) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "emily golden: missing repo name")
		return 1
	}
	name := args[0]
	c, err := loadGoldenRepoConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily golden: %v\n", err)
		return 1
	}
	found := false
	for i := range c.Repos {
		if strings.EqualFold(c.Repos[i].Name, name) {
			c.Repos[i].Enabled = enable
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "emily golden: no repo named %q in %s\n", name, path)
		return 1
	}
	if err := saveGoldenRepoConfig(path, c); err != nil {
		fmt.Fprintf(os.Stderr, "emily golden: write %s: %v\n", path, err)
		return 1
	}
	state := "disabled"
	if enable {
		state = "enabled"
	}
	fmt.Printf("✓ %s %s\n", name, state)
	return 0
}

// ghRun is the subset of the GitHub Actions "list workflow runs" response
// this tool actually uses.
type ghRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"head_sha"`
	Name       string `json:"name"`
	HTMLURL    string `json:"html_url"`
	UpdatedAt  string `json:"updated_at"`
}

type ghRunsResponse struct {
	WorkflowRuns []ghRun `json:"workflow_runs"`
}

func runGoldenStatus(path string) int {
	c, err := loadGoldenRepoConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily golden status: %v\n", err)
		return 1
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "note: GITHUB_TOKEN not set — polling the public API unauthenticated (60 req/hr rate limit)")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	anyFail := false
	for _, r := range c.Repos {
		if !r.Enabled {
			continue
		}
		run, err := fetchLatestRun(client, githubAPIBase, r.GitHub, token)
		if err != nil {
			fmt.Printf("  %-20s  ERROR: %v\n", r.Name, err)
			anyFail = true
			continue
		}
		if run == nil {
			fmt.Printf("  %-20s  (no CI runs found)\n", r.Name)
			continue
		}
		icon := "⏳"
		switch {
		case run.Status != "completed":
			icon = "⏳"
		case run.Conclusion == "success":
			icon = "✓"
		default:
			icon = "✗"
			anyFail = true
		}
		conclusion := run.Conclusion
		if conclusion == "" {
			conclusion = run.Status
		}
		fmt.Printf("  %s %-20s  %-10s %s  %s\n", icon, r.Name, conclusion, run.HeadSHA[:min(7, len(run.HeadSHA))], run.HTMLURL)
	}
	if anyFail {
		return 1
	}
	return 0
}

// githubAPIBase is a var, not a const, so tests can point fetchLatestRun at
// a real httptest server instead of api.github.com.
var githubAPIBase = "https://api.github.com"

func fetchLatestRun(client *http.Client, apiBase, githubRepo, token string) (*ghRun, error) {
	url := fmt.Sprintf("%s/repos/%s/actions/runs?per_page=1", apiBase, githubRepo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github api %d for %s", resp.StatusCode, githubRepo)
	}
	var parsed ghRunsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if len(parsed.WorkflowRuns) == 0 {
		return nil, nil
	}
	return &parsed.WorkflowRuns[0], nil
}
