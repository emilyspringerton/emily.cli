// cmd/backlog_archive.go — emily backlog archive + emily backlog compress
//
// archive: moves all [x] completed items from numbered sections into DONE.md,
//          keeping BACKLOG.md focused on open work only.
//
// compress: generates EMILY/GOLDEN.md — a dense ~600-token context view of
//           current open backlog, used as haiku context for cheap curation.

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	fp "path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

// runBacklogArchive moves [x] items from numbered sections to DONE.md.
func runBacklogArchive(args []string) int {
	fs := flag.NewFlagSet("backlog archive", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "show what would be archived without writing")
	noCommit := fs.Bool("no-commit", false, "skip git commit")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	backlogPath := fp.Join(cfg.EmilyRoot, "BACKLOG.md")
	donePath := fp.Join(cfg.EmilyRoot, "DONE.md")

	raw, err := os.ReadFile(backlogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read BACKLOG.md: %v\n", err)
		return 3
	}

	sections := extractSections(string(raw))
	type doneSection struct {
		name  string
		items []string
	}
	var toArchive []doneSection
	total := 0

	for _, s := range sections {
		var doneItems []string
		for _, line := range s.lines {
			if strings.HasPrefix(line, "- [x]") {
				doneItems = append(doneItems, line)
				total++
			}
		}
		if len(doneItems) > 0 {
			toArchive = append(toArchive, doneSection{name: s.header, items: doneItems})
		}
	}

	if total == 0 {
		fmt.Println("  Nothing to archive — no [x] items in numbered sections.")
		return 0
	}

	fmt.Printf("\n◈ BACKLOG ARCHIVE | %s\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Printf("  archiving %d completed items to DONE.md\n\n", total)

	if *dryRun {
		for _, s := range toArchive {
			fmt.Printf("  [%s]\n", s.name)
			for _, item := range s.items {
				fmt.Printf("    %s\n", truncate(item, 90))
			}
		}
		return 0
	}

	// Build DONE.md append block.
	var doneBlock strings.Builder
	doneBlock.WriteString(fmt.Sprintf("\n## Archived %s\n\n", time.Now().UTC().Format("2006-01-02")))
	for _, s := range toArchive {
		doneBlock.WriteString(fmt.Sprintf("### %s\n\n", s.name))
		for _, item := range s.items {
			doneBlock.WriteString(item + "\n")
		}
		doneBlock.WriteString("\n")
	}

	// Append to DONE.md (create if needed).
	existingDone, _ := os.ReadFile(donePath)
	if len(existingDone) == 0 {
		existingDone = []byte("# EMILY PRIME — DONE ARCHIVE\n*Completed items moved from BACKLOG.md. Git-authoritative.*\n")
	}
	if err := os.WriteFile(donePath, append(existingDone, []byte(doneBlock.String())...), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write DONE.md: %v\n", err)
		return 3
	}

	// Strip [x] items from BACKLOG.md.
	newBacklog := stripDoneItems(string(raw))
	if err := os.WriteFile(backlogPath, []byte(newBacklog), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write BACKLOG.md: %v\n", err)
		return 3
	}

	if !*noCommit {
		exec.Command("git", "-C", cfg.EmilyRoot, "add", "BACKLOG.md", "DONE.md").Run()
		msg := fmt.Sprintf("backlog: archive %d completed items → DONE.md (%s)", total, time.Now().UTC().Format("2006-01-02"))
		// Real gap found and fixed 2026-08-10 (founder: "ensure the entire monorepo always
		// gets that session id in all commits") -- same fix already applied to
		// gitCommitBacklog/changelog's own commit paths, missing here until now.
		if tag := currentSessionTag(cfg.EmilyRoot); tag != "" {
			msg = msg + "\n\nSession: " + tag
		}
		out, err := exec.Command("git", "-C", cfg.EmilyRoot, "commit", "-m", msg).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "nothing to commit") {
			fmt.Fprintf(os.Stderr, "git commit: %v (continuing)\n", err)
		} else {
			fmt.Printf("  ✓ committed BACKLOG.md + DONE.md\n")
		}
	}

	fmt.Printf("  Done. Archived: %d items\n\n", total)
	return 0
}

