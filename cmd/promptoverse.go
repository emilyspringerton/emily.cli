// cmd/promptoverse.go — emily promptoverse add/work/queue/styles
//
// Generates real images for a Subject (e.g. "ducks") across N existing
// reusable Styles (e.g. "stained glass", "LEGO minifigure"), via Vertex AI's
// gemini-2.5-flash-image ("Nano Banana"), and publishes each as a new leaf
// node to the live Prompt-o-verse gallery (IDUNA's promptoverse.write API).
//
// Requests are queued to a durable FIFO file, not fired immediately --
// founder direction ("run gen requests fifo so duck is after the previous
// request"), after repeated Vertex AI 429s this session that were plausibly
// made worse by separate `add` invocations firing close together with no
// shared ordering or pacing. `add` enqueues then drains; `work` just drains
// whatever's already queued (e.g. to resume after a rate-limit stop without
// enqueueing anything new).
//
// Formalizes the ad-hoc Python generation scripts written by hand during
// VS0's first build (2026-08-17) into real, reusable emily.cli
// infrastructure -- see EMILY/docs/NORTHSTAR_PROMPT_O_VERSE.md for the full
// concept and IDUNA/internal/promptoverse for the data model (Label=style
// subcategory, Subject=what it's applied to, EZPrompt+ExpandedPrompt=the
// two-tier prompt pair).
//
// Auth: reuses this box's existing `gcloud` ADC (no dedicated GEMINI_API_KEY
// needed -- see the northstar's §5 for why that path is the one that
// actually works). Requires `gcloud auth print-access-token` to succeed.
package cmd

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

// promptoverseVertexProject/Region match the project this box's gcloud ADC
// is already scoped to (verified live 2026-08-17: aiplatform.googleapis.com
// enabled, active billing). Not read from env/config because there is
// exactly one working setup right now and hardcoding it is more honest than
// pretending this is portable to an unconfigured box.
const (
	promptoverseVertexProject   = "project-d24a71e9-2daf-4b2d-917"
	promptoverseVertexRegion    = "us-central1"
	promptoverseVertexModel     = "gemini-2.5-flash-image"
	promptoverseVertexTextModel = "gemini-2.5-flash" // text-only, used for style discovery -- see promptoverse_discover.go

	// promptoverseDefaultInterRequestDelay spacing between successful Vertex
	// AI calls during a drain. Bumped 6s -> 20s (founder, real-time: "when
	// draining the queue we need a longer wait between") -- 6s wasn't
	// enough to reliably avoid 429s across this session's batches.
	// Overridable via PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS so a future
	// "still not long enough" / "too conservative now" doesn't need a code
	// change to tune.
	promptoverseDefaultInterRequestDelay = 20 * time.Second
	promptoverseQueueFileName            = "promptoverse-queue.jsonl"
	promptoverseDiscoveredFileName       = "promptoverse-discovered-styles.json"
)

// style is one entry in the reusable style registry -- the "subcategory"
// (northstar §1/§3: Label) plus a template turning a bare Subject into the
// real expanded prompt. Only genuine, subject-agnostic art styles belong
// here -- transformation concepts that only make sense for one specific
// subject (e.g. "ice cream novelty" as applied to a baseball card) don't
// generalize the same way and are deliberately left out of this registry.
type style struct {
	Label string
	Kind  string // "historical" | "surreal"
	// Prompt builds the full expanded prompt for this style applied to subject.
	Prompt func(subject string) string
}

