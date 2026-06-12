// cmd/backlog.go — emily backlog curate
// Auto-curates FatBaby observations into EMILY/BACKLOG.md.
// State-tracked (EMILY/var/backlog-curated.txt) for idempotency.

package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

const intakeHeader = `
## INTAKE QUEUE (curated by emily backlog curate)

Items below have been auto-curated from FatBaby observations. Emily Prime reviews
and promotes them into the appropriate section when she plans the next sprint.

`

// RunBacklog dispatches emily backlog subcommands.
func RunBacklog(args []string) int {
	if len(args) == 0 || args[0] == "curate" {
		rest := args
		if len(args) > 0 {
			rest = args[1:]
		}
		return runBacklogCurate(rest)
	}
	switch args[0] {
	case "promote":
		return runBacklogPromote(args[1:])
	case "archive":
		return runBacklogArchive(args[1:])
	case "compress":
		return runBacklogCompress(args[1:])
	case "done":
		return runBacklogDone(args[1:])
	}
	fmt.Fprintf(os.Stderr, "emily backlog: unknown subcommand %q — try: curate, promote, archive, compress, done\n", args[0])
	return 1
}

type curatedResult struct {
	FileKey     string `json:"file_key"`   // filename stem — dedup key in state file
	Timestamp   string `json:"timestamp"`  // JSON timestamp field — for display/Apple body
	Summary     string `json:"summary"`
	BacklogLine string `json:"backlog_line"`
}

