// cmd/memory.go — emily memory <subcommand>
//
// Subcommands:
//   emily memory digest   — print the obs-digest from emily-memory/ in TUI format
package cmd

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/color"
)

// observationDigest mirrors the struct written by emily-agent/obsdigest.go.
type observationDigest struct {
	GeneratedAt     time.Time      `json:"generated_at"`
	TotalCount      int            `json:"total_count"`
	BySeverity      map[string]int `json:"by_severity"`
	TopEntities     []string       `json:"top_entities"`
	LastSeenAt      string         `json:"last_seen_at"`
	RecentSummaries []string       `json:"recent_summaries"`
}

// RunMemory dispatches emily memory subcommands.
func RunMemory(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: emily memory <digest|consolidate>")
		return 1
	}
	switch args[0] {
	case "digest":
		return runMemoryDigest(args[1:])
	case "consolidate":
		return runMemoryConsolidate(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "emily memory: unknown subcommand %q\n", args[0])
		return 1
	}
}

func runMemoryDigest(args []string) int {
	fs := flag.NewFlagSet("memory digest", flag.ContinueOnError)
	emilyRoot := fs.String("emily-root", "", "path to EMILY repo root (default: $EMILY_ROOT or /home/fatbaby/EMILY)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	root := *emilyRoot
	if root == "" {
		root = os.Getenv("EMILY_ROOT")
		if root == "" {
			root = "/home/fatbaby/EMILY"
		}
	}

	digestPath := filepath.Join(root, "emily-agent", "emily-memory", "observations-digest.json")
	raw, err := os.ReadFile(digestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily memory digest: %v\n", err)
		fmt.Fprintf(os.Stderr, "  (run emily-agent to generate digest first)\n")
		return 1
	}

	var d observationDigest
	if err := json.Unmarshal(raw, &d); err != nil {
		fmt.Fprintf(os.Stderr, "emily memory digest: parse error: %v\n", err)
		return 1
	}

	printDigest(d)
	return 0
}

func printDigest(d observationDigest) {
	_ = color.Bold // ensure import is used
	fmt.Println()
	fmt.Printf("%s EMILY PRIME — OBSERVATION DIGEST %s\n",
		color.Bold("══"), color.Bold("══"))
	fmt.Printf("  generated : %s\n", d.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
	if d.LastSeenAt != "" {
		fmt.Printf("  last seen : %s\n", d.LastSeenAt)
	}
	fmt.Printf("  total     : %d observations\n", d.TotalCount)
	fmt.Println()

	if len(d.BySeverity) > 0 {
		fmt.Printf("%s Severity Breakdown\n", color.Bold("──"))
		for _, sev := range []string{"error", "warn", "info", "unknown"} {
			if n, ok := d.BySeverity[sev]; ok {
				icon := severityIcon(sev)
				fmt.Printf("    %s %-8s %d\n", icon, sev, n)
			}
		}
		for sev, n := range d.BySeverity {
			switch sev {
			case "error", "warn", "info", "unknown":
				continue
			}
			fmt.Printf("    · %-8s %d\n", sev, n)
		}
		fmt.Println()
	}

	if len(d.TopEntities) > 0 {
		fmt.Printf("%s Top Entities\n", color.Bold("──"))
		for i, e := range d.TopEntities {
			fmt.Printf("    %d. %s\n", i+1, e)
		}
		fmt.Println()
	}

	if len(d.RecentSummaries) > 0 {
		fmt.Printf("%s Recent Observations\n", color.Bold("──"))
		for _, s := range d.RecentSummaries {
			wrapped := wrapText(s, 72, "    ")
			fmt.Printf("  • %s\n", wrapped)
		}
		fmt.Println()
	}
}

func severityIcon(sev string) string {
	switch strings.ToLower(sev) {
	case "error":
		return "✗"
	case "warn":
		return "⚠"
	case "info":
		return "·"
	default:
		return "?"
	}
}

func wrapText(s string, width int, indent string) string {
	if len(s) <= width {
		return s
	}
	// Simple word-wrap: find last space before width.
	cut := width
	for cut > 0 && s[cut] != ' ' {
		cut--
	}
	if cut == 0 {
		return s[:width] + "\n" + indent + wrapText(s[width:], width, indent)
	}
	return s[:cut] + "\n" + indent + wrapText(s[cut+1:], width, indent)
}

// ── emily memory consolidate (S125-08) ────────────────────────────────────────

// runMemoryConsolidate reads all *.json files in emily-memory/, de-dupes keys
// across all fragments, and writes consolidated.json. Non-destructive: source
// files are not modified.
func runMemoryConsolidate(args []string) int {
	fs := flag.NewFlagSet("memory consolidate", flag.ContinueOnError)
	emilyRoot := fs.String("emily-root", "", "path to EMILY repo root (default: $EMILY_ROOT or /home/fatbaby/EMILY)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	root := *emilyRoot
	if root == "" {
		root = os.Getenv("EMILY_ROOT")
		if root == "" {
			root = "/home/fatbaby/EMILY"
		}
	}

	memDir := filepath.Join(root, "emily-agent", "emily-memory")
	entries, err := os.ReadDir(memDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily memory consolidate: readdir %s: %v\n", memDir, err)
		return 1
	}

	// merged holds the union of all key→value pairs. Later files win on collision.
	merged := map[string]json.RawMessage{}
	var sources []string
	consolidated := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if e.Name() == "consolidated.json" {
			continue // skip previous output
		}
		path := filepath.Join(memDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip %s: %v\n", e.Name(), err)
			continue
		}
		var fragment map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fragment); err != nil {
			// Not a flat JSON object — skip gracefully.
			continue
		}
		for k, v := range fragment {
			merged[k] = v
		}
		sources = append(sources, e.Name())
		consolidated++
	}

	if consolidated == 0 {
		fmt.Fprintln(os.Stderr, "emily memory consolidate: no *.json fragments found")
		return 1
	}

	// Annotate with consolidation metadata.
	now, _ := json.Marshal(time.Now().UTC().Format(time.RFC3339))
	srcJSON, _ := json.Marshal(sources)
	merged["_consolidated_at"] = now
	merged["_sources"] = srcJSON

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily memory consolidate: marshal: %v\n", err)
		return 1
	}

	outPath := filepath.Join(memDir, "consolidated.json")
	if err := os.WriteFile(outPath, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "emily memory consolidate: write %s: %v\n", outPath, err)
		return 1
	}

	fmt.Printf("emily memory consolidate: merged %d fragments → %s (%d keys)\n",
		consolidated, outPath, len(merged))
	return 0
}
