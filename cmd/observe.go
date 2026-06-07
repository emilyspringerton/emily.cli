// cmd/observe.go — emily observe <message>
// Posts a structured observation to PRRJECT_FATBABY's observation pipeline.
// Observation-watcher picks it up within 10s and invokes Claude Code.
// If IDUNA credentials are available, also posts an Apple immediately as a receipt.

package cmd

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
	"github.com/emilyspringerton/emily-cli/internal/obs"
)

func RunObserve(args []string) int {
	fs := flag.NewFlagSet("observe", flag.ContinueOnError)
	severity := fs.String("s", "info", "severity: info|warn|error")
	fs.StringVar(severity, "severity", "info", "severity: info|warn|error")
	findings := fs.String("findings", "", "detailed findings (longer form)")
	fix := fs.String("fix", "", "suggested fix")
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt even if credentials available")
	dryRun := fs.Bool("dry-run", false, "print JSON that would be written, don't write")
	jsonOut := fs.Bool("json", false, "output JSON")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	var summary string
	if fs.NArg() > 0 {
		summary = fs.Arg(0)
	} else {
		// Read summary from stdin when piped (e.g. git log -1 --oneline | emily observe -s info)
		fi, _ := os.Stdin.Stat()
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			sc := bufio.NewScanner(os.Stdin)
			var lines []string
			for sc.Scan() {
				lines = append(lines, sc.Text())
			}
			summary = strings.TrimSpace(strings.Join(lines, " "))
		}
	}
	if summary == "" {
		fmt.Fprintln(os.Stderr, "usage: emily observe [flags] <message>")
		fmt.Fprintln(os.Stderr, "       echo \"message\" | emily observe [flags]")
		fs.PrintDefaults()
		return 1
	}

	switch *severity {
	case "info", "warn", "error":
	default:
		fmt.Fprintf(os.Stderr, "unknown severity %q — use info, warn, or error\n", *severity)
		return 1
	}

	payload := obs.Payload{
		Summary:      summary,
		Severity:     *severity,
		Findings:     *findings,
		SuggestedFix: *fix,
	}

	if *dryRun {
		printObsDryRun(payload, *jsonOut)
		return 0
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	path, err := obs.Write(cfg.FatBabyRoot, payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 3
	}

	// Post Apple receipt to IDUNA if credentials are available.
	// Best-effort — failure here is non-fatal; the observation is already written.
	var appleID int64
	if !*noApple && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		title := fmt.Sprintf("[%s] %s", strings.ToUpper(*severity), summary)
		if len(title) > 100 {
			title = title[:99] + "…"
		}
		body := buildObsBody(payload)
		runID := "cli-obs-" + time.Now().UTC().Format("20060102T150405Z")
		id, appleErr := client.PostApple(iduna.ApplePayload{
			AppleType:  "signal_observation",
			Title:      title,
			Body:       body,
			SourceRepo: "CLI",
			RunID:      runID,
		})
		if appleErr == nil {
			appleID = id
		}
	}

	if *jsonOut {
		out := map[string]interface{}{
			"written":  true,
			"path":     path,
			"severity": payload.Severity,
			"summary":  payload.Summary,
		}
		if appleID > 0 {
			out["apple_id"] = appleID
		}
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(out)
	} else {
		fmt.Printf("✓ observation written\n")
		fmt.Printf("  path:     %s\n", path)
		fmt.Printf("  severity: %s\n", payload.Severity)
		fmt.Printf("  summary:  %s\n", payload.Summary)
		if appleID > 0 {
			fmt.Printf("  apple:    #%d filed to IDUNA\n", appleID)
		}
		fmt.Printf("  watcher:  picks up in ~10s → invokes claude\n")
	}
	return 0
}

func buildObsBody(p obs.Payload) string {
	var parts []string
	parts = append(parts, "severity: "+p.Severity)
	if p.Findings != "" {
		parts = append(parts, "\nfindings:\n"+p.Findings)
	}
	if p.SuggestedFix != "" {
		parts = append(parts, "\nsuggested_fix:\n"+p.SuggestedFix)
	}
	parts = append(parts, "\nsource: emily observe (CLI)")
	return strings.Join(parts, "\n")
}

func printObsDryRun(p obs.Payload, jsonOut bool) {
	if jsonOut {
		out := map[string]interface{}{
			"dry_run":       true,
			"severity":      p.Severity,
			"summary":       p.Summary,
			"findings":      p.Findings,
			"suggested_fix": p.SuggestedFix,
		}
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(out)
	} else {
		fmt.Println("[dry-run] would write:")
		fmt.Printf("  summary:       %s\n", p.Summary)
		fmt.Printf("  severity:      %s\n", p.Severity)
		if p.Findings != "" {
			fmt.Printf("  findings:      %s\n", p.Findings)
		}
		if p.SuggestedFix != "" {
			fmt.Printf("  suggested_fix: %s\n", p.SuggestedFix)
		}
	}
}
