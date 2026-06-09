// cmd/sync.go — emily sync
// Syncs PRRJECT_FATBABY observations → IDUNA as signal_observation Apples.
// State-tracked to avoid double-posting.

package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

const defaultSyncLimit = 10

func RunSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	all := fs.Bool("all", false, "backfill all (not just new)")
	dryRun := fs.Bool("dry-run", false, "show what would be posted, don't POST")
	limit := fs.Int("limit", defaultSyncLimit, "max observations to process per pass")
	watch := fs.Bool("watch", false, "run in daemon mode — poll for new obs files until Ctrl-C")
	interval := fs.Int("interval", 10, "poll interval in seconds (--watch mode)")
	jsonOut := fs.Bool("json", false, "output JSON")
	appleGitDir := fs.String("apples-git-dir", "", "git repo dir — write each posted Apple as JSON and auto-commit")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	obsDir := filepath.Join(cfg.FatBabyRoot, "var", "emily-observations")
	stateFile := filepath.Join(cfg.EmilyRoot, "var", "fatbaby-synced.txt")

	var client *iduna.Client
	if !*dryRun {
		if cfg.IDUNAAgentSecret == "" {
			fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set")
			return 2
		}
		client = iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	}

	if *watch {
		return runSyncWatch(obsDir, stateFile, client, *limit, *interval, *jsonOut, *appleGitDir)
	}

	if !*jsonOut {
		fmt.Printf("\n◈ EMILY OS — FATBABY OBSERVATION SYNC | %s\n", time.Now().Format("2006-01-02 15:04"))
	}
	posted := loadPosted(stateFile)
	n, skipped := syncPass(obsDir, stateFile, client, posted, *all, *limit, *dryRun, *jsonOut, *appleGitDir)
	if !*jsonOut {
		fmt.Printf("\n  Done. Posted: %d | Skipped (already synced): %d\n\n", n, skipped)
	}
	return 0
}

