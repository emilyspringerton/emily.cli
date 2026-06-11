// cmd/backlog_promote.go — emily backlog promote
// Uses claude-haiku to classify INTAKE QUEUE items and move them into the
// appropriate numbered sections (or discard noise/done items).
// Requires ANTHROPIC_API_KEY env var; falls back to heuristic-only mode.

package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	fp "path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// intakeItem is a parsed entry from the INTAKE QUEUE section of BACKLOG.md.
type intakeItem struct {
	Key     string // obs timestamp, e.g. "2026-06-11T00:54:39Z"
	Summary string // the **...** text
	Line    string // full original markdown line (for exact removal)
}

// promotionClassification is haiku's verdict for one intake item.
type promotionClassification struct {
	Key         string `json:"key"`
	Action      string `json:"action"`       // promote|done|noise|duplicate
	Section     int    `json:"section"`      // target section (1-13+ existing, 14+ new)
	SectionName string `json:"section_name"` // name for new sections not yet in BACKLOG.md
	Title       string `json:"title"`        // clean title (max 80 chars)
	Priority    string `json:"priority"`     // high|normal|low
	Detail      string `json:"detail"`       // one-sentence description of what to build
}

const haikuPromoteSystem = `You are Emily Prime's backlog curator for EINHORN_INDUSTRIAL's multi-repo autonomous AI system.

Classify each INTAKE QUEUE observation. Output a JSON array — one object per item, same order as input.

Each object:
  "key": exact key from input
  "action": "promote" | "done" | "noise" | "duplicate"
  "section": integer section number (only for promote)
  "section_name": string (only if section >= 14, the new section's name in SCREAMING_SNAKE)
  "title": clean backlog title max 80 chars (only for promote)
  "priority": "high" | "normal" | "low" (only for promote)
  "detail": one sentence describing what to build (only for promote)

action rules:
  promote  = real new work not yet tracked in a numbered section
  done     = already implemented (matches completed work listed below)
  noise    = status pings, RSI/session complete, typos, clarification questions, error log pastes
  duplicate = same intent as another item in this batch

Already-completed work (mark done if matching):
  eo alias, APPLES git sync to dedicated repo, backlog curation pipeline,
  TUI Bloomberg terminal v0.9.0, observation batching, FatBaby signal quality fixes
  (director_long_tenure, governance_health_index, director_link, signal accuracy),
  MJOLNIR Android app, HEIMDAL sprint planning interface, Emily Prime web audit tool,
  Emily Prime FCM push, morning briefing, TYLER S06-S07 episodes, SHANKPIT Layer 2,
  SHANKPIT cross-scene attack guard, rsi-loop.sh, IDUNA systemd unit,
  cross-repo status script, observation watcher fires obs into golden backlog (Section 13).

Noise patterns (always mark noise):
  "RSI cycle complete", "RSI session complete", "prime task complete",
  "emily.cli vX.X RSI cycle", "emily.cli SECTION N CORE BACKLOG complete",
  "emily v0.", "emily.cli first real observation", typo-only messages like "tuo",
  clarification questions ("what is emily sync?", "how are prime-tasks fired?"),
  error log pastes (dial tcp, connect: connection refused, Failed to connect to bus),
  "tic toc rsi loop" memo items.

Existing sections:
  1=FOUNDATION, 2=MONEYPRINTERTURBO, 3=SYSTEM_OBSERVABILITY, 4=SHANKPIT_TYLER,
  5=FUTURE, 6=RSI_TIGHTENING, 7=SIGNAL_QUALITY, 8=RSI_NEXT_HORIZON,
  9=MJOLNIR, 10=WEB_AUDIT, 11=MJOLNIR_INTELLIGENCE, 12=HEIMDAL, 13=BACKLOG_CURATION

Section assignment:
  EmilyOS (bare-metal, exokernel, ISO2424242, Debian/Arch, BAZEL) → 14 (EMILYOS)
  PITVIPER custom terminal → 15 (PITVIPER)
  Emily Prime API / FABLE advisor / haiku→FABLE / token splitting → 16 (EMILY_PRIME_AI_TIER)
  Newssite (stock charts, charting lib, ticker UX, article dates, ingest) → 17 (NEWSSITE)
  IDUNA login/admin dashboard → 12 (HEIMDAL)
  Single log stream / all inputs to git → 3 (SYSTEM_OBSERVABILITY)
  Apple instrumentation gaps / Apple context token hack → 3 (SYSTEM_OBSERVABILITY)
  APPLES synced by IDUNA source of truth → 3 (SYSTEM_OBSERVABILITY)
  GPT-2 entropy / gpt-2 C fork → 5 (FUTURE)
  GTM funnel / subscription / editorial / self-improving training → 5 (FUTURE)
  TYLER expand phone role / cutscene functionality → 4 (SHANKPIT_TYLER)
  TUI bugs (hang, process handling, real-time clock, fatbaby mode) → 6 (RSI_TIGHTENING)
  Obs-watcher rate limit / prime task skip on rate limit → 6 (RSI_TIGHTENING)
  Golden docs review / emily prime system prompts update → 16 (EMILY_PRIME_AI_TIER)
  GitHub issue on observation → 5 (FUTURE)
  Filing observation correction UX → 3 (SYSTEM_OBSERVABILITY)
  Emily CLI status show emily.cli PID / fatbaby processes → 6 (RSI_TIGHTENING)

Output ONLY the JSON array. No markdown. No explanation.`