func runBacklogCurate(args []string) int {
	fs := flag.NewFlagSet("backlog curate", flag.ContinueOnError)
	all := fs.Bool("all", false, "process all uncurated observations")
	limit := fs.Int("limit", 10, "max observations to process per pass")
	dryRun := fs.Bool("dry-run", false, "show what would be added without writing")
	noCommit := fs.Bool("no-commit", false, "write BACKLOG.md but skip git commit")
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt")
	jsonOut := fs.Bool("json", false, "output JSON summary")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	obsDir := filepath.Join(cfg.FatBabyRoot, "var", "emily-observations")
	backlogPath := filepath.Join(cfg.EmilyRoot, "BACKLOG.md")
	stateFile := filepath.Join(cfg.EmilyRoot, "var", "backlog-curated.txt")

	// ensure var/ exists
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", filepath.Dir(stateFile), err)
		return 3
	}

	curated := loadCurated(stateFile)
	obsFiles, err := listObsFiles(obsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list obs: %v\n", err)
		return 3
	}

	// Reverse-chron: newest first (listObsFiles returns mtime-asc, so reverse).
	for i, j := 0, len(obsFiles)-1; i < j; i, j = i+1, j-1 {
		obsFiles[i], obsFiles[j] = obsFiles[j], obsFiles[i]
	}

	var toProcess []string
	for _, f := range obsFiles {
		ts := obsTimestamp(f)
		if _, seen := curated[ts]; seen {
			continue
		}
		toProcess = append(toProcess, f)
		if !*all && len(toProcess) >= *limit {
			break
		}
	}
	if *all && len(toProcess) > *limit {
		toProcess = toProcess[:*limit]
	}

	if !*jsonOut {
		fmt.Printf("\n◈ EMILY OS — BACKLOG CURATION | %s\n", time.Now().Format("2006-01-02 15:04"))
		fmt.Printf("  backlog: %s\n  state:   %s\n  obs dir: %s\n\n",
			backlogPath, stateFile, obsDir)
		fmt.Printf("  uncurated available: %d | processing: %d\n\n",
			len(toProcess), min(len(toProcess), *limit))
	}

	if len(toProcess) == 0 {
		if !*jsonOut {
			fmt.Println("  Nothing to curate — all observations already in backlog.")
		} else {
			fmt.Println(`{"curated":0,"message":"nothing to curate"}`)
		}
		return 0
	}

	// Read each obs and build backlog lines.
	var results []curatedResult
	for _, f := range toProcess {
		obs, err := readObsFile(filepath.Join(obsDir, f))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", f, err)
			continue
		}
		// Skip status pings — these are already captured as Apples, not backlog items.
		if isTrivialObs(obs.Summary) {
			if !*jsonOut {
				fmt.Printf("  ~ skip (status): %s\n", truncate(obs.Summary, 70))
			}
			// Still mark as "curated" so we don't re-evaluate on every pass.
			results = append(results, curatedResult{
				FileKey:     obsTimestamp(f),
				Timestamp:   obs.Timestamp,
				Summary:     obs.Summary,
				BacklogLine: "", // empty = state-tracked but not written to backlog
			})
			continue
		}
		line := fmt.Sprintf("- [ ] **%s** — obs `%s`. CURATED: %s.",
			sanitizeBacklogLine(obs.Summary),
			obs.Timestamp,
			time.Now().UTC().Format("2006-01-02"))
		results = append(results, curatedResult{
			FileKey:     obsTimestamp(f),
			Timestamp:   obs.Timestamp,
			Summary:     obs.Summary,
			BacklogLine: line,
		})
		if !*jsonOut {
			fmt.Printf("  + %s\n    %s\n\n", obs.Timestamp, truncate(obs.Summary, 80))
		}
	}

	if len(results) == 0 {
		if !*jsonOut {
			fmt.Println("  No parseable observations found.")
		}
		return 0
	}

	if *dryRun {
		written := 0
		if !*jsonOut {
			fmt.Println("  [dry-run] BACKLOG.md would receive:")
			for _, r := range results {
				if r.BacklogLine != "" {
					fmt.Printf("    %s\n", r.BacklogLine)
					written++
				}
			}
			if written == 0 {
				fmt.Println("    (none — all observations are status pings)")
			}
		} else {
			b, _ := json.Marshal(map[string]interface{}{"dry_run": true, "would_curate": results})
			fmt.Println(string(b))
		}
		return 0
	}

	// Append to BACKLOG.md.
	if err := appendToBacklog(backlogPath, results); err != nil {
		fmt.Fprintf(os.Stderr, "write backlog: %v\n", err)
		return 3
	}

	// Update state file.
	if err := markCurated(stateFile, results); err != nil {
		fmt.Fprintf(os.Stderr, "update state: %v\n", err)
		return 3
	}

	// Git commit.
	if !*noCommit {
		msg := fmt.Sprintf("backlog: curate %d obs → INTAKE QUEUE (%s)",
			len(results), time.Now().UTC().Format("2006-01-02"))
		if err := gitCommitBacklog(cfg.EmilyRoot, msg); err != nil {
			fmt.Fprintf(os.Stderr, "git commit: %v (continuing)\n", err)
		} else if !*jsonOut {
			fmt.Printf("  ✓ committed BACKLOG.md\n")
		}
	}

	// Post Apple.
	var appleID int64
	if !*noApple && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		titles := make([]string, 0, len(results))
		for _, r := range results {
			titles = append(titles, truncate(r.Summary, 60))
		}
		body := fmt.Sprintf("Curated %d FatBaby observations into EMILY/BACKLOG.md INTAKE QUEUE.\n\nItems:\n", len(results))
		for _, r := range results {
			body += fmt.Sprintf("- %s (%s)\n", r.Summary, r.Timestamp)
		}
		id, err := client.PostApple(iduna.ApplePayload{
			SourceRepo: "emily.cli",
			RunID:      fmt.Sprintf("backlog-curate-%d", time.Now().Unix()),
			AppleType:  "curation",
			Title:      fmt.Sprintf("backlog curate: %d obs -> INTAKE QUEUE", len(results)),
			Body:       body,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  apple post: %v (continuing)\n", err)
		} else {
			appleID = id
			if !*jsonOut {
				fmt.Printf("  ✓ Apple #%d filed\n", appleID)
			}
		}
	}

	if !*jsonOut {
		fmt.Printf("\n  Done. Curated: %d | Apple: #%d\n\n", len(results), appleID)
	} else {
		b, _ := json.Marshal(map[string]interface{}{
			"curated":  len(results),
			"apple_id": appleID,
			"items":    results,
		})
		fmt.Println(string(b))
	}
	return 0
}

