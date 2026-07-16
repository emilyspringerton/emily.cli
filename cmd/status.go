// cmd/status.go — emily status
// Cross-repo git state + last Apple per agent from IDUNA.

package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/color"
	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

type processState struct {
	Name    string
	Running bool
	Note    string // optional detail (pid, unit state, etc.)
}

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
	watch := fs.Bool("watch", false, "live-updating mode — refresh every --interval seconds until Ctrl-C")
	interval := fs.Int("interval", 30, "refresh interval in seconds (--watch mode)")
	fatbaby := fs.Bool("fatbaby", false, "also show PRRJECT_FATBABY service process states")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, _ := config.Resolve()

	if *watch {
		return runStatusWatch(cfg, *noGit, *noIDUNA, *interval, *fatbaby)
	}

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
				dirtyTag = color.Warn(fmt.Sprintf(" [+%d dirty]", r.DirtyCount))
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

	// EmilyOS posture (best-effort via emily-agent API)
	agentURL := os.Getenv("EMILY_AGENT_URL")
	if p, blocked, err := fetchPostureFromAgent(agentURL); err == nil {
		label := p
		if blocked {
			label = p + " (LLM blocked)"
		}
		fmt.Printf("  POSTURE: %s\n", label)
	}

	// THE_FIELD archetype engine status (best-effort)
	if reachable, corridor, spirits := fetchArchetypeStatusFromAgent(agentURL); reachable {
		fmt.Printf("  ARCHETYPE ENGINE: online | last_corridor=%s | spirits=%s\n", corridor, spirits)
	}

	printProcesses(collectProcesses(cfg))
	if *fatbaby {
		printProcesses(collectFatBabyProcesses(cfg))
	}

	fmt.Println("\n──────────────────────────────────────────────────────")
	fmt.Println()
	return 0
}

// fetchArchetypeStatusFromAgent queries the emily-agent archetype status proxy.
// Returns (reachable, resonance_corridor, active_spirit_names).
func fetchArchetypeStatusFromAgent(agentURL string) (reachable bool, corridor, spirits string) {
	if agentURL == "" {
		agentURL = "http://localhost:8086"
	}
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Get(strings.TrimRight(agentURL, "/") + "/api/v1/emily/archetype/status")
	if err != nil {
		return false, "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, "", ""
	}
	var body struct {
		Reachable bool   `json:"reachable"`
		Status    string `json:"status"`
		E1Model   string `json:"e1_model"`
		E2Model   string `json:"e2_model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || !body.Reachable {
		return false, "", ""
	}
	spiritsStr := body.E1Model + "/" + body.E2Model
	return true, "—", spiritsStr
}

// fetchPostureFromAgent queries the emily-agent posture endpoint.
func fetchPostureFromAgent(agentURL string) (posture string, llmBlocked bool, err error) {
	if agentURL == "" {
		agentURL = "http://localhost:8086"
	}
	hc := &http.Client{Timeout: 2 * time.Second}
	resp, err := hc.Get(strings.TrimRight(agentURL, "/") + "/api/v1/emily/posture")
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("agent returned %d", resp.StatusCode)
	}
	var body struct {
		Posture    string `json:"posture"`
		LLMBlocked bool   `json:"llm_blocked"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", false, err
	}
	return body.Posture, body.LLMBlocked, nil
}

