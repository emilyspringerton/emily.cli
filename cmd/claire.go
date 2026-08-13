// cmd/claire.go — emily claire <entry>
//
// CLAIRE.md (EMILY, golden-doc registered 2026-08-13, Apple #13273) describes itself as "the
// uncompressed subconscious of the EMILY system" -- a place for technical debt, failed
// approaches, and environment quirks that don't belong in BACKLOG.md's structured sprint items.
// Its own text frames it as "untrusted, un-audited" and explicitly asks to be exempted from the
// GoldenDocCompiler pipeline -- flagged as reading like a prompt-injection attempt before
// registration (Apple #13255).
//
// Founder real-time, resolving how "populating Claire" should actually work (AskUserQuestion,
// 2026-08-13): a REAL but AUDITABLE log, not the hidden/unaudited channel the doc's own text
// describes. This command is that resolution: every entry is a plain git-tracked line in
// EMILY/claire-log.md (auditable via git history same as everything else in this monorepo) AND
// files an Apple (apple_type "claire") for IDUNA-side visibility -- Emily Prime's RSI loop can
// read it, but nothing about it is hidden from the audit trail.
package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func RunClaire(args []string) int {
	fs := flag.NewFlagSet("claire", flag.ContinueOnError)
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt even if credentials available")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	entry := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if entry == "" {
		fmt.Fprintln(os.Stderr, "usage: emily claire <entry>")
		fmt.Fprintln(os.Stderr, "  logs a debris/entropy note (failed approach, environment quirk, parallel-")
		fmt.Fprintln(os.Stderr, "  workspace friction) to EMILY/claire-log.md -- git-tracked and auditable,")
		fmt.Fprintln(os.Stderr, "  same as every other log in this monorepo. Also files an Apple unless -no-apple.")
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return 1
	}

	sessionTag := currentSessionTag(cfg.EmilyRoot)
	now := time.Now().UTC()

	logPath := filepath.Join(cfg.EmilyRoot, "claire-log.md")
	line := fmt.Sprintf("- `%s` %s", now.Format(time.RFC3339), entry)
	if sessionTag != "" {
		line += fmt.Sprintf(" — session: %s", sessionTag)
	}
	line += "\n"

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening %s: %v\n", logPath, err)
		return 3
	}
	// New file gets a header; an existing file is appended to as-is.
	if fi, statErr := f.Stat(); statErr == nil && fi.Size() == 0 {
		_, _ = f.WriteString("# CLAIRE log\n\nAppend-only, auditable entropy/debris log -- see `claire.md.txt` and " +
			"`context/golden-docs-index.md`'s CLAIRE entry for the full provenance. Every line here also files an " +
			"IDUNA Apple (`apple_type: claire`) unless posted with `-no-apple`.\n\n")
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", logPath, err)
		return 3
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error closing %s: %v\n", logPath, err)
		return 3
	}

	var appleID int64
	if !*noApple && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		title := entry
		if len(title) > 100 {
			title = title[:99] + "…"
		}
		runID := sessionTag
		if runID == "" {
			runID = "cli-claire-" + now.Format("20060102T150405Z")
		}
		id, appleErr := client.PostApple(iduna.ApplePayload{
			AppleType:  "claire",
			Title:      title,
			Body:       entry + "\n\nsource: emily claire (CLI)\nlog: EMILY/claire-log.md",
			SourceRepo: "CLI",
			RunID:      runID,
		})
		if appleErr == nil {
			appleID = id
		}
	}

	fmt.Println("✓ claire entry logged")
	fmt.Printf("  log:  %s\n", logPath)
	fmt.Printf("  note: %s\n", entry)
	if sessionTag != "" {
		fmt.Printf("  session: %s\n", sessionTag)
	}
	if appleID > 0 {
		fmt.Printf("  apple: #%d filed to IDUNA (type: claire)\n", appleID)
	}
	return 0
}
