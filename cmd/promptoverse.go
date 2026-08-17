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
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
	promptoverseVertexProject = "project-d24a71e9-2daf-4b2d-917"
	promptoverseVertexRegion  = "us-central1"
	promptoverseVertexModel   = "gemini-2.5-flash-image"

	promptoverseInterRequestDelay = 6 * time.Second // spacing between successful Vertex AI calls
	promptoverseQueueFileName     = "promptoverse-queue.jsonl"
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
}

func styleByLabel(label string) (style, bool) {
	for _, st := range promptoverseStyles {
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
remainder queued for 'emily promptoverse work' later.

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

func appendQueue(path string, newItems []queueItem) error {
	existing, err := loadQueue(path)
	if err != nil {
		return err
	}
	return writeQueue(path, append(existing, newItems...))
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
	if count > len(promptoverseStyles) {
		fmt.Printf("only %d styles in the registry — queuing all of them (run 'emily promptoverse styles' to see the list)\n", len(promptoverseStyles))
		count = len(promptoverseStyles)
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	now := time.Now().UTC()
	newItems := make([]queueItem, 0, count)
	for _, st := range promptoverseStyles[:count] {
		newItems = append(newItems, queueItem{Subject: subject, StyleLabel: st.Label, EnqueuedAt: now})
	}
	path := queuePath(cfg)
	if err := appendQueue(path, newItems); err != nil {
		fmt.Fprintf(os.Stderr, "enqueue: %v\n", err)
		return 1
	}
	fmt.Printf("queued %d requests for %q (FIFO — behind anything already pending)\n", count, subject)

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

	ok := 0
	for len(items) > 0 {
		it := items[0]
		st, found := styleByLabel(it.StyleLabel)
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
			time.Sleep(promptoverseInterRequestDelay)
		}
	}

	fmt.Printf("\n%d published, queue empty.\n", ok)
	return 0
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