// isTrivialObs returns true for observations that are status pings rather than
// actionable backlog items (RSI loop completions, heartbeats, prime task receipts, etc.).
func isTrivialObs(summary string) bool {
	lower := strings.ToLower(summary)
	trivialSubstrings := []string{
		"rsi loop iteration",
		"tic-toc cycle done",
		"fatbaby+tyler tic-toc",
		"prime task complete",
		"rsi cycle complete",
		"rsi session complete",
		"rsi session ",
		"emily.cli v0.",
		"emily.cli rsi",
		"emily.cli section",
		"emily.cli first real",
		"emily v0.2",
		"emily v0.3",
		"rsi: rsi loop",
	}
	for _, s := range trivialSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	// Single-word typos / test observations.
	trimmed := strings.TrimSpace(summary)
	trivialExact := []string{"tuo", "test", "ok", "ok ok"}
	for _, t := range trivialExact {
		if strings.EqualFold(trimmed, t) {
			return true
		}
	}
	return false
}

// appendToBacklog adds items to the INTAKE QUEUE section of BACKLOG.md,
// creating the section if needed.
func appendToBacklog(path string, results []curatedResult) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	// Build the new lines block (skip trivial-skipped entries with empty BacklogLine).
	var newLines strings.Builder
	for _, r := range results {
		if r.BacklogLine != "" {
			newLines.WriteString(r.BacklogLine + "\n")
		}
	}
	if newLines.Len() == 0 {
		return nil // nothing to write
	}

	const intakeMarker = "## INTAKE QUEUE"
	if idx := strings.Index(content, intakeMarker); idx >= 0 {
		// Find the end of the section header block (past the description paragraph).
		// Insert new items just before the next "## " section or EOF.
		insertPos := len(content)
		searchFrom := idx + len(intakeMarker)
		if next := strings.Index(content[searchFrom:], "\n## "); next >= 0 {
			insertPos = searchFrom + next + 1
		}
		content = content[:insertPos] + newLines.String() + content[insertPos:]
	} else {
		// Append the whole section at end (before BACKLOG PROTOCOL if present).
		const protocolMarker = "## BACKLOG PROTOCOL"
		if idx2 := strings.Index(content, protocolMarker); idx2 >= 0 {
			content = content[:idx2] + intakeHeader + newLines.String() + "\n---\n\n" + content[idx2:]
		} else {
			content += intakeHeader + newLines.String()
		}
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func gitCommitBacklog(emilyRoot, msg string) error {
	add := exec.Command("git", "-C", emilyRoot, "add", "BACKLOG.md")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, out)
	}
	commit := exec.Command("git", "-C", emilyRoot, "commit", "-m", msg)
	if out, err := commit.CombinedOutput(); err != nil {
		// "nothing to commit" is not a real error.
		if strings.Contains(string(out), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit: %w: %s", err, out)
	}
	return nil
}

func markCurated(stateFile string, results []curatedResult) error {
	f, err := os.OpenFile(stateFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, r := range results {
		fmt.Fprintln(w, r.FileKey)
	}
	return w.Flush()
}

func loadCurated(stateFile string) map[string]struct{} {
	m := make(map[string]struct{})
	f, err := os.Open(stateFile)
	if err != nil {
		return m
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if ts := strings.TrimSpace(s.Text()); ts != "" {
			m[ts] = struct{}{}
		}
	}
	return m
}

func readObsFile(path string) (obsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return obsFile{}, err
	}
	var obs obsFile
	if err := json.Unmarshal(data, &obs); err != nil {
		return obsFile{}, err
	}
	if obs.Summary == "" {
		return obsFile{}, fmt.Errorf("empty summary")
	}
	return obs, nil
}

type obsEntry struct {
	name  string
	mtime int64 // unix nano — for sort stability across mixed filename formats
}