var promptoverseStyles = []style{
	{"1910s tobacco card", "historical", func(s string) string {
		return fmt.Sprintf("A portrait of %s, rendered as a vintage 1910s tobacco baseball card -- "+
			"sepia-toned hand-tinted lithograph illustration style, ornate Victorian card border, "+
			"ivory background, T206-style antique card design.", s)
	}},
	{"claymation", "surreal", func(s string) string {
		return fmt.Sprintf("%s as a claymation stop-motion figure, visible clay texture and "+
			"fingerprints, Wallace-and-Gromit-style character design.", s)
	}},
	{"Renaissance oil painting", "surreal", func(s string) string {
		return fmt.Sprintf("A Renaissance oil painting portrait of %s, dramatic chiaroscuro "+
			"lighting, ornate gilded frame border, classical portraiture style.", s)
	}},
	{"8-bit pixel art", "surreal", func(s string) string {
		return fmt.Sprintf("%s in 8-bit pixel art style, retro video game sprite aesthetic, "+
			"limited color palette, blocky pixelated rendering.", s)
	}},
	{"LEGO minifigure", "surreal", func(s string) string {
		return fmt.Sprintf("%s built entirely out of LEGO bricks, minifigure-adjacent "+
			"proportions, plastic brick texture, studio product-photo lighting.", s)
	}},
	{"stained glass", "surreal", func(s string) string {
		return fmt.Sprintf("%s rendered as a stained glass cathedral window, leaded glass "+
			"segments, jewel-toned colors, light shining through, ornate gothic frame border.", s)
	}},
	{"Art Deco travel poster", "surreal", func(s string) string {
		return fmt.Sprintf("%s designed as a vintage Art Deco travel poster, bold flat colors, "+
			"geometric sunburst background, elegant 1920s typography.", s)
	}},
	{"pop art silkscreen", "surreal", func(s string) string {
		return fmt.Sprintf("%s in 1960s pop-art silkscreen style, Warhol-esque bold flat color "+
			"blocks, halftone dot shading, high contrast.", s)
	}},
	{"woodcut block print", "surreal", func(s string) string {
		return fmt.Sprintf("%s as a bold black-and-white woodcut block-print illustration, "+
			"heavy linework, high contrast.", s)
	}},
	{"watercolor sketchbook", "surreal", func(s string) string {
		return fmt.Sprintf("%s in loose watercolor sketchbook style, visible brushstrokes and "+
			"paper texture, soft bleeding pigment.", s)
	}},
	// The next 4 were proven out as one-off Labels in the original 20-prompt
	// baseball-card batch (S176-02) and promoted into the reusable registry
	// here (founder: "ensure we have more variety for the categories that
	// already exist like space and underwater etc") -- genuinely subject-
	// agnostic transformation concepts, same bar as the 10 above. Two
	// baseball-card-only siblings from that batch ("1990s glossy rookie
	// card", "2020s Topps Chrome refractor" -- print-era variants of the
	// tobacco-card concept, not their own transformation) and "ice cream
	// novelty" (judged too baseball-card-specific when the registry was
	// first built) are deliberately left out.
	{"outer space", "surreal", func(s string) string {
		return fmt.Sprintf("%s floating in outer space, starfield and nebula backdrop, "+
			"dramatic rim lighting, astronaut-helmet reflection detail.", s)
	}},
	{"underwater", "surreal", func(s string) string {
		return fmt.Sprintf("%s submerged underwater, refracted sunbeams through water, "+
			"floating bubbles, soft blue-green caustic lighting.", s)
	}},
	{"robot", "surreal", func(s string) string {
		return fmt.Sprintf("%s reimagined as a chrome robot, exposed servos and rivets, "+
			"glowing optical sensors, industrial sci-fi design.", s)
	}},
	{"made of candy", "surreal", func(s string) string {
		return fmt.Sprintf("%s sculpted entirely out of candy -- gumdrops, licorice, and "+
			"hard-candy shell -- glossy sugar-glaze texture, bright saturated colors.", s)
	}},
}

func styleByLabel(label string) (style, bool) {
	return styleByLabelInPool(promptoverseStyles, label)
}

func styleByLabelInPool(pool []style, label string) (style, bool) {
	for _, st := range pool {
		if st.Label == label {
			return st, true
		}
	}
	return style{}, false
}

func RunPromptOVerse(args []string) int {
	if len(args) == 0 {
		return promptoverseUsage()
	}
	switch args[0] {
	case "add":
		return runPromptOVerseAdd(args[1:])
	case "work":
		return runPromptOVerseWork()
	case "queue":
		return runPromptOVerseQueueList()
	case "styles":
		return runPromptOVerseStyles()
	default:
		fmt.Fprintf(os.Stderr, "emily promptoverse: unknown subcommand %q\n\n", args[0])
		return promptoverseUsage()
	}
}

func promptoverseUsage() int {
	fmt.Print(`emily promptoverse — generate + publish Prompt-o-verse gallery nodes

Subcommands:
  emily promptoverse add <subject> <count>   Queue <count> styles applied to <subject>, then drain
  emily promptoverse work                    Drain whatever's already queued (e.g. resume after a 429)
  emily promptoverse queue                   List pending queue entries, oldest first
  emily promptoverse styles                  List the reusable style registry

Example:
  emily promptoverse add ducks 6

Requests are queued FIFO to a durable file, not fired immediately -- if
another 'add' is already mid-flight or queued, new requests wait their turn
in arrival order. Draining stops (not retries) on a rate limit, leaving the
remainder queued for 'emily promptoverse work' later. 20s between successful
requests by default -- override with PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS.

Requires: gcloud ADC already authenticated (this box's existing setup),
IDUNA_AGENT_SECRET for EMILY-PRIME (promptoverse.write), iduna.service running.
`)
	return 1
}

