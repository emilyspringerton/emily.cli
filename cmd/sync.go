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
		return runSyncWatch(obsDir, stateFile, client, *limit, *interval, *jsonOut)
	}

	if !*jsonOut {
		fmt.Printf("\n◈ EMILY OS — FATBABY OBSERVATION SYNC | %s\n", time.Now().Format("2006-01-02 15:04"))
	}
	posted := loadPosted(stateFile)
	n, skipped := syncPass(obsDir, stateFile, client, posted, *all, *limit, *dryRun, *jsonOut)
	if !*jsonOut {
		fmt.Printf("\n  Done. Posted: %d | Skipped (already synced): %d\n\n", n, skipped)
	}
	return 0
}

func runSyncWatch(obsDir, stateFile string, client *iduna.Client, limit, intervalSec int, jsonOut bool) int {
	if !jsonOut {
		fmt.Printf("\n◈ EMILY OS — SYNC WATCH | poll every %ds | ctrl-c to stop\n", intervalSec)
		fmt.Printf("  obsDir:    %s\n", obsDir)
		fmt.Printf("  stateFile: %s\n\n", stateFile)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	// Run once immediately before first tick
	posted := loadPosted(stateFile)
	n, _ := syncPass(obsDir, stateFile, client, posted, false, limit, false, jsonOut)
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
			n, _ := syncPass(obsDir, stateFile, client, posted, false, limit, false, jsonOut)
			if !jsonOut && n > 0 {
				fmt.Printf("  [%s] posted %d new observation(s)\n", time.Now().Format("15:04:05"), n)
			}
		}
	}
}

func syncPass(obsDir, stateFile string, client *iduna.Client, posted map[string]bool, all bool, limit int, dryRun bool, jsonOut bool) (nPosted, nSkipped int) {
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
