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

// promptoverseSpriteStyleLabel marks a style as sprite-generation output --
// checked by drainQueue right after generation, before upload, to run the
// chroma-key background-removal post-process. Founder, real-time: "feel
// free to create a whole sprite generation subsystem for promptoverse if
// that helps" (offered while building BRAWLPIT's first 4 pixel-art
// characters, whose source images were honest-but-imperfect full scenes --
// a moon, a beach -- not clean game-ready portraits). This is that
// subsystem's first real style: request a background this pipeline can
// reliably strip out programmatically, rather than trying to prompt for
// "transparent" (image models don't reliably produce real alpha) or
// attempting ML-based background segmentation (a much bigger, separate
// undertaking than a one-session addition).
const promptoverseSpriteStyleLabel = "game sprite"
const promptoverseSpriteChromaKeyHex = "#00FF00"

// promptoverseHatStyleLabel — kanban HSG-000 ("promptoverse hat gen - we already query for the
// sprites with the specific background and give them a good prompt for a nice pixel art hat as
// the tag promptoverse hat pirate"). A standalone cosmetic item render (a hat/headwear object
// on its own, not a full character wearing it), for BRAWLPIT's own real hat-store catalog
// (WOTAN_HAT_STORE_NORTHSTAR.md Phase 4's own "user-generated hats" ask, kanban BPHS-00001's
// own real dependency). Same real chroma-key convention promptoverseSpriteStyleLabel already
// established, reused rather than reinvented, for the same real reason (image models don't
// reliably produce true alpha transparency).
const promptoverseHatStyleLabel = "promptoverse hat"

var promptoverseStyles = []style{
	{promptoverseHatStyleLabel, "surreal", func(s string) string {
		return fmt.Sprintf("A single standalone pixel-art hat/headwear item themed around %s, "+
			"centered, isometric 3/4 product-icon angle, no head or character wearing it -- just "+
			"the hat object by itself, clean bold pixel-art outlines, limited retro color "+
			"palette, video-game inventory-icon style -- on a SOLID PURE GREEN chroma-key "+
			"background (%s), absolutely no scenery, no props, no shadows, no gradient, no "+
			"texture behind the item, only flat solid green fills the entire background.", s, promptoverseSpriteChromaKeyHex)
	}},
	{promptoverseSpriteStyleLabel, "surreal", func(s string) string {
		return fmt.Sprintf("A full-body character portrait of %s, centered, facing forward, "+
			"standing in a neutral idle pose, video-game-sprite proportions, clean bold outlines, "+
			"flat cel-shaded coloring -- on a SOLID PURE GREEN chroma-key background (%s), "+
			"absolutely no scenery, no props, no shadows on the background, no gradient, no texture "+
			"behind the character, only flat solid green fills the entire background.", s, promptoverseSpriteChromaKeyHex)
	}},
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
	// Best-effort, every invocation: keep the hardcoded-registry export
	// fresh for IDUNA's discovery page (GET /api/v1/promptoverse/discovery)
	// to read -- the hardcoded list only changes when this binary is
	// rebuilt, so "refresh on every command" is simpler than tracking when
	// it actually changed, and cheap enough not to matter.
	if cfg, err := config.Resolve(); err == nil {
		_ = writeHardcodedStylesSnapshot(cfg)
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
	case "promote-subject":
		return runPromptOVersePromoteSubject(args[1:])
	case "mashups":
		return runPromptOVerseMashups(args[1:])
	case "regenerate":
		return runPromptOVerseRegenerate(args[1:])
	case "annotations":
		return runPromptOVerseAnnotations(args[1:])
	case "backfill-annotation":
		return runPromptOVerseBackfillAnnotation(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "emily promptoverse: unknown subcommand %q\n\n", args[0])
		return promptoverseUsage()
	}
}