func runBacklogPromote(args []string) int {
	fs := flag.NewFlagSet("backlog promote", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "show classifications without modifying BACKLOG.md")
	limit := fs.Int("limit", 50, "max items to process per run")
	noCommit := fs.Bool("no-commit", false, "skip git commit after writing")
	noApple := fs.Bool("no-apple", false, "skip IDUNA Apple receipt")
	batchSize := fs.Int("batch", 15, "items per haiku call")
	jsonOut := fs.Bool("json", false, "output JSON summary")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	backlogPath := fp.Join(cfg.EmilyRoot, "BACKLOG.md")
	rawContent, err := os.ReadFile(backlogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read BACKLOG.md: %v\n", err)
		return 3
	}

	items := parseIntakeItems(string(rawContent))
	if len(items) == 0 {
		if !*jsonOut {
			fmt.Println("  Nothing in INTAKE QUEUE to promote.")
		} else {
			fmt.Println(`{"promoted":0,"removed":0}`)
		}
		return 0
	}
	if len(items) > *limit {
		items = items[:*limit]
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if !*jsonOut {
		mode := "haiku (full classify + route)"
		if apiKey == "" {
			mode = "heuristic (noise/done only — set ANTHROPIC_API_KEY for full classify)"
		}
		fmt.Printf("\n◈ BACKLOG PROMOTE | %s\n", time.Now().Format("2006-01-02 15:04"))
		fmt.Printf("  intake items: %d | batch: %d | mode: %s\n\n", len(items), *batchSize, mode)
	}

	var allResults []promotionClassification
	for i := 0; i < len(items); i += *batchSize {
		end := i + *batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]
		var results []promotionClassification
		if apiKey != "" {
			results, err = classifyIntakeBatch(apiKey, batch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  haiku classify: %v — falling back to heuristic\n", err)
				results = heuristicClassify(batch)
			}
		} else {
			results = heuristicClassify(batch)
		}
		// Without API key: skip promote actions (keep in INTAKE for proper classification later).
		if apiKey == "" {
			for i := range results {
				if results[i].Action == "promote" {
					results[i].Action = "skip"
				}
			}
		}
		allResults = append(allResults, results...)
	}

	if !*jsonOut {
		for _, r := range allResults {
			switch r.Action {
			case "promote":
				fmt.Printf("  ↑ [S%02d] %-65s [%s]\n", r.Section, truncate(r.Title, 65), r.Priority)
			case "done":
				fmt.Printf("  ✓ done     %s\n", truncate(r.Key, 30))
			case "noise":
				fmt.Printf("  ~ noise    %s\n", truncate(r.Key, 30))
			case "duplicate":
				fmt.Printf("  = dup      %s\n", truncate(r.Key, 30))
			case "skip":
				// silently skip — stays in INTAKE for haiku classification
			}
		}
		fmt.Println()
	}

	if *dryRun {
		if *jsonOut {
			b, _ := json.Marshal(map[string]any{"dry_run": true, "results": allResults})
			fmt.Println(string(b))
		}
		return 0
	}

	// Apply classifications to BACKLOG.md.
	doc := string(rawContent)
	byKey := make(map[string]intakeItem, len(items))
	for _, it := range items {
		byKey[it.Key] = it
	}

	promoted := 0
	removed := 0
	for _, r := range allResults {
		it, ok := byKey[r.Key]
		if !ok {
			continue
		}
		switch r.Action {
		case "promote":
			if r.Title != "" {
				detail := r.Detail
				if detail == "" {
					detail = "See original observation."
				}
				newLine := fmt.Sprintf("- [ ] **%s** — %s Obs: %s.", r.Title, detail, r.Key)
				doc = sectionInsert(doc, r.Section, r.SectionName, newLine)
				promoted++
			}
			doc = removeIntakeLine(doc, it.Line)
			removed++
		case "done", "noise", "duplicate":
			doc = removeIntakeLine(doc, it.Line)
			removed++
		case "skip":
			// Leave in INTAKE QUEUE — needs haiku classification via ANTHROPIC_API_KEY.
		}
	}
	// Collapse triple blank lines from removals.
	for strings.Contains(doc, "\n\n\n") {
		doc = strings.ReplaceAll(doc, "\n\n\n", "\n\n")
	}

	if err := os.WriteFile(backlogPath, []byte(doc), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write BACKLOG.md: %v\n", err)
		return 3
	}

	if !*noCommit {
		msg := fmt.Sprintf("backlog: promote %d → sections, clean %d noise/done (%s)",
			promoted, removed-promoted, time.Now().UTC().Format("2006-01-02"))
		if err := gitCommitBacklog(cfg.EmilyRoot, msg); err != nil {
			fmt.Fprintf(os.Stderr, "git commit: %v (continuing)\n", err)
		} else if !*jsonOut {
			fmt.Printf("  ✓ committed BACKLOG.md\n")
		}
	}

	var appleID int64
	if !*noApple && cfg.IDUNAAgentSecret != "" {
		client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
		id, appleErr := client.PostApple(iduna.ApplePayload{
			SourceRepo: "emily.cli",
			RunID:      fmt.Sprintf("backlog-promote-%d", time.Now().Unix()),
			AppleType:  "curation",
			Title:      fmt.Sprintf("backlog promote: %d promoted, %d removed", promoted, removed-promoted),
			Body:       fmt.Sprintf("Classified %d INTAKE items. Promoted: %d. Noise/done removed: %d.", len(allResults), promoted, removed-promoted),
		})
		if appleErr != nil {
			fmt.Fprintf(os.Stderr, "  apple post: %v (continuing)\n", appleErr)
		} else {
			appleID = id
		}
	}

	if !*jsonOut {
		fmt.Printf("  Done. Promoted: %d | Removed: %d | Apple: #%d\n\n", promoted, removed-promoted, appleID)
	} else {
		b, _ := json.Marshal(map[string]any{
			"promoted": promoted, "removed": removed - promoted,
			"apple_id": appleID, "results": allResults,
		})
		fmt.Println(string(b))
	}
	return 0
}

