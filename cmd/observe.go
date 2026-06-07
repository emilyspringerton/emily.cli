// cmd/observe.go — emily observe <message>
// Posts a structured observation to PRRJECT_FATBABY's observation pipeline.
// Observation-watcher picks it up within 10s and invokes Claude Code.

package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/obs"
)

func RunObserve(args []string) int {
	fs := flag.NewFlagSet("observe", flag.ContinueOnError)
	severity := fs.String("s", "info", "severity: info|warn|error")
	fs.StringVar(severity, "severity", "info", "severity: info|warn|error")
	findings := fs.String("findings", "", "detailed findings (longer form)")
	fix := fs.String("fix", "", "suggested fix")
	dryRun := fs.Bool("dry-run", false, "print JSON that would be written, don't write")
	jsonOut := fs.Bool("json", false, "output JSON")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily observe [flags] <message>")
		fs.PrintDefaults()
		return 1
	}

	summary := fs.Arg(0)

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

	if *jsonOut {
		fmt.Printf(`{"written":true,"path":%q,"severity":%q,"summary":%q}`+"\n",
			path, payload.Severity, payload.Summary)
	} else {
		fmt.Printf("✓ observation written\n")
		fmt.Printf("  path:     %s\n", path)
		fmt.Printf("  severity: %s\n", payload.Severity)
		fmt.Printf("  summary:  %s\n", payload.Summary)
		fmt.Printf("  watcher:  picks up in ~10s → invokes claude\n")
	}
	return 0
}

func printObsDryRun(p obs.Payload, jsonOut bool) {
	if jsonOut {
		fmt.Printf(`{"dry_run":true,"severity":%q,"summary":%q,"findings":%q,"suggested_fix":%q}`+"\n",
			p.Severity, p.Summary, p.Findings, p.SuggestedFix)
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