func runSyncWatch(obsDir, stateFile string, client *iduna.Client, limit, intervalSec int, jsonOut bool, appleGitDir string) int {
	if !jsonOut {
		fmt.Printf("\n◈ EMILY OS — SYNC WATCH | poll every %ds | ctrl-c to stop\n", intervalSec)
		fmt.Printf("  obsDir:    %s\n", obsDir)
		fmt.Printf("  stateFile: %s\n", stateFile)
		if appleGitDir != "" {
			fmt.Printf("  apples:    %s (git archive)\n", appleGitDir)
		}
		fmt.Println()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	// Run once immediately before first tick
	posted := loadPosted(stateFile)
	n, _ := syncPass(obsDir, stateFile, client, posted, false, limit, false, jsonOut, appleGitDir)
	if !jsonOut && n > 0 {
		fmt.Printf("  [%s] posted %d new observation(s)\n", time.Now().Format("15:04:05"), n)
	}

	for {
		select {
		case <-sigCh:
			if !jsonOut {
				fmt.Println("\n  stopped.")
			}
			return 0
		case <-ticker.C:
			posted = loadPosted(stateFile) // refresh state each poll
			n, _ := syncPass(obsDir, stateFile, client, posted, false, limit, false, jsonOut, appleGitDir)
			if !jsonOut && n > 0 {
				fmt.Printf("  [%s] posted %d new observation(s)\n", time.Now().Format("15:04:05"), n)
			}
		}
	}
}

func syncPass(obsDir, stateFile string, client *iduna.Client, posted map[string]bool, all bool, limit int, dryRun bool, jsonOut bool, appleGitDir string) (nPosted, nSkipped int) {
	entries, err := filepath.Glob(filepath.Join(obsDir, "*.json"))
	if err != nil || len(entries) == 0 {
		return 0, 0
	}
	sort.Sort(sort.Reverse(sort.StringSlice(entries)))

	for _, fpath := range entries {
		if nPosted >= limit {
			break
		}
		fname := filepath.Base(fpath)
		if fname == "latest.json" {
			continue
		}
		if posted[fname] && !all {
			nSkipped++
			continue
		}
		if posted[fname] {
			nSkipped++
			continue
		}

		payload, err := buildAppleFromObs(fpath, fname)
		if err != nil {
			if !jsonOut {
				fmt.Printf("  SKIP: %s (%v)\n", fname, err)
			}
			continue
		}

		if dryRun {
			if !jsonOut {
				fmt.Printf("  [DRY-RUN] %s → %s\n", fname, truncate(payload.Title, 70))
			}
			nPosted++
			continue
		}

		id, err := client.PostApple(*payload)
		if err != nil {
			if !jsonOut {
				fmt.Printf("  FAIL: %s (%v)\n", fname, err)
			}
			continue
		}

		if !jsonOut {
			fmt.Printf("  Apple #%d ← %s\n", id, fname)
		} else {
			fmt.Printf(`{"apple_id":%d,"file":%q}`+"\n", id, fname)
		}
		markPosted(stateFile, fname)
		posted[fname] = true
		nPosted++
		// Archive to git repo if configured.
		if appleGitDir != "" {
			archiveAppleToGit(appleGitDir, id, payload, !jsonOut)
			updateManifest(appleGitDir, id, payload, !jsonOut)
		}
	}
	return nPosted, nSkipped
}

// ── helpers ───────────────────────────────────────────────────────────────────

type obsFile struct {
	Timestamp    string `json:"timestamp"`
	Summary      string `json:"summary"`
	Severity     string `json:"severity"`
	Findings     string `json:"findings"`
	SuggestedFix string `json:"suggested_fix"`
}

func buildAppleFromObs(fpath, fname string) (*iduna.ApplePayload, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, err
	}
	var o obsFile
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}

	summary := o.Summary
	if summary == "" {
		summary = "(no summary)"
	}
	severity := o.Severity
	if severity == "" {
		severity = "info"
	}

	title := fmt.Sprintf("FatBaby obs [%s]: %s", severity, summary)
	if len(title) > 100 {
		title = title[:99] + "…"
	}

	var bodyParts []string
	bodyParts = append(bodyParts, "severity: "+severity)
	if o.Timestamp != "" {
		bodyParts = append(bodyParts, "timestamp: "+o.Timestamp)
	}
	if o.Findings != "" {
		bodyParts = append(bodyParts, "\nfindings:\n"+limit800(o.Findings))
	}
	if o.SuggestedFix != "" {
		bodyParts = append(bodyParts, "\nsuggested_fix:\n"+limit400(o.SuggestedFix))
	}

	runID := strings.TrimSuffix(fname, ".json")

	return &iduna.ApplePayload{
		AppleType:  "signal_observation",
		Title:      title,
		Body:       strings.Join(bodyParts, "\n"),
		SourceRepo: "PRRJECT_FATBABY",
		RunID:      runID,
	}, nil
}

func loadPosted(stateFile string) map[string]bool {
	m := map[string]bool{}
	f, err := os.Open(stateFile)
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			m[line] = true
		}
	}
	return m
}

func markPosted(stateFile, fname string) {
	_ = os.MkdirAll(filepath.Dir(stateFile), 0o755)
	f, err := os.OpenFile(stateFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, fname)
}

func limit800(s string) string {
	if len(s) <= 800 {
		return s
	}
	return s[:799] + "…"
}

func limit400(s string) string {
	if len(s) <= 400 {
		return s
	}
	return s[:399] + "…"
}

// manifestEntry is one row in MANIFEST.json.
type manifestEntry struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	SourceRepo string `json:"source_repo"`
	Date       string `json:"date"` // YYYYMMDD
	ArchivedAt string `json:"archived_at"`
}

// manifest is the root of MANIFEST.json.
type manifest struct {
	GeneratedAt string          `json:"generated_at"`
	Repo        string          `json:"repo"`
	Count       int             `json:"count"`
	Apples      []manifestEntry `json:"apples"`
}