// listObsFiles returns all non-hidden .json filenames (not full paths) in obsDir,
// excluding latest.json. Sorted by mtime ascending so the caller can reverse for newest-first.
func listObsFiles(obsDir string) ([]string, error) {
	entries, err := os.ReadDir(obsDir)
	if err != nil {
		return nil, err
	}
	var oes []obsEntry
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasPrefix(n, ".") || n == "latest.json" {
			continue
		}
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		oes = append(oes, obsEntry{name: n, mtime: info.ModTime().UnixNano()})
	}
	sort.Slice(oes, func(i, j int) bool { return oes[i].mtime < oes[j].mtime })
	names := make([]string, len(oes))
	for i, o := range oes {
		names[i] = o.name
	}
	return names, nil
}

// obsTimestamp returns the canonical dedup key for a filename.
// We use the filename stem (without .json) directly — no format conversion.
// Observation filenames are unique per timestamp regardless of separator style.
func obsTimestamp(filename string) string {
	return strings.TrimSuffix(filename, ".json")
}

func sanitizeBacklogLine(s string) string {
	// Escape markdown bold markers to avoid breaking the `**summary**` format.
	s = strings.ReplaceAll(s, "**", "*")
	// Trim to a reasonable length.
	return truncate(s, 120)
}

// runBacklogDone marks a matching `- [ ]` item as `- [x]` in BACKLOG.md.
// Usage: emily backlog done <match> [--apple-id N] [--no-commit] [--no-apple]
func runBacklogDone(args []string) int {
	fs := flag.NewFlagSet("backlog done", flag.ContinueOnError)
	appleID := fs.Int64("apple-id", 0, "Apple ID to record alongside the done marker")
	noCommit := fs.Bool("no-commit", false, "write BACKLOG.md but skip git commit")
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt")

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily backlog done [--apple-id N] [--no-commit] <match>")
		return 1
	}
	match := strings.Join(fs.Args(), " ")

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	backlogPath := filepath.Join(cfg.EmilyRoot, "BACKLOG.md")
	data, err := os.ReadFile(backlogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read backlog: %v\n", err)
		return 1
	}

	today := time.Now().UTC().Format("2006-01-02")
	var suffix string
	if *appleID > 0 {
		suffix = fmt.Sprintf(" [Apple #%d, %s]", *appleID, today)
	} else {
		suffix = fmt.Sprintf(" [done %s]", today)
	}

	lines := strings.Split(string(data), "\n")
	matched := false
	for i, line := range lines {
		if !strings.Contains(line, "- [ ]") {
			continue
		}
		if strings.Contains(strings.ToLower(line), strings.ToLower(match)) {
			// Replace first `- [ ]` with `- [x]` and append suffix.
			lines[i] = strings.Replace(line, "- [ ]", "- [x]", 1) + suffix
			matched = true
			fmt.Printf("  ✓ marked done: %s\n", truncate(line, 80))
			break
		}
	}
	if !matched {
		fmt.Fprintf(os.Stderr, "backlog done: no open item matching %q\n", match)
		return 1
	}

	if err := os.WriteFile(backlogPath, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write backlog: %v\n", err)
		return 1
	}

	if !*noCommit {
		commitMsg := fmt.Sprintf("backlog: ✓ %s", truncate(match, 60))
		if err := gitCommitBacklog(cfg.EmilyRoot, commitMsg); err != nil {
			fmt.Fprintf(os.Stderr, "git commit: %v\n", err)
			return 1
		}
		fmt.Println("  ✓ committed BACKLOG.md")
	}

	if !*noApple && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		id, err := client.PostApple(iduna.ApplePayload{
			SourceRepo: "emily.cli",
			AppleType:  "completion",
			Title:      fmt.Sprintf("backlog done: %s", truncate(match, 70)),
			Body:       fmt.Sprintf("Marked done in BACKLOG.md. Match: %q. Apple: #%d.", match, *appleID),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "  apple post: %v (continuing)\n", err)
		} else {
			fmt.Printf("  ✓ Apple #%d filed\n", id)
		}
	}

	return 0
}