// runBacklogCompress generates EMILY/GOLDEN.md — a dense context view.
func runBacklogCompress(args []string) int {
	fs := flag.NewFlagSet("backlog compress", flag.ContinueOnError)
	noCommit := fs.Bool("no-commit", false, "skip git commit")
	outPath := fs.String("out", "", "output path (default: EMILY/GOLDEN.md)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	backlogPath := fp.Join(cfg.EmilyRoot, "BACKLOG.md")
	goldenPath := fp.Join(cfg.EmilyRoot, "GOLDEN.md")
	if *outPath != "" {
		goldenPath = *outPath
	}

	raw, err := os.ReadFile(backlogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read BACKLOG.md: %v\n", err)
		return 3
	}

	sections := extractSections(string(raw))
	type sectionStats struct {
		header string
		open   []string
		done   int
	}
	var stats []sectionStats
	totalOpen, totalDone := 0, 0
	for _, s := range sections {
		var openItems []string
		doneCount := 0
		for _, line := range s.lines {
			if strings.HasPrefix(line, "- [ ]") {
				openItems = append(openItems, line)
				totalOpen++
			} else if strings.HasPrefix(line, "- [x]") {
				doneCount++
				totalDone++
			}
		}
		if len(openItems) > 0 || doneCount > 0 {
			stats = append(stats, sectionStats{header: s.header, open: openItems, done: doneCount})
		}
	}

	intakeItems := parseIntakeItems(string(raw))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("# EMILY GOLDEN CONTEXT — %s\n", time.Now().UTC().Format("2006-01-02")))
	b.WriteString("*Auto-generated by `emily backlog compress`. Dense haiku context. Source: EMILY/BACKLOG.md.*\n\n")
	b.WriteString("## SYSTEM REFS\n")
	b.WriteString("Repos: EMILY · PRRJECT_FATBABY · IDUNA · TYLER · SHANKPIT · MJOLNIR · APPLES · emily.cli\n")
	b.WriteString("Sprint API: IDUNA /api/v1/heimdal/sprints | Northstar: PRRJECT_FATBABY/docs/northstar/northstar.md\n")
	b.WriteString("Backlog: EMILY/BACKLOG.md | Archive: EMILY/DONE.md\n\n")
	b.WriteString(fmt.Sprintf("## BACKLOG STATUS: %d open, %d done (archived), %d intake pending\n\n", totalOpen, totalDone, len(intakeItems)))

	// Completed sections (no open items) → one summary line.
	var completedNames []string
	for _, s := range stats {
		if len(s.open) == 0 {
			completedNames = append(completedNames, extractSectionShortName(s.header))
		}
	}
	if len(completedNames) > 0 {
		b.WriteString("## COMPLETE (no open items): " + strings.Join(completedNames, " · ") + "\n\n")
	}

	b.WriteString("## OPEN ITEMS\n")
	for _, s := range stats {
		if len(s.open) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("\n### %s (%d open, %d done)\n", s.header, len(s.open), s.done))
		for _, item := range s.open {
			b.WriteString("• " + compactBacklogLine(item) + "\n")
		}
	}

	if len(intakeItems) > 0 {
		b.WriteString(fmt.Sprintf("\n### INTAKE QUEUE (%d items — run emily backlog promote to classify)\n", len(intakeItems)))
		show := intakeItems
		if len(show) > 8 {
			show = show[:8]
		}
		for _, it := range show {
			b.WriteString("• " + truncate(it.Summary, 88) + "\n")
		}
		if len(intakeItems) > 8 {
			b.WriteString(fmt.Sprintf("• ...and %d more\n", len(intakeItems)-8))
		}
	}

	b.WriteString("\n## RSI PRIME DIRECTIVE\n")
	b.WriteString("Pipeline: raw thought → emily eo → INTAKE QUEUE → emily backlog promote (haiku) → numbered section → HEIMDAL sprint → implementation.\n")
	b.WriteString("Every completion: Apple (IDUNA POST /api/v1/apples) + mark [x] + git commit + push.\n")

	golden := b.String()
	if err := os.WriteFile(goldenPath, []byte(golden), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write GOLDEN.md: %v\n", err)
		return 3
	}

	if !*noCommit {
		exec.Command("git", "-C", cfg.EmilyRoot, "add", "GOLDEN.md").Run()
		msg := fmt.Sprintf("emily: compress → GOLDEN.md (%d open, %d intake) %s",
			totalOpen, len(intakeItems), time.Now().UTC().Format("2006-01-02"))
		if tag := currentSessionTag(cfg.EmilyRoot); tag != "" {
			msg = msg + "\n\nSession: " + tag
		}
		exec.Command("git", "-C", cfg.EmilyRoot, "commit", "-m", msg).Run()
	}

	fmt.Printf("  ✓ GOLDEN.md written (%d open items, %d intake, ~%d tokens)\n",
		totalOpen, len(intakeItems), len(golden)/4)
	return 0
}