// updateManifest reads MANIFEST.json (creating it if absent), appends the new entry,
// writes it back, and commits it. Best-effort — failures are logged, sync continues.
func updateManifest(gitDir string, id int64, payload *iduna.ApplePayload, verbose bool) {
	manifestPath := filepath.Join(gitDir, "MANIFEST.json")
	var m manifest
	if data, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	if m.Repo == "" {
		m.Repo = "APPLES"
	}

	today := time.Now().UTC().Format("20060102")
	entry := manifestEntry{
		ID:         id,
		Type:       payload.AppleType,
		Title:      truncate(payload.Title, 140),
		SourceRepo: payload.SourceRepo,
		Date:       today,
		ArchivedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Avoid duplicates (idempotent on retry).
	for _, e := range m.Apples {
		if e.ID == id {
			return
		}
	}
	m.Apples = append(m.Apples, entry)
	m.Count = len(m.Apples)
	m.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o644); err != nil {
		if verbose {
			fmt.Printf("  [manifest] write error: %v\n", err)
		}
		return
	}

	addCmd := exec.Command("git", "-C", gitDir, "add", "MANIFEST.json")
	if err := addCmd.Run(); err != nil {
		if verbose {
			fmt.Printf("  [manifest] git add: %v\n", err)
		}
		return
	}
	commitCmd := exec.Command("git", "-C", gitDir, "commit", "--amend", "--no-edit")
	commitCmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=emily-sync", "GIT_COMMITTER_NAME=emily-sync")
	if err := commitCmd.Run(); err != nil {
		// amend failed (e.g. nothing staged) — try a fresh commit
		commitCmd2 := exec.Command("git", "-C", gitDir, "commit", "-m",
			fmt.Sprintf("manifest: update — %d apples", m.Count))
		commitCmd2.Env = commitCmd.Env
		if err2 := commitCmd2.Run(); err2 != nil && verbose {
			fmt.Printf("  [manifest] git commit: %v\n", err2)
			return
		}
	}
	if verbose {
		fmt.Printf("  [manifest] MANIFEST.json updated (%d total apples)\n", m.Count)
	}
}

// archiveAppleToGit writes an Apple as a JSON file to a git-tracked directory
// and auto-commits. The file path is <gitDir>/<YYYYMMDD>/<id>_<type>.json.
// Best-effort: failures are logged but do not abort the sync.
func archiveAppleToGit(gitDir string, id int64, payload *iduna.ApplePayload, verbose bool) {
	today := time.Now().UTC().Format("20060102")
	dir := filepath.Join(gitDir, today)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if verbose {
			fmt.Printf("  [apples-git] mkdir %s: %v\n", dir, err)
		}
		return
	}
	fname := fmt.Sprintf("%d_%s.json", id, strings.ReplaceAll(payload.AppleType, "_", "-"))
	fpath := filepath.Join(dir, fname)

	record := map[string]interface{}{
		"id":          id,
		"apple_type":  payload.AppleType,
		"source_repo": payload.SourceRepo,
		"run_id":      payload.RunID,
		"title":       payload.Title,
		"body":        payload.Body,
		"archived_at": time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(fpath, append(data, '\n'), 0o644); err != nil {
		if verbose {
			fmt.Printf("  [apples-git] write %s: %v\n", fpath, err)
		}
		return
	}

	// git add + commit (best-effort).
	addCmd := exec.Command("git", "-C", gitDir, "add", filepath.Join(today, fname))
	if err := addCmd.Run(); err != nil {
		if verbose {
			fmt.Printf("  [apples-git] git add: %v\n", err)
		}
		return
	}
	commitMsg := fmt.Sprintf("apple: #%d %s — %s", id, payload.AppleType, truncate(payload.Title, 60))
	commitCmd := exec.Command("git", "-C", gitDir, "commit", "-m", commitMsg)
	commitCmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=emily-sync", "GIT_COMMITTER_NAME=emily-sync")
	if err := commitCmd.Run(); err != nil {
		if verbose {
			fmt.Printf("  [apples-git] git commit: %v\n", err)
		}
		return
	}
	if verbose {
		fmt.Printf("  [apples-git] committed Apple #%d → %s/%s\n", id, today, fname)
	}
}