func promptoverseUsage() int {
	fmt.Print(`emily promptoverse — generate + publish Prompt-o-verse gallery nodes

Subcommands:
  emily promptoverse add [<subject>] <count> [--force] [--slow] [--tag <style>]...
                         [--annotation "text" | --annotation-from-lore] [--annotation-alias NAME]
                                                        Queue <count> styles, then drain
  emily promptoverse work [--force] [--slow]           Drain whatever's already queued (e.g. resume after a 429)
  emily promptoverse queue                             List pending queue entries, oldest first
  emily promptoverse requeue                            Re-pick styles for everything still queued (skips --tag-forced items)
  emily promptoverse styles                            List the reusable style registry
  emily promptoverse brainstorm [--target styles|subjects] [--seed "a, b, c"] [--sample N]   Prompt GPT-2 for candidates
                                [--max-tokens N] [--temperature F] [--via server|proxy|emily]
  emily promptoverse promote <label> [--rare]           Turn a candidate/name into a real persisted style
  emily promptoverse promote-subject <label> [--rare]   Turn a candidate/name into a real known subject
  emily promptoverse mashups [--target subjects|styles] [--provider gemini|claude|all] [--subject <label>]
                                LLM-judge which subjects/styles are genuine compositional mashups
                                (two SUBJECTS combined -- see "hybrid" below for two STYLES) or
                                paraphrase-equivalent duplicates -- NOT string matching, see
                                internal/mashupjudge and NORTHSTAR_PROMPT_O_VERSE.md §9 for why
  emily promptoverse regenerate <slug> --note "..."     Additive "regenerate with variation" -- posts a
                                                        new variant on the SAME leaf page, never overwrites
  emily promptoverse annotations [list|set|clear] ...   Manage subject-level prompt annotations (see below)
  emily promptoverse backfill-annotation <subject>      Mark already-published nodes as pre-annotation

Example:
  emily promptoverse add ducks 6
  emily promptoverse add princess 4 --tag gladiator
    Forces "gladiator" as one of the 4 (creating it via Vertex AI if it's
    not already a known style), then fills the remaining 3 through the
    normal deduped/variety-weighted selection.
  emily promptoverse add 6
    No subject given -- auto-picks one via the same weighted "marble bag"
    styles use, from every subject ever published (or discovered). Can
    also propose a brand new subject on a pity-adjusted chance, STYLE-
    ANCHORED: picks one style via the same weighted scheme, asks Vertex
    for that style's archetypal subject, and tries another weighted-
    picked style if declined -- until one is discovered or every style
    has been tried.
  emily promptoverse add 5 --tag "game sprite"
    STYLE SWEEP -- no subject AND a --tag together flips <count>'s
    meaning: 5 DIFFERENT auto-picked subjects, all locked to "game
    sprite" (creating it once via Vertex AI if new), not "game sprite" as
    one of 5 styles for a single subject. Never repeats a subject already
    picked this sweep or one that already has this style.

Style hybrids -- passing --tag more than once does NOT force N separate
generations. It combines the tags into ONE new blended style ("hybrid",
distinct from "mashup" above which combines two SUBJECTS not styles):
  emily promptoverse add Medusa --tag kawaii --tag FFXI
    Creates one "kawaii × FFXI" style (via Vertex AI, same as a single
    --tag) and generates exactly one Medusa image in it -- not a kawaii
    Medusa AND a separate FFXI Medusa. The published node is tagged
    style_hybrid_of="kawaii, FFXI" (visible on its gallery page).

Subject annotations -- for a subject whose bare name collides with real
third-party IP (e.g. "Paimon": TYLER's own Goetia-king hero vs. Genshin
Impact's companion character), an annotation sticks to the SUBJECT and is
appended only to the real generation prompt sent to Vertex AI -- never to
the EZ prompt, taxonomy, or slug, which all stay exactly "Paimon" forever:
  emily promptoverse add Paimon --annotation-from-lore
    Auto-derives disambiguating context from TYLER's hero compendium
    (multiverse_heroes.md) + the Goetia frequency table, sets it as this
    subject's default, and applies it to this batch.
  emily promptoverse annotations set Paimon --alias genshin-impact --text "..."
    Registers a second, non-default alias for deliberate one-off use via
    --annotation-alias genshin-impact, without ever renaming the subject.

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

// hardcodedStyleSnapshot is one entry in the exported hardcoded-registry
// file -- IDUNA has no access to promptoverseStyles/promptoverseRareStyles
// (they're Go source in this binary, not data), so this is the only way
// the discovery page (founder: "wheres the link to the tag discovery page
// with the candidates for style promotion") can show the full picture.
type hardcodedStyleSnapshot struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Rare  bool   `json:"rare"`
}

func hardcodedStylesSnapshotPath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", "promptoverse-hardcoded-styles.json")
}

func writeHardcodedStylesSnapshot(cfg *config.Config) error {
	snap := make([]hardcodedStyleSnapshot, 0, len(promptoverseStyles)+len(promptoverseRareStyles))
	for _, st := range promptoverseStyles {
		snap = append(snap, hardcodedStyleSnapshot{Label: st.Label, Kind: st.Kind})
	}
	for _, st := range promptoverseRareStyles {
		snap = append(snap, hardcodedStyleSnapshot{Label: st.Label, Kind: st.Kind, Rare: true})
	}
	path := hardcodedStylesSnapshotPath(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
	// AnnotationAlias, if set, overrides which of the subject's stored
	// annotation aliases gets applied at generation time -- empty means
	// "use the subject's default alias" (the common case; see
	// promptoverse_annotations.go). Set via --annotation-alias on `add`,
	// e.g. a deliberate one-off batch that wants the "genshin-impact"
	// framing of "Paimon" instead of the default "tyler-lore" one.
	AnnotationAlias string `json:"annotation_alias,omitempty"`
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

// prependQueue puts newItems at the FRONT of the queue -- ahead of
// everything already pending, not just ahead of items from the same add
// call -- so drainQueue's strict FIFO order processes them first. Same
// dedup backstop as appendQueue. Founder, real-time: "add fox 3 --tag
// 'outer space' fifos the tag subject combo to the top of the queue and
// starts on that (force or not force) but the other 2 gens get added end
// of queue same as always" -- used only for the --tag-forced item(s),
// never for normal selection, which still goes through appendQueue.
func prependQueue(path string, newItems []queueItem) error {
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
	return writeQueue(path, append(deduped, existing...))
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

// parseAddPositionalArgs interprets the positional (non-flag) args to
// `emily promptoverse add`. Three shapes:
//   - <count> alone (1 arg, parses as a number) -- auto-pick the subject.
//   - <subject> <count> (2 args) -- the original, explicit form.
//   - <subject> alone with --tag set (1 arg, does NOT parse as a number) --
//     added for the "skip the queue" case: count defaults to 1. Founder,
//     real-time: "add fox --tag 'outer space' --force SHOULD SKIP QUEUE
//     AND LIFO ADD TO THE QUEUE AND PROCESS THAT ONE FIRST ONLY IF A
//     NUMBER IS NOT SET."
//
// A single non-numeric arg with no --tag is still an error (matches the
// original behavior -- there's no subject-only shape without --tag).
func parseAddPositionalArgs(args []string, tag string) (subject string, count int, autoSubject bool, err error) {
	if len(args) != 1 && len(args) != 2 {
		return "", 0, false, fmt.Errorf("usage: emily promptoverse add [<subject>] <count> [--force] [--slow] [--tag <style>]...")
	}
	var countArg string
	count = 1
	switch {
	case len(args) == 2:
		subject = strings.TrimSpace(args[0])
		countArg = args[1]
	case tag != "" && func() bool { _, err := strconv.Atoi(args[0]); return err != nil }():
		subject = strings.TrimSpace(args[0])
		countArg = ""
	default:
		autoSubject = true
		countArg = args[0]
	}
	if countArg != "" {
		c, convErr := strconv.Atoi(countArg)
		if convErr != nil || c <= 0 {
			return "", 0, false, fmt.Errorf("emily promptoverse add: <count> must be a positive integer, got %q", countArg)
		}
		count = c
	}
	return subject, count, autoSubject, nil
}

func runPromptOVerseAdd(args []string) int {
	force := false
	slow := false
	var tags []string
	annotationText := ""
	annotationFromLore := false
	annotationAlias := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--force":
			force = true
		case a == "--slow":
			slow = true
		case a == "--tag":
			// Repeatable: "--tag X --tag Y" is a STYLE MASHUP request (two
			// existing style labels blended into one new hybrid style, one
			// generation), NOT two separate forced styles -- founder,
			// real-time: "add Medusa --tag kawaii --tag FFXI" / "it can be
			// created and then when we double tag it in our system we know
			// its a hybrid" / "i mean its a style mashup". A single --tag
			// behaves exactly as before.
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse add: --tag requires a value")
				return 1
			}
			tags = append(tags, args[i+1])
			i++
		case strings.HasPrefix(a, "--tag="):
			tags = append(tags, strings.TrimPrefix(a, "--tag="))
		case a == "--annotation":
			// Sets/overwrites this subject's default annotation, applied to
			// this batch AND every future generation of this subject --
			// annotations stick to the subject itself, not to one add call
			// (founder: "annotations stick to the subject itself").
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse add: --annotation requires a value")
				return 1
			}
			annotationText = args[i+1]
			i++
		case a == "--annotation-from-lore":
			// Same as --annotation, but the text is auto-derived from the
			// TYLER hero compendium + Goetia frequency table instead of
			// typed by hand.
			annotationFromLore = true
		case a == "--annotation-alias":
			// Selects which of the subject's stored annotation aliases this
			// batch's generations use, without changing the subject's
			// default -- e.g. a deliberate one-off "genshin-impact"-aliased
			// batch for a subject whose default alias is "tyler-lore".
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse add: --annotation-alias requires a value")
				return 1
			}
			annotationAlias = args[i+1]
			i++
		default:
			rest = append(rest, a)
		}
	}
	args = rest

	// More than one --tag: combine into a single new hybrid style label
	// (see ComponentStyles' doc comment in promptoverse_discover.go) rather
	// than forcing N separate styles. A single --tag is unchanged.
	tag := ""
	var hybridComponents []string
	switch len(tags) {
	case 0:
	case 1:
		tag = tags[0]
	default:
		hybridComponents = tags
		tag = strings.Join(tags, " × ")
	}

	subject, count, autoSubject, err := parseAddPositionalArgs(args, tag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	// No subject given, but a style IS forced: <count> means "how many
	// DIFFERENT subjects, all in this one locked style" -- not "how many
	// styles for one auto-picked subject" (the normal autoSubject
	// meaning). Founder, real-time: "i need a way to force generations of
	// a style like game sprite or pixel art if i dont specify a subject
	// but do specify count and do set a tag all of the styles should lock
	// to that style tag of the count specified." A style sweep like
	// "generate 10 game sprites" is a different shape of request from
	// "generate 10 styles of this one subject" and needs its own path.
	if autoSubject && tag != "" {
		return runPromptOVerseAddStyleSweep(cfg, tags, count, force, slow)
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	existing, err := client.ListPromptOVerseNodes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list existing nodes (needed to dedupe): %v\n", err)
		return 1
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pityPath := pityStatePath(cfg)
	pity, err := loadPityState(pityPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load pity state: %v\n", err)
		return 1
	}

	if autoSubject {
		picked, perr := pickSubject(cfg, existing, rng, &pity, nil)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "auto-pick subject: %v\n", perr)
			return 1
		}
		subject = picked
		fmt.Printf("auto-picked subject: %q\n", subject)
	}

	if annotationFromLore {
		derived, ok, derr := deriveLoreAnnotation(cfg.TylerRoot, subject)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "annotation lore lookup: %v\n", derr)
			return 1
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "--annotation-from-lore: no TYLER hero compendium entry matches %q\n", subject)
			return 1
		}
		annotationText = derived
	}
	if annotationText != "" {
		alias := annotationAlias
		if alias == "" {
			if annotationFromLore {
				alias = "tyler-lore"
			} else {
				alias = "manual"
			}
		}
		source := "manual"
		if annotationFromLore {
			source = "tyler-lore"
		}
		if err := setSubjectAnnotationAlias(cfg, subject, alias, annotationText, source, annotationAlias == ""); err != nil {
			fmt.Fprintf(os.Stderr, "save annotation: %v\n", err)
			return 1
		}
		fmt.Printf("annotation %q set for %q (used for this batch and every future generation of this subject)\n", alias, subject)
		if annotationAlias == "" {
			annotationAlias = alias
		}
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
				newStyle.ComponentStyles = hybridComponents
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
				if len(hybridComponents) > 0 {
					fmt.Printf("created style hybrid %q (of %s) and forced it into the registry\n", tag, strings.Join(hybridComponents, ", "))
				} else {
					fmt.Printf("forced a new style %q into the registry\n", tag)
				}
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
	// A --tag-forced item jumps to the FRONT of the whole queue (ahead of
	// everything already pending, not just the rest of this batch) and gets
	// drained first -- founder: "fifos the tag subject combo to the top of
	// the queue and starts on that (force or not force) but the other 2
	// gens get added end of queue same as always." Everything else keeps
	// the normal FIFO-behind-existing-items behavior.
	var frontItems, backItems []queueItem
	for _, st := range selected {
		item := queueItem{
			Subject: subject, StyleLabel: st.Label, EnqueuedAt: now,
			Forced:          forcedLabel != "" && st.Label == forcedLabel,
			AnnotationAlias: annotationAlias,
		}
		if item.Forced {
			frontItems = append(frontItems, item)
		} else {
			backItems = append(backItems, item)
		}
	}
	if len(frontItems) > 0 {
		if err := prependQueue(path, frontItems); err != nil {
			fmt.Fprintf(os.Stderr, "enqueue (front): %v\n", err)
			return 1
		}
		fmt.Printf("queued %q for %q at the FRONT of the queue (--tag forced, processed first)\n", forcedLabel, subject)
	}
	if len(backItems) > 0 {
		if err := appendQueue(path, backItems); err != nil {
			fmt.Fprintf(os.Stderr, "enqueue: %v\n", err)
			return 1
		}
		fmt.Printf("queued %d requests for %q (FIFO — behind anything already pending)\n", len(backItems), subject)
	}

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

	// hybridComponentsByLabel resolves a style label to the style labels it
	// was blended from (see ComponentStyles' doc comment), so drainQueue
	// can stamp a "style_hybrid_of" tag on the published node -- reusing
	// the node's existing generic Tags map/table rather than adding a new
	// column/endpoint, per the founder's own steer ("you can fake it in
	// the data and present the mashups [hybrids] in the site if thats
	// better").
	hybridComponentsByLabel := make(map[string][]string, len(discovered))
	for _, ds := range discovered {
		if len(ds.ComponentStyles) > 0 {
			hybridComponentsByLabel[ds.Label] = ds.ComponentStyles
		}
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
		// Subject-level annotation, if any, is appended to the real
		// generation prompt only -- the EZ prompt built below from
		// st.Label+it.Subject never sees it, so the taxonomy/gallery-facing
		// subject stays exactly "Paimon" (see promptoverse_annotations.go).
		prompt = annotatePrompt(cfg, it.Subject, it.AnnotationAlias, prompt)
		img, err := vertexGenerateImage(token, prompt)
		if err != nil {
			if errors.Is(err, errVertexContentBlocked) {
				// A permanent policy rejection, not a transient failure --
				// retrying gets the identical result forever, which would
				// otherwise jam every other queued request behind it
				// (founder-visible, 2026-08-17: "anime x Rapunzel" hit
				// this and looked exactly like an API-key problem). Never
				// retried -- recorded to the dead-letter dataset instead,
				// for later analysis of which (subject, style) pairs
				// correlate with IP-sensitive blocks (founder: "rapunzel
				// is not disney but certain depictions of her are so
				// icecream or candy rapunzle may proportionately cause
				// more content sensitive api responses").
				fmt.Fprintf(os.Stderr, "  SKIPPED (content policy): %s x %s -- %v\n", st.Label, it.Subject, err)
				var cbErr *contentBlockedError
				if errors.As(err, &cbErr) {
					dlPath := deadLetterPath(cfg)
					entry := deadLetterEntry{
						Subject: it.Subject, StyleLabel: st.Label,
						Reason: cbErr.Reason, Message: cbErr.Message, BlockedAt: time.Now().UTC(),
					}
					if err := appendDeadLetter(dlPath, entry); err != nil {
						fmt.Fprintf(os.Stderr, "  WARNING: failed to record dead-letter entry to %s: %v\n", dlPath, err)
					} else {
						fmt.Fprintf(os.Stderr, "  recorded to dead-letter dataset: %s\n", dlPath)
					}
				} else {
					fmt.Fprintf(os.Stderr, "  WARNING: content-blocked error was not the expected *contentBlockedError type -- not recorded to dead-letter dataset\n")
				}
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

		if st.Label == promptoverseSpriteStyleLabel {
			stripped, serr := stripChromaKeyBackground(img)
			if serr != nil {
				// Don't fail the whole drain over a post-processing step --
				// publish the flat-green original rather than lose a real,
				// already-paid-for generation; the SOLID-GREEN negative
				// constraints in the prompt itself keep this a rare path.
				fmt.Fprintf(os.Stderr, "  WARNING: chroma-key background removal failed, publishing with green background intact: %v\n", serr)
			} else {
				img = stripped
			}
		}

		slug := fmt.Sprintf("%s-%s", slugifyPO(it.Subject), slugifyPO(st.Label))
		tags := map[string]string{"style": st.Label, "subject": it.Subject}
		if components, isHybrid := hybridComponentsByLabel[st.Label]; isHybrid {
			// Distinct vocabulary from mashup_nominations (subject+subject):
			// this is a style hybrid (style+style) -- founder: "hybrid is a
			// dual subject mashup is a dual style" -> confirmed the other
			// way: mashup=subjects (existing feature), hybrid=styles (this
			// one). Rendered on the site for free via the existing generic
			// Tags table (render.go), no schema/endpoint change needed.
			tags["style_hybrid_of"] = strings.Join(components, ", ")
		}
		node := iduna.PromptOVerseNode{
			Slug:           slug,
			Label:          st.Label,
			Subject:        it.Subject,
			Kind:           st.Kind,
			EZPrompt:       fmt.Sprintf("%s %s", st.Label, it.Subject),
			ExpandedPrompt: prompt,
			ImageBase64:    base64.StdEncoding.EncodeToString(img),
			Tags:           tags,
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

// stripChromaKeyBackground turns the flat-green background a "game
// sprite"-style generation asks for into real alpha transparency, via
// ImageMagick's `convert` -- the same real, already-proven dependency
// the thumbnail pipeline (cmd/promptoverse-thumbnails) uses, piped
// stdin-to-stdout so no temp files are needed. -fuzz tolerates the
// slight color variance real generations have around a nominally-solid
// green fill; too low and edge pixels stay opaque green fringing, too
// high and it eats into the character's own green-adjacent colors, so
// 12% is a real tested middle, not a guess.
func stripChromaKeyBackground(pngBytes []byte) ([]byte, error) {
	cmd := exec.Command("convert", "-", "-fuzz", "12%", "-transparent", promptoverseSpriteChromaKeyHex, "-")
	cmd.Stdin = bytes.NewReader(pngBytes)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("chroma-key background removal: %w: %s", err, stderr.String())
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("chroma-key background removal produced no output")
	}
	return out.Bytes(), nil
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
				return nil, &contentBlockedError{Reason: c.FinishReason, Message: c.FinishMessage}
			}
			return nil, fmt.Errorf("no image data in response (%s)", msg)
		}
	}
	return nil, fmt.Errorf("no image data in response")
}

// contentBlockedError marks a PERMANENT rejection -- Vertex's own
// safety/IP-content policy declined this exact prompt, not a transient
// failure retrying would fix. drainQueue skips-and-continues on this
// specific error instead of stopping the whole run, same shape as
// iduna.ErrPromptOVerseNodeExists, and records it to the dead-letter
// dataset (promptoverse_deadletter.go) rather than just logging it.
// Discovered live (2026-08-17): "anime x Rapunzel" hit
// IMAGE_PROHIBITED_CONTENT (a real Disney-IP filter) and, before this fix,
// looked exactly like an auth failure while also permanently jamming
// everything queued behind it. Structured (Reason/Message fields) rather
// than a formatted string so the dead-letter record doesn't need to
// re-parse error text.
type contentBlockedError struct {
	Reason  string
	Message string
}

func (e *contentBlockedError) Error() string {
	msg := e.Reason
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return "vertex ai: content policy blocked this prompt: " + msg
}

// Is lets errors.Is(err, errVertexContentBlocked) keep working as the
// simple "is this the permanent-block class of error" check, without
// every caller needing errors.As + the concrete type.
func (e *contentBlockedError) Is(target error) bool { return target == errVertexContentBlocked }

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
		// Any other rune (punctuation like "×", "&", accents, ...) is
		// dropped silently. That's fine when it sits directly between two
		// letters ("Master Chief (Halo)" -> "master-chief-halo"), but a
		// dropped rune surrounded by spaces on both sides -- e.g. the "×"
		// in a hybrid style label like "kawaii × FFXI" -- otherwise leaves
		// a double hyphen behind (IDUNA's slug endpoint 400s on
		// "medusa-kawaii--ffxi": found live 2026-08-18 generating that
		// exact hybrid). Collapsed and trimmed below rather than only
		// special-cased for "×", so any future punctuation-bearing
		// subject/style is safe the same way.
	}
	slug := b.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	return strings.Trim(slug, "-")
}

func trimMsgPO(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