// ---------------------------------------------------------------------------
// Haiku API
// ---------------------------------------------------------------------------

func classifyIntakeBatch(apiKey string, items []intakeItem) ([]promotionClassification, error) {
	type inputItem struct {
		Key  string `json:"key"`
		Text string `json:"text"`
	}
	input := make([]inputItem, len(items))
	for i, it := range items {
		input[i] = inputItem{Key: it.Key, Text: it.Summary}
	}
	inputJSON, _ := json.Marshal(input)

	reqBody := map[string]any{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 2048,
		"system":     haikuPromoteSystem,
		"messages": []map[string]any{
			{"role": "user", "content": string(inputJSON)},
		},
	}
	payload, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("haiku request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("haiku status %d: %.200s", resp.StatusCode, raw)
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("parse haiku response: %w", err)
	}
	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty haiku response")
	}
	text := strings.TrimSpace(apiResp.Content[0].Text)
	// Strip markdown fences if haiku wrapped anyway.
	if strings.HasPrefix(text, "```") {
		if nl := strings.Index(text, "\n"); nl >= 0 {
			text = text[nl+1:]
		}
		if end := strings.LastIndex(text, "```"); end >= 0 {
			text = text[:end]
		}
		text = strings.TrimSpace(text)
	}

	var results []promotionClassification
	if err := json.Unmarshal([]byte(text), &results); err != nil {
		return nil, fmt.Errorf("parse classifications JSON: %w — got: %.300s", err, text)
	}
	return results, nil
}

// ---------------------------------------------------------------------------
// Heuristic fallback (no API key)
// ---------------------------------------------------------------------------

