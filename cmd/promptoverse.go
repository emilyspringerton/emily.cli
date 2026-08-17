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
	"math/rand"
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

	// promptoverseInterRequestGrowth: each successful request already made
	// THIS run adds this much extra spacing before the next one, on top of
	// the base delay above, capped at promptoverseInterRequestGrowthCap.
	// Founder, real-time: "we are still getting apilimited in like our 3rd
	// or 4th gen usually" -- a flat delay wasn't enough once a run had made
	// a few requests in a short window, which looks like Vertex AI's actual
	// limit here behaves more like "requests per minute" than "seconds
	// since the last one." Growing the gap as a run progresses (20s, 35s,
	// 50s, 65s, ...) targets that directly instead of just raising the flat
	// floor again.
	promptoverseInterRequestGrowth    = 15 * time.Second
	promptoverseInterRequestGrowthCap = 2 * time.Minute

	promptoverseQueueFileName      = "promptoverse-queue.jsonl"
	promptoverseDiscoveredFileName = "promptoverse-discovered-styles.json"
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
	// agnostic transformation concepts, same bar as the 10 above. The
	// batch's other 3 Labels (ice cream novelty, 1990s glossy rookie card,
	// 2020s Topps Chrome refractor) are too baseball-card-specific to
	// select every time, but not excluded outright either -- see
	// promptoverseRareStyles below.
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
	// Founder-named additions (2026-08-17): "add these as top level hard
	// coded styles to potentially pull from - Whiteboard, Paper-craft,
	// Anime, Kawaii." Same bar as everything above -- subject-agnostic
	// transformation concepts, not tied to one original subject.
	{"whiteboard", "surreal", func(s string) string {
		return fmt.Sprintf("%s drawn as a quick whiteboard marker sketch, loose expressive "+
			"linework, visible marker squeak texture, faint ghosting from erased previous "+
			"drawings, dry-erase board surface.", s)
	}},
	{"paper-craft", "surreal", func(s string) string {
		return fmt.Sprintf("%s built from layered paper-craft cutouts, folded and glued "+
			"cardstock forms, visible paper edges and shadow gaps between layers, "+
			"diorama-style studio lighting.", s)
	}},
	{"anime", "surreal", func(s string) string {
		return fmt.Sprintf("%s illustrated in 1990s cel-shaded anime style, bold linework, "+
			"flat saturated color blocks, dramatic speed lines, expressive large eyes.", s)
	}},
	{"kawaii", "surreal", func(s string) string {
		return fmt.Sprintf("%s reimagined in kawaii style, rounded chibi proportions, "+
			"pastel color palette, oversized sparkling eyes, tiny blush marks, soft "+
			"rounded shading.", s)
	}},
}

// promptoverseRareStyles are styles judged too subject-specific to compete
// for a slot every time (they'd rarely be the best "under-used" pick for
// an unrelated subject) but that shouldn't be permanently locked out
// either -- founder: "the too specific ones should still trigger on a
// somewhat rare basis... like the shiny tops [Topps]." selectStylesForSubject
// treats these exactly like any other style once they clear
// promptoverseRareStyleChance for a given `add` run (see runPromptOVerseAdd);
// discoveredStyle.Rare marks the same behavior for anything promoted or
// auto-discovered later, not just this hardcoded set.
var promptoverseRareStyles = []style{
	{"ice cream novelty", "surreal", func(s string) string {
		return fmt.Sprintf("%s as an ice cream truck novelty treat, wax-paper wrapper, "+
			"pastel swirl texture, cartoonish sprinkles, kitschy summer-fair photography.", s)
	}},
	{"1990s glossy rookie card", "historical", func(s string) string {
		return fmt.Sprintf("%s rendered as a glossy 1990s rookie trading card, chrome foil "+
			"border, bold neon accent color block, high-gloss studio photo finish.", s)
	}},
	{"2020s Topps Chrome refractor", "historical", func(s string) string {
		return fmt.Sprintf("%s rendered as a 2020s Topps Chrome refractor trading card, "+
			"rainbow prismatic foil shimmer, sharp modern photography, holographic "+
			"refractor pattern across the surface.", s)
	}},
}