func runPromptOVerseStyles() int {
	fmt.Println("Reusable style registry:")
	for i, st := range promptoverseStyles {
		fmt.Printf("  %2d. %s (%s)\n", i+1, st.Label, st.Kind)
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	discovered, err := loadDiscoveredStyles(discoveredStylesPath(cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load discovered styles: %v\n", err)
		return 1
	}
	if len(discovered) > 0 {
		fmt.Println("\nDiscovered styles (added by Vertex AI during 'add' runs):")
		for i, ds := range discovered {
			fmt.Printf("  %2d. %s (%s) — discovered for %q on %s\n",
				i+1, ds.Label, ds.Kind, ds.DiscoveredFor, ds.DiscoveredAt.Format("2006-01-02"))
		}
	}
	return 0
}

// queueItem is one pending generation request, persisted as a JSONL line.
type queueItem struct {
	Subject    string    `json:"subject"`
	StyleLabel string    `json:"style_label"`
	EnqueuedAt time.Time `json:"enqueued_at"`
}

func queuePath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverseQueueFileName)
}

func loadQueue(path string) ([]queueItem, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var items []queueItem
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var it queueItem
		if err := json.Unmarshal(line, &it); err != nil {
			continue // skip a corrupt line rather than fail the whole queue
		}
		items = append(items, it)
	}
	return items, sc.Err()
}

