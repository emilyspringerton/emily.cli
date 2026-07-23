// cmd/apples.go — emily apples list|post
// Queries IDUNA Apples log or posts a new Apple.

package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func RunApples(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily apples <list|post> [flags]")
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return runApplesList(rest)
	case "post":
		return runApplesPost(rest)
	case "get":
		return runApplesGet(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q — use list, post, or get\n", sub)
		return 1
	}
}

func runApplesList(args []string) int {
	fs := flag.NewFlagSet("apples list", flag.ContinueOnError)
	limit := fs.Int("n", 20, "max apples to show (fetched before --since filter)")
	fs.IntVar(limit, "limit", 20, "max apples to show")
	appleType := fs.String("t", "", "filter by apple_type")
	fs.StringVar(appleType, "type", "", "filter by apple_type")
	repo := fs.String("r", "", "filter by source_repo")
	fs.StringVar(repo, "repo", "", "filter by source_repo")
	since := fs.Int("since", 0, "only show apples from the last N minutes (0 = all)")
	full := fs.Bool("full", false, "show body text (first 5 lines)")
	jsonOut := fs.Bool("json", false, "output JSON")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Positional arg is a convenience repo filter
	if fs.NArg() > 0 && *repo == "" {
		*repo = fs.Arg(0)
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set and not found in secrets file")
		return 2
	}

	fetchLimit := *limit
	if *since > 0 {
		// Fetch more to cover the time window; the caller can raise -n if needed.
		fetchLimit = max(fetchLimit, 200)
	}

	apples, err := client.ListApples(iduna.AppleListFilters{
		Limit:      fetchLimit,
		SourceRepo: *repo,
		AppleType:  *appleType,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}

	if *since > 0 {
		cutoff := time.Now().UTC().Add(-time.Duration(*since) * time.Minute)
		var filtered []iduna.Apple
		for _, a := range apples {
			t, err := time.Parse(time.RFC3339, a.RecordedAt)
			if err != nil {
				// Try without fractional seconds
				t, err = time.Parse("2006-01-02T15:04:05Z", a.RecordedAt)
			}
			if err != nil || t.After(cutoff) {
				filtered = append(filtered, a)
			}
		}
		apples = filtered
	}

	if *jsonOut {
		printApplesJSON(apples)
		return 0
	}

	printApplesTable(apples, *full)
	return 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func runApplesPost(args []string) int {
	fs := flag.NewFlagSet("apples post", flag.ContinueOnError)
	appleType := fs.String("t", "", "apple_type (required)")
	fs.StringVar(appleType, "type", "", "apple_type (required)")
	repo := fs.String("r", "CLI", "source_repo")
	fs.StringVar(repo, "repo", "CLI", "source_repo")
	runID := fs.String("run-id", "", "run_id tag")
	jsonOut := fs.Bool("json", false, "output JSON")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *appleType == "" {
		fmt.Fprintln(os.Stderr, "error: -t/--type is required")
		return 1
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily apples post -t <type> <title> [body]")
		return 1
	}

	title := fs.Arg(0)
	body := ""
	if fs.NArg() > 1 {
		body = strings.Join(fs.Args()[1:], " ")
	} else {
		// Read body from stdin if piped
		fi, _ := os.Stdin.Stat()
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			var sb strings.Builder
			sc := bufio.NewScanner(os.Stdin)
			for sc.Scan() {
				sb.WriteString(sc.Text())
				sb.WriteByte('\n')
			}
			body = strings.TrimRight(sb.String(), "\n")
		}
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	// Auto-tag with the active session fingerprint (EMILY/BACKLOG.md S170-11)
	// so every Apple posted via the CLI is traceable to the Claude Code
	// session that produced it, with no new flag to remember. Explicit
	// -run-id still wins if the caller sets one.
	if *runID == "" {
		if tag := currentSessionTag(cfg.EmilyRoot); tag != "" {
			*runID = tag
		} else {
			*runID = "cli-" + time.Now().UTC().Format("20060102T150405Z")
		}
	}
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set")
		return 2
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	id, err := client.PostApple(iduna.ApplePayload{
		AppleType:  *appleType,
		Title:      title,
		Body:       body,
		SourceRepo: *repo,
		RunID:      *runID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 4
	}

	if *jsonOut {
		fmt.Printf(`{"id":%d,"type":%q,"title":%q}`+"\n", id, *appleType, title)
	} else {
		fmt.Printf("✓ Apple #%d filed\n", id)
		fmt.Printf("  type:  %s\n", *appleType)
		fmt.Printf("  title: %s\n", title)
	}
	return 0
}

func runApplesGet(args []string) int {
	fs := flag.NewFlagSet("apples get", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "output JSON")

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily apples get <id>")
		return 1
	}
	id, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %q is not a valid Apple ID\n", fs.Arg(0))
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
	apple, err := client.GetApple(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 4
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(apple)
		return 0
	}

	ts := apple.RecordedAt
	if len(ts) >= 19 {
		ts = ts[:19]
	}
	ts = strings.Replace(ts, "T", " ", 1)

	fmt.Printf("\n◈ Apple #%d\n", apple.ID)
	fmt.Printf("  type:       %s\n", apple.AppleType)
	fmt.Printf("  repo:       %s\n", apple.SourceRepo)
	fmt.Printf("  run:        %s\n", apple.RunID)
	fmt.Printf("  recorded:   %s\n", ts)
	fmt.Printf("  title:      %s\n", apple.Title)
	if apple.Body != "" {
		fmt.Println("\n  body:")
		for _, line := range strings.Split(apple.Body, "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
	fmt.Println()
	return 0
}

// ── Formatters ────────────────────────────────────────────────────────────────

func printApplesTable(apples []iduna.Apple, full bool) {
	fmt.Printf("\n◈ EMILY OS — APPLES | %s\n\n", time.Now().Format("2006-01-02 15:04"))
	if len(apples) == 0 {
		fmt.Println("  (no apples)")
		fmt.Println()
		return
	}
	for _, a := range apples {
		ts := a.RecordedAt
		if len(ts) >= 16 {
			ts = ts[:16]
		}
		ts = strings.Replace(ts, "T", " ", 1)
		repo := fmt.Sprintf("[%-12s]", a.SourceRepo)
		atype := truncate(a.AppleType, 20)
		title := truncate(a.Title, 55)
		fmt.Printf("  #%4d  %s  %s  %-20s  %s\n", a.ID, ts, repo, atype, title)
		if full && a.Body != "" {
			lines := strings.SplitN(a.Body, "\n", 6)
			for i, l := range lines {
				if i >= 5 {
					fmt.Printf("         …\n")
					break
				}
				fmt.Printf("         %s\n", l)
			}
			fmt.Println()
		}
	}
	fmt.Printf("\n  Total: %d apple(s)\n\n", len(apples))
}

func printApplesJSON(apples []iduna.Apple) {
	type out struct {
		ID         int64  `json:"id"`
		SourceRepo string `json:"source_repo"`
		AppleType  string `json:"apple_type"`
		Title      string `json:"title"`
		RecordedAt string `json:"recorded_at"`
	}
	rows := make([]out, len(apples))
	for i, a := range apples {
		rows[i] = out{a.ID, a.SourceRepo, a.AppleType, a.Title, a.RecordedAt}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(rows)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