func styleByLabel(label string) (style, bool) {
	return styleByLabelInPool(promptoverseStyles, label)
}

// promptoverseRareStyleChance / promptoverseSpontaneousDiscoveryChance are
// the "somewhat rare" probabilities behind two related asks: rare styles
// competing for a slot sometimes instead of never (founder: "the too
// specific ones should still trigger on a somewhat rare basis"), and a
// brand new style occasionally emerging even without --tag (founder: "when
// i am querying for new stuff i should occasionally see a new style
// category emerge without using the --tag flag"). Same rough odds --
// roughly 1 in 7 -- for both; no principled reason for them to differ yet.
const (
	promptoverseRareStyleChance            = 1.0 / 7.0
	promptoverseSpontaneousDiscoveryChance = 1.0 / 7.0
)

// chanceTriggered is a pure, directly-testable wrapper around a random
// draw vs. a probability -- kept separate from the actual math/rand call
// so tests don't depend on real randomness.
func chanceTriggered(roll, chance float64) bool {
	return roll < chance
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
		return runPromptOVerseWork(args[1:])
	case "queue":
		return runPromptOVerseQueueList()
	case "styles":
		return runPromptOVerseStyles()
	case "brainstorm":
		return runPromptOVerseBrainstorm(args[1:])
	case "requeue":
		return runPromptOVerseRequeue()
	case "promote":
		return runPromptOVersePromote(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "emily promptoverse: unknown subcommand %q\n\n", args[0])
		return promptoverseUsage()
	}
}