// writeQueue overwrites the queue file with exactly items, in order --
// oldest (front of the FIFO) first. Used after every successful drain step
// so a crash mid-run loses at most the one in-flight item, not the whole
// remaining queue.
func writeQueue(path string, items []queueItem) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	for _, it := range items {
		b, err := json.Marshal(it)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// appendQueue adds newItems behind whatever's already queued, skipping any
// item that exactly duplicates an existing pending (subject, style) pair.
// This is a backstop against the TOCTOU race between two concurrent `add`
// invocations that both read the queue as empty for a subject before either
// has written -- runPromptOVerseAdd's own exclude-set check narrows this
// window but can't close it alone without file locking (founder: "the queue
// needs to be cleared out we have some duplicates queued that are not
// getting deduped").
func appendQueue(path string, newItems []queueItem) error {
	existing, err := loadQueue(path)
	if err != nil {
		return err
	}
	have := make(map[string]bool, len(existing))
	for _, it := range existing {
		have[it.Subject+"\x00"+it.StyleLabel] = true
	}
	deduped := make([]queueItem, 0, len(newItems))
	for _, it := range newItems {
		key := it.Subject + "\x00" + it.StyleLabel
		if have[key] {
			continue
		}
		have[key] = true
		deduped = append(deduped, it)
	}
	return writeQueue(path, append(existing, deduped...))
}

func runPromptOVerseQueueList() int {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	items, err := loadQueue(queuePath(cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read queue: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Println("queue is empty")
		return 0
	}
	fmt.Printf("%d pending, oldest first:\n", len(items))
	for i, it := range items {
		fmt.Printf("  %2d. %s x %s (queued %s)\n", i+1, it.StyleLabel, it.Subject, it.EnqueuedAt.Format(time.RFC3339))
	}
	return 0
}

// selectStylesForSubject picks up to `count` styles from pool to apply to a
// subject, skipping every Label in `exclude` (styles already published OR
// already queued for this exact subject -- founder: "we should not prompt
// for the tobacco card with that exact same prompt if we already have
// one"), and ordering the remainder by ascending global usage count so a
// run prefers styles under-used across the WHOLE gallery instead of always
// the first entries in the registry (founder: "it is favoring the tobacco
// card and the claymation every time ensure we have more variety"). Ties
// (equal usage) keep pool order, via SliceStable. pool is normally
// combinedStylePool's output (hardcoded registry + discovered styles), not
// promptoverseStyles directly -- "always add to the graph first" means the
// graph already includes anything discovered on a prior run.
func selectStylesForSubject(pool []style, count int, exclude map[string]bool, globalUsage map[string]int) []style {
	candidates := make([]style, 0, len(pool))
	for _, st := range pool {
		if !exclude[st.Label] {
			candidates = append(candidates, st)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return globalUsage[candidates[i].Label] < globalUsage[candidates[j].Label]
	})
	if count < len(candidates) {
		candidates = candidates[:count]
	}
	return candidates
}

func runPromptOVerseAdd(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse add <subject> <count>")
		return 1
	}
	subject := strings.TrimSpace(args[0])
	count, err := strconv.Atoi(args[1])
	if err != nil || count <= 0 {
		fmt.Fprintf(os.Stderr, "emily promptoverse add: <count> must be a positive integer, got %q\n", args[1])
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	existing, err := client.ListPromptOVerseNodes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list existing nodes (needed to dedupe): %v\n", err)
		return 1
	}

	discoveredPath := discoveredStylesPath(cfg)
	discovered, err := loadDiscoveredStyles(discoveredPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load discovered styles: %v\n", err)
		return 1
	}
	pool := combinedStylePool(discovered)

	exclude := map[string]bool{}
	globalUsage := map[string]int{}
	subjectHasPriorGeneration := false
	for _, n := range existing {
		globalUsage[n.Label]++
		if n.Subject == subject {
			exclude[n.Label] = true
			subjectHasPriorGeneration = true
		}
	}

	path := queuePath(cfg)
	pending, err := loadQueue(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read queue (needed to dedupe): %v\n", err)
		return 1
	}
	for _, it := range pending {
		if it.Subject == subject {
			exclude[it.StyleLabel] = true
			subjectHasPriorGeneration = true
		}
	}

	selected := selectStylesForSubject(pool, count, exclude, globalUsage)

	// Style discovery: only on the 2nd+ generation for a subject (a brand
	// new subject uses the existing registry only, same as the original
	// baseball-card batch did -- founder: "always add to the graph first
	// then expand it"), and only ONE attempt per add call, not one per
	// missing slot -- expansion is optional, not a quota to fill (founder:
	// "we should not add frivolous styles so the second gen will not
	// necessarily always expand the graph if it doesn't make sense to do
	// so"). The model itself can decline; that's the expected common case.
	if len(selected) < count && subjectHasPriorGeneration {
		token, tokErr := gcloudAccessToken()
		if tokErr != nil {
			fmt.Fprintf(os.Stderr, "style discovery skipped (gcloud auth: %v)\n", tokErr)
		} else {
			labels := make([]string, 0, len(pool))
			for _, st := range pool {
				labels = append(labels, st.Label)
			}
			proposed, discErr := maybeDiscoverStyle(token, subject, labels)
			switch {
			case discErr != nil:
				fmt.Fprintf(os.Stderr, "style discovery attempt failed (continuing without it): %v\n", discErr)
			case proposed == nil:
				fmt.Println("style discovery: no new style was worth adding this time")
			default:
				if err := appendDiscoveredStyle(discoveredPath, *proposed); err != nil {
					fmt.Fprintf(os.Stderr, "failed to persist discovered style %q: %v\n", proposed.Label, err)
				} else if st, ok := styleFromDiscovered(*proposed); ok {
					fmt.Printf("discovered a new style and added it to the registry: %q\n", proposed.Label)
					selected = append(selected, st)
				}
			}
		}
	}

	if len(selected) == 0 {
		fmt.Printf("already have (published or queued) every reusable style for %q — nothing new to queue\n", subject)
		return 0
	}
	if len(selected) < count {
		fmt.Printf("only %d new style(s) available for %q (%d already published or queued) — queuing what's left\n",
			len(selected), subject, count-len(selected))
	}

	now := time.Now().UTC()
	newItems := make([]queueItem, 0, len(selected))
	for _, st := range selected {
		newItems = append(newItems, queueItem{Subject: subject, StyleLabel: st.Label, EnqueuedAt: now})
	}
	if err := appendQueue(path, newItems); err != nil {
		fmt.Fprintf(os.Stderr, "enqueue: %v\n", err)
		return 1
	}
	fmt.Printf("queued %d requests for %q (FIFO — behind anything already pending)\n", len(newItems), subject)

	return drainQueue(cfg)
}

func runPromptOVerseWork() int {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	return drainQueue(cfg)
}

// drainQueue processes the queue strictly front-to-back (FIFO). On a real
// failure (rate limit or anything else) it stops immediately rather than
// skipping ahead -- an item is only ever removed from the queue after it
// actually succeeds, so nothing is silently dropped. The queue file is
// rewritten after every successful item, not just at the end, so a crash
// mid-drain loses at most the one in-flight request.
func drainQueue(cfg *config.Config) int {
	path := queuePath(cfg)
	items, err := loadQueue(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read queue: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Println("queue is empty, nothing to do")
		return 0
	}

	discovered, err := loadDiscoveredStyles(discoveredStylesPath(cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load discovered styles: %v\n", err)
		return 1
	}
	pool := combinedStylePool(discovered)

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set and not found in secrets file (need EMILY-PRIME's, which has promptoverse.write)")
		return 2
	}
	token, err := gcloudAccessToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gcloud auth: %v (is gcloud authenticated on this box?)\n", err)
		return 2
	}

	ok, skipped := 0, 0
	for len(items) > 0 {
		it := items[0]
		st, found := styleByLabelInPool(pool, it.StyleLabel)
		if !found {
			// Style no longer in the registry -- drop it rather than loop
			// on it forever, but say so.
			fmt.Fprintf(os.Stderr, "dropping queued item for unknown style %q (subject %q)\n", it.StyleLabel, it.Subject)
			items = items[1:]
			_ = writeQueue(path, items)
			continue
		}

		fmt.Fprintf(os.Stderr, "generating %s x %s...\n", st.Label, it.Subject)
		prompt := st.Prompt(it.Subject)
		img, err := vertexGenerateImage(token, prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED: %v\n", err)
			fmt.Fprintf(os.Stderr, "stopping drain -- %d item(s) still queued, left in place for 'emily promptoverse work' later\n", len(items))
			return 1
		}

		slug := fmt.Sprintf("%s-%s", slugifyPO(it.Subject), slugifyPO(st.Label))
		node := iduna.PromptOVerseNode{
			Slug:           slug,
			Label:          st.Label,
			Subject:        it.Subject,
			Kind:           st.Kind,
			EZPrompt:       fmt.Sprintf("%s %s", st.Label, it.Subject),
			ExpandedPrompt: prompt,
			ImageBase64:    base64.StdEncoding.EncodeToString(img),
			Tags:           map[string]string{"style": st.Label, "subject": it.Subject},
		}
		url, err := client.PostPromptOVerseNode(node)
		if err != nil {
			if errors.Is(err, iduna.ErrPromptOVerseNodeExists) {
				// A stale/duplicate entry (e.g. queued before the dedup fix
				// existed, or a pre-fix race) -- drop it and keep going
				// rather than jamming every request behind it forever
				// (founder: "the queue needs to be cleared out we have some
				// duplicates queued that are not getting deduped so new
				// inputs always fail").
				fmt.Fprintf(os.Stderr, "  SKIPPED (already published): %s\n", slug)
				skipped++
				items = items[1:]
				if err := writeQueue(path, items); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to persist queue after skip: %v\n", err)
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "  publish FAILED for %s: %v\n", slug, err)
			fmt.Fprintf(os.Stderr, "stopping drain -- %d item(s) still queued (image generated but not yet published, will retry the full item)\n", len(items))
			return 1
		}
		fmt.Printf("  OK -> %s\n", url)
		ok++

		items = items[1:]
		if err := writeQueue(path, items); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to persist queue after success: %v\n", err)
		}
		if len(items) > 0 {
			time.Sleep(promptoverseInterRequestDelay())
		}
	}

	fmt.Printf("\n%d published, %d skipped (already existed), queue empty.\n", ok, skipped)
	return 0
}

// promptoverseInterRequestDelay reads PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS
// if set (a positive integer), else returns promptoverseDefaultInterRequestDelay.
func promptoverseInterRequestDelay() time.Duration {
	if v := os.Getenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return promptoverseDefaultInterRequestDelay
}

func gcloudAccessToken() (string, error) {
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func vertexGenerateImage(token, prompt string) ([]byte, error) {
	url := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		promptoverseVertexRegion, promptoverseVertexProject, promptoverseVertexRegion, promptoverseVertexModel,
	)
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]string{{"text": prompt}}},
		},
	})
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 90 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex ai %d: %s", resp.StatusCode, trimMsgPO(raw))
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						Data string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode vertex response: %w", err)
	}
	for _, c := range parsed.Candidates {
		for _, p := range c.Content.Parts {
			if p.InlineData != nil && p.InlineData.Data != "" {
				return base64.StdEncoding.DecodeString(p.InlineData.Data)
			}
		}
	}
	return nil, fmt.Errorf("no image data in response")
}

func slugifyPO(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func trimMsgPO(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