func runStatusWatch(cfg *config.Config, noGit, noIDUNA bool, intervalSec int, fatbaby bool) int {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// clearScreen sends ANSI codes to move cursor home and clear the screen.
	clearScreen := func() {
		fmt.Print("\033[H\033[2J")
	}

	printOnce := func() {
		clearScreen()
		var repos []repoStatus
		if !noGit {
			for _, r := range repoDefs {
				repos = append(repos, collectRepoStatus(r.Name, r.Path))
			}
		}
		var apples []iduna.Apple
		idunaOnline := false
		if !noIDUNA && cfg.IDUNAAgentSecret != "" {
			client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
			if a, err := client.ListApples(iduna.AppleListFilters{Limit: 100}); err == nil {
				apples = a
				idunaOnline = true
			}
		}

		fmt.Printf("\n◈ EMILY OS — LIVE STATUS | refresh every %ds | ctrl-c to stop | %s\n",
			intervalSec, time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println("══════════════════════════════════════════════════════")

		if !noGit {
			fmt.Println("\n  GIT REPOS")
			fmt.Println("  ─────────")
			for _, r := range repos {
				dirtyTag := ""
				if r.DirtyCount > 0 {
					dirtyTag = color.Warn(fmt.Sprintf(" [+%d dirty]", r.DirtyCount))
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

		if !noIDUNA {
			if cfg.IDUNAAgentSecret == "" {
				fmt.Printf("  IDUNA: (no credentials)\n")
			} else if idunaOnline {
				fmt.Printf("  IDUNA: %s ✓  (%d apples total)\n", cfg.IDUNABaseURL, len(apples))
				fmt.Println("  ──────")
				seen := map[string]bool{}
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
					fmt.Printf("    [%-15s]  %s  #%d  %s\n",
						a.SourceRepo, ts, a.ID, truncate(a.Title, 45))
				}
			} else {
				fmt.Printf("  IDUNA: %s (offline or auth failed)\n", cfg.IDUNABaseURL)
			}
		}

		printProcesses(collectProcesses(cfg))
		if fatbaby {
			printProcesses(collectFatBabyProcesses(cfg))
		}

		fmt.Println("\n──────────────────────────────────────────────────────")
	}

	printOnce()

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			fmt.Println("\n  stopped.")
			return 0
		case <-ticker.C:
			printOnce()
		}
	}
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

// collectProcesses checks daemon liveness without blocking.
func collectProcesses(cfg *config.Config) []processState {
	procs := []processState{}
	// All external commands use a short timeout so TUI never hangs on a fresh boot
	// where D-Bus or other system services may be unavailable.
	cmdCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// observation-watcher — anchored to its real invocation ("cmd/observation-watcher
	// --root ..."), not a bare "observation-watcher" substring — a bare pattern
	// matches any process whose cmdline happens to mention the name (e.g. an
	// operator's shell command or an interactive session discussing it by name).
	out, _ := exec.CommandContext(cmdCtx, "pgrep", "-f", "cmd/observation-watcher --root").Output()
	pids := strings.TrimSpace(string(out))
	if pids != "" {
		procs = append(procs, processState{Name: "observation-watcher", Running: true, Note: "pid " + strings.ReplaceAll(pids, "\n", ",")})
	} else {
		procs = append(procs, processState{Name: "observation-watcher", Running: false})
	}

	// emily-agent daemon runs as `go run . -- --daemon` (cwd=EMILY/emily-agent) —
	// its cmdline never contains the literal substring "emily-agent", so pgrep -f
	// can never match it (and a bare "emily-agent" pattern instead matches
	// anything else mentioning the name). Use the PID file written by
	// startEmilyAgent (cmd/start.go) instead, same liveness convention as
	// tuiPIDFile below.
	if pid, alive := pidFileAlive(emilyAgentPIDPath(cfg)); alive {
		procs = append(procs, processState{Name: "emily-agent", Running: true, Note: fmt.Sprintf("pid %d", pid)})
	} else {
		procs = append(procs, processState{Name: "emily-agent", Running: false})
	}

	// emily-sync systemd user unit — systemctl --user can block if D-Bus is unavailable
	// (e.g. on a fresh boot before the user session is fully initialized).
	unitCtx, unitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer unitCancel()
	unitOut, err := exec.CommandContext(unitCtx, "systemctl", "--user", "is-active", "emily-sync.service").Output()
	unitState := strings.TrimSpace(string(unitOut))
	if err == nil && unitState == "active" {
		procs = append(procs, processState{Name: "emily-sync.service", Running: true, Note: unitState})
	} else if unitState != "" && unitState != "inactive" {
		procs = append(procs, processState{Name: "emily-sync.service", Running: false, Note: unitState})
	} else if unitCtx.Err() != nil {
		procs = append(procs, processState{Name: "emily-sync.service", Running: false, Note: "D-Bus timeout"})
	} else {
		procs = append(procs, processState{Name: "emily-sync.service", Running: false, Note: "not installed"})
	}

	// Latest observation file age
	latestLink := filepath.Join(cfg.FatBabyRoot, "var/emily-observations/latest.json")
	if fi, err := os.Lstat(latestLink); err == nil {
		target, _ := os.Readlink(latestLink)
		age := time.Since(fi.ModTime()).Round(time.Minute)
		note := fmt.Sprintf("%s  (%s old)", target, age)
		procs = append(procs, processState{Name: "latest observation", Running: true, Note: note})
	}

	// Prime tasks directory — show pending task count and most recent file age
	tasksDir := filepath.Join(cfg.EmilyRoot, "signals/tasks")
	if entries, err := os.ReadDir(tasksDir); err == nil {
		var newest os.FileInfo
		var taskCount int
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			taskCount++
			if fi, err := e.Info(); err == nil {
				if newest == nil || fi.ModTime().After(newest.ModTime()) {
					newest = fi
				}
			}
		}
		if taskCount == 0 {
			procs = append(procs, processState{Name: "prime-tasks/", Running: true, Note: "empty"})
		} else {
			age := time.Since(newest.ModTime()).Round(time.Minute)
			note := fmt.Sprintf("%d file(s)  newest %s old", taskCount, age)
			procs = append(procs, processState{Name: "prime-tasks/", Running: true, Note: note})
		}
	}

	// emily tui — check PID file
	if raw, err := os.ReadFile(tuiPIDFile); err == nil {
		pid := strings.TrimSpace(string(raw))
		if pidInt, err := strconv.Atoi(pid); err == nil && pidInt > 0 {
			alive := false
			if proc, err := os.FindProcess(pidInt); err == nil {
				// kill -0 checks if process exists without sending a real signal
				alive = proc.Signal(syscall.Signal(0)) == nil
			}
			procs = append(procs, processState{Name: "emily-tui", Running: alive,
				Note: fmt.Sprintf("pid %s", pid)})
		}
	}

	return procs
}

// collectFatBabyProcesses returns processState entries for PRRJECT_FATBABY services.
func collectFatBabyProcesses(cfg *config.Config) []processState {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	fatbaby := []struct{ name, pattern string }{
		{"newssite", "cmd/newssite"},
		{"signalapi", "cmd/signalapi"},
		{"eps-processor", "eps-processor"},
		{"eps-reconciler", "eps-reconciler"},
		{"entity-graph", "entity-graph"},
		{"observation-watcher", "cmd/observation-watcher --root"},
		{"secwatch", "cmd/secwatch"},
	}

	var procs []processState
	for _, fb := range fatbaby {
		out, _ := exec.CommandContext(ctx, "pgrep", "-f", fb.pattern).Output()
		pids := strings.TrimSpace(string(out))
		if pids != "" {
			procs = append(procs, processState{Name: fb.name, Running: true,
				Note: "pid " + strings.ReplaceAll(pids, "\n", ",")})
		} else {
			procs = append(procs, processState{Name: fb.name, Running: false})
		}
	}

	// IDUNA — check health endpoint
	idCtx, idCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer idCancel()
	base := cfg.IDUNABaseURL
	if base == "" {
		base = "http://localhost:8080"
	}
	req, err := newGetRequest(idCtx, base+"/health")
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			procs = append(procs, processState{Name: "IDUNA (:8080)", Running: resp.StatusCode < 500,
				Note: fmt.Sprintf("HTTP %d", resp.StatusCode)})
		} else {
			procs = append(procs, processState{Name: "IDUNA (:8080)", Running: false, Note: "offline"})
		}
	}

	return procs
}

func newGetRequest(ctx context.Context, url string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
}

func printProcesses(procs []processState) {
	fmt.Println("\n  PROCESSES")
	fmt.Println("  ─────────")
	for _, p := range procs {
		sym := color.Severity("✓", "info")
		label := p.Name
		note := p.Note
		if !p.Running {
			sym = color.Severity("✗", "warn")
		}
		if note != "" {
			fmt.Printf("  %s  %-25s  %s\n", sym, label, note)
		} else {
			fmt.Printf("  %s  %s\n", sym, label)
		}
	}
}