// heuristicClassify is a no-API fallback that catches obvious noise and done items.
// When ANTHROPIC_API_KEY is set, classifyIntakeBatch is used instead for proper
// section routing, priorities, and clean titles.
func heuristicClassify(items []intakeItem) []promotionClassification {
	noiseSubstrings := []string{
		"rsi cycle complete", "rsi session complete", "rsi session ",
		"prime task complete",
		"emily.cli v0.", "emily.cli rsi", "emily.cli section", "emily.cli first real",
		"emily v0.2", "emily v0.3",
		"tic toc rsi loop", "tic-toc rsi loop",
		"dial tcp", "connect: connection refused", "failed to connect to bus",
		"systemctl --user status daemon-reload",
		"rsi: rsi loop", "rsi cycle: s07e", "s07e02 'paris' complete",
		"golden backlog synced, tyler s07e01",
		"rsi session 4 complete", "rsi session 3 complete",
	}
	noiseFull := []string{"tuo", "test", "ok"}

	// Items known to be implemented (match against completed sections).
	doneSubstrings := []string{
		"dedicated emily observe command eo",
		"auto git sync all apples with dedicated apples repo",
		"observation watcher needs to fire the observations into the golden backlog",
		"emily.cli command that ensures all observations have been added to the golden backlog",
		"emily observe should auto-post an apple to iduna",
		"rsi: item 5.02 leadership parser",
		"rsi: form 4 insider watcher",
		"iduna bootstrap + vision docs complete",
		"jon stockwell agent implemented",
		"observation-watcher cannot invoke claude code — claude cli not in path",
		"expand ticker coverage using a static curated watchlist",
		"observation-watcher must inject full reporting",
		"all required and optional environment variables must be documented",
		"rsi token efficiency: prompt caching implemented",
		"ensure observation watcher correctly invokes claude",
		"it seems like some of our rsi work leaves dirty stuff in emily repo",
	}

	var out []promotionClassification
	for _, it := range items {
		lower := strings.ToLower(it.Summary)
		action := "promote"

		for _, t := range noiseFull {
			if strings.EqualFold(strings.TrimSpace(it.Summary), t) {
				action = "noise"
				break
			}
		}
		if action == "promote" {
			for _, p := range noiseSubstrings {
				if strings.Contains(lower, p) {
					action = "noise"
					break
				}
			}
		}
		if action == "promote" {
			for _, p := range doneSubstrings {
				if strings.Contains(lower, p) {
					action = "done"
					break
				}
			}
		}

		r := promotionClassification{Key: it.Key, Action: action}
		if action == "promote" {
			// Without haiku: default to FUTURE, user re-runs with ANTHROPIC_API_KEY for proper routing.
			r.Section = 5
			r.SectionName = "FUTURE"
			r.Title = truncate(it.Summary, 80)
			r.Priority = "normal"
			r.Detail = "Awaiting full classification — run emily backlog promote with ANTHROPIC_API_KEY."
		}
		out = append(out, r)
	}
	return out
}

// ---------------------------------------------------------------------------
// BACKLOG.md mutation helpers
// ---------------------------------------------------------------------------

// parseIntakeItems extracts all [ ] entries from the INTAKE QUEUE area.
// The area runs from "## INTAKE QUEUE" to "## BACKLOG PROTOCOL" (or EOF).
var intakeLineRe = regexp.MustCompile("^- \\[ \\] \\*\\*(.+?)\\*\\*.*— obs `([^`]+)`")

func parseIntakeItems(content string) []intakeItem {
	const startMarker = "## INTAKE QUEUE"
	const endMarker = "## BACKLOG PROTOCOL"
	start := strings.Index(content, startMarker)
	if start < 0 {
		return nil
	}
	end := strings.Index(content[start:], endMarker)
	var zone string
	if end < 0 {
		zone = content[start:]
	} else {
		zone = content[start : start+end]
	}

	var items []intakeItem
	for _, line := range strings.Split(zone, "\n") {
		m := intakeLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		items = append(items, intakeItem{
			Key:     m[2],
			Summary: m[1],
			Line:    line,
		})
	}
	return items
}

// sectionInsert inserts newLine at the bottom of section N in content.
// Creates the section if it doesn't exist (just before the INTAKE QUEUE).
func sectionInsert(content string, sectionNum int, sectionName, newLine string) string {
	headerRe := regexp.MustCompile(fmt.Sprintf(`(?m)^## SECTION %d:`, sectionNum))
	loc := headerRe.FindStringIndex(content)
	if loc == nil {
		// Create new section.
		name := sectionName
		if name == "" {
			name = fmt.Sprintf("SECTION_%d", sectionNum)
		}
		newSection := fmt.Sprintf("\n## SECTION %d: %s\n\n%s\n\n---\n", sectionNum, name, newLine)
		const intakeMarker = "\n## INTAKE QUEUE"
		if idx := strings.Index(content, intakeMarker); idx >= 0 {
			return content[:idx] + newSection + content[idx:]
		}
		return content + newSection
	}

	// Find the closing "---" for this section (first one after the header).
	searchFrom := loc[1]
	sepRe := regexp.MustCompile(`(?m)^---$`)
	nextHeadingRe := regexp.MustCompile(`(?m)^## `)

	sepLoc := sepRe.FindStringIndex(content[searchFrom:])
	headingLoc := nextHeadingRe.FindStringIndex(content[searchFrom:])

	var insertAt int
	switch {
	case sepLoc != nil && (headingLoc == nil || sepLoc[0] < headingLoc[0]):
		insertAt = searchFrom + sepLoc[0]
	case headingLoc != nil:
		insertAt = searchFrom + headingLoc[0]
	default:
		insertAt = len(content)
	}

	return content[:insertAt] + newLine + "\n" + content[insertAt:]
}

// removeIntakeLine removes one intake line from content.
func removeIntakeLine(content, line string) string {
	if r := strings.Replace(content, line+"\n", "", 1); r != content {
		return r
	}
	return strings.Replace(content, line, "", 1)
}