func promptoverseUsage() int {
	fmt.Print(`emily promptoverse — generate + publish Prompt-o-verse gallery nodes

Subcommands:
  emily promptoverse add <subject> <count> [--force] [--slow] [--tag <style>]   Queue <count> styles applied to <subject>, then drain
  emily promptoverse work [--force] [--slow]           Drain whatever's already queued (e.g. resume after a 429)
  emily promptoverse queue                             List pending queue entries, oldest first
  emily promptoverse requeue                            Re-pick styles for everything still queued (skips --tag-forced items)
  emily promptoverse styles                            List the reusable style registry
  emily promptoverse brainstorm [--seed "a, b, c"] [--sample N]   Prompt GPT-2 for candidate style tags
                                [--max-tokens N] [--temperature F] [--via server|proxy|emily]
  emily promptoverse promote <label> [--rare]           Turn a candidate/name into a real persisted style

Example:
  emily promptoverse add ducks 6
  emily promptoverse add princess 4 --tag gladiator
    Forces "gladiator" as one of the 4 (creating it via Vertex AI if it's
    not already a known style), then fills the remaining 3 through the
    normal deduped/variety-weighted selection.

Requests are queued FIFO to a durable file, not fired immediately -- if
another 'add' is already mid-flight or queued, new requests wait their turn
in arrival order. Draining stops (not retries) on a rate limit, leaving the
remainder queued for 'emily promptoverse work' later. 20s between successful
requests by default -- override with PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS.
That gap also grows +15s per successful request already made THIS run
(capped at +2m), since the API's real limit behaves more like requests/min
than seconds-since-last-request -- the flat delay alone wasn't enough by
the 3rd or 4th generation in a run.

Adaptive backoff: if recent runs hit API overload, the NEXT run waits a
little longer before its very first request too (not just between retries),
scaling with how many times in a row it's recently failed -- and that same
extra wait is added to every gap for the rest of that run, not just the
first one. --force skips all of that for one run (bookkeeping still happens).
--slow doubles every wait this command applies (base delay, growth, and any
backoff extra) -- orthogonal to --force, which zeroes the backoff extra
before --slow doubles whatever's left.

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
	// Forced marks an item explicitly picked via --tag -- founder: "it
	// should requeue on every run except if --tag hard coded a tag."
	// Everything else is fair game for auto-requeue to re-pick.
	Forced bool `json:"forced,omitempty"`
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

// requeueQueue re-picks the style for every NON-forced item still pending
// using the CURRENT selection logic, in place -- original list order,
// subject grouping, and item count are all preserved exactly; only the
// StyleLabel of each non-forced item changes. Forced items (Forced==true,
// set only by --tag) are left completely untouched -- founder: "it should
// requeue on every run except if --tag hard coded a tag." Returns the
// number of items actually re-picked (0 is a valid, common result -- an
// all-forced or already-fresh queue).
//
// Originally built as a one-off fix (founder, real-time: "i have 100 gens
// queued already with the bad rng - can we have a requeue function that
// rediscovers/marble-bag-rng-selects the tag styles" -- confirmed live: 8
// robot, 8 whiteboard, 7 underwater, 5 outer space were sitting in queue,
// artificially "fresh" to the old strict-ascending sort because usage was
// only tallied from published nodes) and then promoted to run
// automatically at the top of every drainQueue, not just on manual
// request.
func requeueQueue(cfg *config.Config) (int, error) {
	path := queuePath(cfg)
	pending, err := loadQueue(path)
	if err != nil {
		return 0, fmt.Errorf("read queue: %w", err)
	}
	if len(pending) == 0 {
		return 0, nil
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	existingNodes, err := client.ListPromptOVerseNodes()
	if err != nil {
		return 0, fmt.Errorf("list existing nodes: %w", err)
	}
	discovered, err := loadDiscoveredStyles(discoveredStylesPath(cfg))
	if err != nil {
		return 0, fmt.Errorf("load discovered styles: %w", err)
	}
	pool := combinedStylePool(discovered)

	globalUsage := map[string]int{}
	excludeBySubject := map[string]map[string]bool{}
	ensureExclude := func(subject string) map[string]bool {
		if excludeBySubject[subject] == nil {
			excludeBySubject[subject] = map[string]bool{}
		}
		return excludeBySubject[subject]
	}
	for _, n := range existingNodes {
		globalUsage[n.Label]++
		ensureExclude(n.Subject)[n.Label] = true
	}

	wantBySubject := map[string]int{}
	for _, it := range pending {
		if it.Forced {
			// A forced item still occupies its style for this subject --
			// don't let a fresh pick collide with it.
			ensureExclude(it.Subject)[it.StyleLabel] = true
			globalUsage[it.StyleLabel]++
			continue
		}
		wantBySubject[it.Subject]++
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	freshBySubject := map[string][]style{}
	shortfalls := map[string]int{}
	for subject, want := range wantBySubject {
		exclude := map[string]bool{}
		for label := range excludeBySubject[subject] {
			exclude[label] = true
		}
		picked := selectStylesForSubject(pool, want, exclude, globalUsage, rng)
		for _, st := range picked {
			globalUsage[st.Label]++ // so later subjects in this same pass see it as used
		}
		freshBySubject[subject] = picked
		if len(picked) < want {
			shortfalls[subject] = want - len(picked)
		}
	}

	touched := 0
	newItems := make([]queueItem, 0, len(pending))
	for _, it := range pending {
		if it.Forced {
			newItems = append(newItems, it)
			continue
		}
		fresh := freshBySubject[it.Subject]
		if len(fresh) == 0 {
			// Registry ran short for this subject -- drop the item rather
			// than leave it with a stale/duplicate label.
			touched++
			continue
		}
		newItems = append(newItems, queueItem{Subject: it.Subject, StyleLabel: fresh[0].Label, EnqueuedAt: it.EnqueuedAt})
		freshBySubject[it.Subject] = fresh[1:]
		touched++
	}

	for subject, n := range shortfalls {
		fmt.Fprintf(os.Stderr, "warning: only found enough new styles for %d/%d of %q's queued items -- registry may be running short for this subject\n",
			wantBySubject[subject]-n, wantBySubject[subject], subject)
	}

	if err := writeQueue(path, newItems); err != nil {
		return 0, fmt.Errorf("write queue: %w", err)
	}
	return touched, nil
}

func runPromptOVerseRequeue() int {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	touched, err := requeueQueue(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "requeue: %v\n", err)
		return 1
	}
	if touched == 0 {
		fmt.Println("nothing to requeue (queue empty, or every pending item is --tag forced)")
		return 0
	}
	fmt.Printf("requeued %d item(s) using current marble-bag selection\n", touched)
	return 0
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
// one"). The remainder is chosen by weighted random sampling WITHOUT
// replacement, weighted 1/(usage+1) -- under-used styles are more LIKELY
// to be picked, not guaranteed to be. A strict ascending-usage sort was
// tried first and was wrong in practice: it always filled in whichever
// single style had the globally lowest count, so once one style
// accumulated a lead (founder's example: tobacco card) it could never
// come back, and an under-but-not-least-used style (founder's example:
// "watercolor is fire but we havent generated one in quite a few gens
// now") stayed starved just as hard as an over-used one.
// Founder: "it should favor the less represented styles but also be
// random." pool is normally combinedStylePool's output (hardcoded
// registry + discovered styles) -- "always add to the graph first" means
// the graph already includes anything discovered on a prior run.
func selectStylesForSubject(pool []style, count int, exclude map[string]bool, globalUsage map[string]int, rng *rand.Rand) []style {
	remaining := make([]style, 0, len(pool))
	for _, st := range pool {
		if !exclude[st.Label] {
			remaining = append(remaining, st)
		}
	}

	selected := make([]style, 0, count)
	for len(selected) < count && len(remaining) > 0 {
		weights := make([]float64, len(remaining))
		total := 0.0
		for i, st := range remaining {
			w := 1.0 / float64(globalUsage[st.Label]+1)
			weights[i] = w
			total += w
		}
		draw := rng.Float64() * total
		idx := len(remaining) - 1 // float rounding safety net -- last item if the loop below never hits
		cum := 0.0
		for i, w := range weights {
			cum += w
			if draw < cum {
				idx = i
				break
			}
		}
		selected = append(selected, remaining[idx])
		remaining = append(remaining[:idx], remaining[idx+1:]...)
	}
	return selected
}

func runPromptOVerseAdd(args []string) int {
	force := false
	slow := false
	tag := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--force":
			force = true
		case a == "--slow":
			slow = true
		case a == "--tag":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse add: --tag requires a value")
				return 1
			}
			tag = args[i+1]
			i++
		case strings.HasPrefix(a, "--tag="):
			tag = strings.TrimPrefix(a, "--tag=")
		default:
			rest = append(rest, a)
		}
	}
	args = rest

	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse add <subject> <count> [--force] [--slow] [--tag <style>]")
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
		// Counted toward globalUsage regardless of subject -- founder,
		// real-time: "tag selection is wonky you are including underwater
		// and robot and outerspace over and over again." Root cause: usage
		// was only tallied from PUBLISHED nodes, so a style sitting queued
		// dozens of times behind a slow drain still looked "fresh" (0
		// uses) to every new `add` call and kept winning the least-used
		// tiebreak. A style is "used" the moment it's queued, not only
		// once it's actually published.
		globalUsage[it.StyleLabel]++
		if it.Subject == subject {
			exclude[it.StyleLabel] = true
			subjectHasPriorGeneration = true
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pityPath := pityStatePath(cfg)
	pity, err := loadPityState(pityPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load pity state: %v\n", err)
		return 1
	}

	// Rare styles (promptoverseRareStyles + discoveredStyle.Rare) are
	// excluded by default -- competing for a slot every time would defeat
	// "rare" -- but the whole group gets one per-run roll to become
	// eligible anyway (founder: "the too specific ones should still
	// trigger on a somewhat rare basis"), and that roll's odds escalate
	// the longer it's been since the last hit (founder: "like the same way
	// a legendary pull percentage goes up after opening a certain number
	// of packs" -- see promptoverse_pity.go). A style explicitly requested
	// via --tag below bypasses this entirely, same as it bypasses
	// everything else about normal selection.
	rareTriggered := chanceTriggered(rng.Float64(), pityAdjustedChance(promptoverseRareStyleChance, pity.RareStyleRunsSinceTrigger))
	if rareTriggered {
		pity.RareStyleRunsSinceTrigger = 0
	} else {
		pity.RareStyleRunsSinceTrigger++
		for label := range rareStyleLabels(discovered) {
			exclude[label] = true
		}
	}

	// --tag forces one specific style into this batch, whether or not it's
	// already in the registry -- founder: "princess 4 --tag gladiator
	// forces a new or already existing style gladiator and then queues 3
	// more princess via the deduped tag selection process already
	// established." A tag that would duplicate what's already
	// published/queued for this subject is NOT force-added (dedup still
	// wins) -- it just falls through to the normal selection below.
	selected := make([]style, 0, count)
	forcedLabel := ""
	if tag = strings.TrimSpace(tag); tag != "" {
		switch {
		case exclude[tag]:
			fmt.Printf("%q already has style %q (published or queued) -- ignoring --tag, queuing %d via normal selection\n", subject, tag, count)
		default:
			forced, ok := styleByLabelInPool(pool, tag)
			if !ok {
				token, tokErr := gcloudAccessToken()
				if tokErr != nil {
					fmt.Fprintf(os.Stderr, "cannot create new style %q for --tag (gcloud auth: %v)\n", tag, tokErr)
					return 2
				}
				newStyle, expErr := expandNamedStyle(token, tag, subject)
				if expErr != nil {
					fmt.Fprintf(os.Stderr, "failed to create new style %q for --tag: %v\n", tag, expErr)
					return 1
				}
				if err := appendDiscoveredStyle(discoveredPath, *newStyle); err != nil {
					fmt.Fprintf(os.Stderr, "failed to persist forced style %q: %v\n", tag, err)
					return 1
				}
				st, ok := styleFromDiscovered(*newStyle)
				if !ok {
					fmt.Fprintf(os.Stderr, "model produced a malformed template for %q\n", tag)
					return 1
				}
				forced = st
				pool = append(pool, st)
				fmt.Printf("forced a new style %q into the registry\n", tag)
			} else {
				fmt.Printf("forcing existing style %q\n", tag)
			}
			selected = append(selected, forced)
			exclude[tag] = true
			forcedLabel = tag
		}
	}
	selected = append(selected, selectStylesForSubject(pool, count-len(selected), exclude, globalUsage, rng)...)

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
	} else if subjectHasPriorGeneration {
		// Spontaneous discovery: the batch was already full, so this isn't
		// filling a gap -- founder: "when i am querying for new stuff i
		// should occasionally see a new style category emerge without
		// using the --tag flag," with the same pity escalation as the rare-
		// style roll above. On a hit, swap the newly discovered style in
		// for the last selected slot (arbitrary but simple -- the marble-
		// bag draw above doesn't rank its picks by preference, so there's
		// no principled "least valuable" slot to target instead).
		discoveryTriggered := chanceTriggered(rng.Float64(), pityAdjustedChance(promptoverseSpontaneousDiscoveryChance, pity.DiscoveryRunsSinceTrigger))
		if !discoveryTriggered {
			pity.DiscoveryRunsSinceTrigger++
		} else {
			token, tokErr := gcloudAccessToken()
			if tokErr != nil {
				fmt.Fprintf(os.Stderr, "spontaneous style discovery skipped (gcloud auth: %v)\n", tokErr)
			} else {
				labels := make([]string, 0, len(pool))
				for _, st := range pool {
					labels = append(labels, st.Label)
				}
				proposed, discErr := maybeDiscoverStyle(token, subject, labels)
				switch {
				case discErr != nil:
					fmt.Fprintf(os.Stderr, "spontaneous style discovery attempt failed (continuing without it): %v\n", discErr)
					pity.DiscoveryRunsSinceTrigger++
				case proposed == nil:
					// The model declining doesn't count as a "miss" for
					// pity purposes -- the roll DID trigger, the model
					// just had nothing good to propose this time.
					pity.DiscoveryRunsSinceTrigger = 0
				default:
					pity.DiscoveryRunsSinceTrigger = 0
					if err := appendDiscoveredStyle(discoveredPath, *proposed); err != nil {
						fmt.Fprintf(os.Stderr, "failed to persist discovered style %q: %v\n", proposed.Label, err)
					} else if st, ok := styleFromDiscovered(*proposed); ok && len(selected) > 0 {
						fmt.Printf("a new style category emerged and replaced a slot: %q\n", proposed.Label)
						selected[len(selected)-1] = st
					}
				}
			}
		}
	}

	if err := savePityState(pityPath, pity); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to persist pity state: %v\n", err)
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
		newItems = append(newItems, queueItem{
			Subject: subject, StyleLabel: st.Label, EnqueuedAt: now,
			Forced: forcedLabel != "" && st.Label == forcedLabel,
		})
	}
	if err := appendQueue(path, newItems); err != nil {
		fmt.Fprintf(os.Stderr, "enqueue: %v\n", err)
		return 1
	}
	fmt.Printf("queued %d requests for %q (FIFO — behind anything already pending)\n", len(newItems), subject)

	return drainQueue(cfg, force, slow)
}

func runPromptOVerseWork(args []string) int {
	force := false
	slow := false
	for _, a := range args {
		switch a {
		case "--force":
			force = true
		case "--slow":
			slow = true
		}
	}
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	return drainQueue(cfg, force, slow)
}

// drainQueue processes the queue strictly front-to-back (FIFO). On a real
// failure (rate limit or anything else) it stops immediately rather than
// skipping ahead -- an item is only ever removed from the queue after it
// actually succeeds, so nothing is silently dropped. The queue file is
// rewritten after every successful item, not just at the end, so a crash
// mid-drain loses at most the one in-flight request.
//
// Before doing anything else it consults the persisted backoff state
// (promptoverse_backoff.go): if recent runs hit API overload, THIS run
// waits a little longer before its very first request too, not just
// between retries within one run -- three separate invocations in a row
// that each hit a 429 make the third one preemptively cautious. force
// skips that wait for this one run without disabling the bookkeeping.
//
// The gap between successive requests within a single run also grows as
// the run goes on (promptoverseEffectiveDelay) -- founder, real-time: "we
// are still getting apilimited in like our 3rd or 4th gen usually," so a
// flat per-request delay wasn't enough on its own.
//
// slow doubles every wait this function applies (preemptive backoff, and
// every gap between requests) -- founder: "add --slow flag that doubles
// waits." Orthogonal to force: force zeroes out the backoff-driven extras,
// slow then doubles whatever's left (including just the base+growth
// portion if force was also given).
func drainQueue(cfg *config.Config, force, slow bool) int {
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

	// Auto-requeue: every drain re-picks non-forced items with the CURRENT
	// selection logic before generating anything -- founder: "it should
	// requeue on every run except if --tag hard coded a tag." Keeps a
	// queue that's been sitting for a while (registry/usage shifted since
	// it was enqueued) from draining stale picks.
	if touched, err := requeueQueue(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "auto-requeue failed (continuing with the queue as-is): %v\n", err)
	} else if touched > 0 {
		fmt.Fprintf(os.Stderr, "auto-requeued %d item(s) before draining\n", touched)
		items, err = loadQueue(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read queue after auto-requeue: %v\n", err)
			return 1
		}
	}

	backoffPath := backoffStatePath(cfg)
	var backoffExtra time.Duration
	if !force {
		st, err := loadBackoffState(backoffPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load backoff state: %v\n", err)
			return 1
		}
		backoffExtra = backoffWaitFor(st, time.Now().UTC())
		preWait := backoffExtra
		if slow {
			preWait *= 2
		}
		if preWait > 0 {
			fmt.Fprintf(os.Stderr, "%d recent consecutive failure(s) -- waiting an extra %s before the first request (use --force to skip)\n",
				st.ConsecutiveFailures, preWait)
			time.Sleep(preWait)
		}
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
			if errors.Is(err, errVertexContentBlocked) {
				// A permanent policy rejection, not a transient failure --
				// retrying gets the identical result forever, which would
				// otherwise jam every other queued request behind it
				// (founder-visible, 2026-08-17: "anime x Rapunzel" hit
				// this and looked exactly like an API-key problem).
				fmt.Fprintf(os.Stderr, "  SKIPPED (content policy): %s x %s -- %v\n", st.Label, it.Subject, err)
				skipped++
				items = items[1:]
				if err := writeQueue(path, items); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to persist queue after skip: %v\n", err)
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "  FAILED: %v\n", err)
			fmt.Fprintf(os.Stderr, "stopping drain -- %d item(s) still queued, left in place for 'emily promptoverse work' later\n", len(items))
			recordBackoffFailure(backoffPath)
			return 1
		}
		clearBackoffState(backoffPath)

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
			delay := promptoverseEffectiveDelay(ok, backoffExtra, slow)
			fmt.Fprintf(os.Stderr, "  waiting %s before the next request...\n", delay)
			time.Sleep(delay)
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

// promptoverseEffectiveDelay is the actual spacing before the NEXT request
// in a run: the configurable base, plus promptoverseInterRequestGrowth for
// every successful request already made this run (capped), plus any
// cross-invocation backoff extra (0 when --force skipped it), all doubled
// if slow is set (founder: "add --slow flag that doubles waits").
// successesSoFar resets to 0 every new invocation -- deliberately not
// persisted the way the cross-run backoff state is, since it's tracking
// "how far into THIS burst am I," a different question than "how many
// recent runs failed."
func promptoverseEffectiveDelay(successesSoFar int, backoffExtra time.Duration, slow bool) time.Duration {
	growth := time.Duration(successesSoFar) * promptoverseInterRequestGrowth
	if growth > promptoverseInterRequestGrowthCap {
		growth = promptoverseInterRequestGrowthCap
	}
	delay := promptoverseInterRequestDelay() + growth + backoffExtra
	if slow {
		delay *= 2
	}
	return delay
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
	return parseVertexImageResponse(raw)
}

// parseVertexImageResponse pulls the base64 image out of a successful (200)
// Vertex AI generateContent response, or explains why there isn't one.
// Split out from vertexGenerateImage so the finishReason handling is
// testable against real captured response bodies without a live network
// call. No image in the response -- surface WHY, not just that it's
// missing. Live example that prompted this (2026-08-17): "no image data in
// response" alone looked like an auth/key problem; the real cause was
// finishReason "IMAGE_PROHIBITED_CONTENT" (Vertex's own third-party IP
// filter, triggered by a subject like "Rapunzel") -- a real, expected
// platform decision, not a bug, but undiagnosable without this text.
func parseVertexImageResponse(raw []byte) ([]byte, error) {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						Data string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason  string `json:"finishReason"`
			FinishMessage string `json:"finishMessage"`
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
	for _, c := range parsed.Candidates {
		if c.FinishReason != "" && c.FinishReason != "STOP" {
			msg := c.FinishReason
			if c.FinishMessage != "" {
				msg += ": " + c.FinishMessage
			}
			if vertexContentBlockReasons[c.FinishReason] {
				return nil, fmt.Errorf("%w: %s", errVertexContentBlocked, msg)
			}
			return nil, fmt.Errorf("no image data in response (%s)", msg)
		}
	}
	return nil, fmt.Errorf("no image data in response")
}

// errVertexContentBlocked marks a PERMANENT rejection -- Vertex's own
// safety/IP-content policy declined this exact prompt, not a transient
// failure retrying would fix. drainQueue skips-and-continues on this
// specific error instead of stopping the whole run, same shape as
// iduna.ErrPromptOVerseNodeExists. Discovered live (2026-08-17): "anime x
// Rapunzel" hit IMAGE_PROHIBITED_CONTENT (a real Disney-IP filter) and,
// before this fix, looked exactly like an auth failure while also
// permanently jamming everything queued behind it.
var errVertexContentBlocked = errors.New("vertex ai: content policy blocked this prompt")

var vertexContentBlockReasons = map[string]bool{
	"IMAGE_PROHIBITED_CONTENT": true,
	"PROHIBITED_CONTENT":       true,
	"SAFETY":                   true,
	"RECITATION":               true,
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