// ---------------------------------------------------------------------------
// BACKLOG.md parsing helpers (shared with backlog_promote.go)
// ---------------------------------------------------------------------------

type backlogSection struct {
	header string   // e.g. "SECTION 1: FOUNDATION (current sprint)"
	lines  []string // all non-header lines in this section
}

var sectionHeaderRe = regexp.MustCompile(`^## (SECTION \d+:.+)$`)

// extractSections parses numbered sections from BACKLOG.md content.
// Stops at INTAKE QUEUE or BACKLOG PROTOCOL.
func extractSections(content string) []backlogSection {
	var sections []backlogSection
	var cur *backlogSection

	for _, line := range strings.Split(content, "\n") {
		if m := sectionHeaderRe.FindStringSubmatch(line); m != nil {
			if cur != nil {
				sections = append(sections, *cur)
			}
			cur = &backlogSection{header: m[1]}
			continue
		}
		if strings.HasPrefix(line, "## INTAKE QUEUE") || strings.HasPrefix(line, "## BACKLOG PROTOCOL") {
			if cur != nil {
				sections = append(sections, *cur)
				cur = nil
			}
			break
		}
		if cur != nil {
			cur.lines = append(cur.lines, line)
		}
	}
	if cur != nil {
		sections = append(sections, *cur)
	}
	return sections
}

// stripDoneItems removes all "- [x]" lines and their continuation lines from numbered sections.
// A continuation line is any indented line (starts with spaces/tab) that follows a [x] item,
// with no blank line separator.
func stripDoneItems(content string) string {
	var out []string
	inNumberedSection := false
	inDoneItem := false // true while consuming continuation lines of a [x] item

	for _, line := range strings.Split(content, "\n") {
		if sectionHeaderRe.MatchString(line) {
			inNumberedSection = true
			inDoneItem = false
		} else if strings.HasPrefix(line, "## INTAKE QUEUE") || strings.HasPrefix(line, "## BACKLOG PROTOCOL") {
			inNumberedSection = false
			inDoneItem = false
		}

		if inNumberedSection {
			if strings.HasPrefix(line, "- [x]") {
				inDoneItem = true
				continue // drop the [x] line
			}
			if inDoneItem {
				// Continuation lines are indented (start with space/tab).
				if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
					continue // drop continuation line
				}
				// Blank line ends the done item block.
				inDoneItem = false
			}
		}
		out = append(out, line)
	}
	result := strings.Join(out, "\n")
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}
	return result
}

// extractSectionShortName returns e.g. "S3:SYSTEM_OBS" from the section header.
func extractSectionShortName(header string) string {
	re := regexp.MustCompile(`SECTION (\d+):\s+(\S+)`)
	m := re.FindStringSubmatch(header)
	if m == nil {
		return header
	}
	name := m[2]
	if len(name) > 10 {
		name = name[:10]
	}
	return fmt.Sprintf("S%s:%s", m[1], name)
}

// compactBacklogLine strips markdown from a backlog item line for dense display.
func compactBacklogLine(line string) string {
	line = strings.TrimPrefix(line, "- [ ] ")
	line = strings.TrimPrefix(line, "- [x] ")
	line = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(line, "$1")
	return truncate(line, 100)
}
