package cmd

import (
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugifyPO(t *testing.T) {
	cases := map[string]string{
		"ducks":                "ducks",
		"Master Chief (Halo)":  "master-chief-halo",
		"stained glass":        "stained-glass",
		"1910s Tobacco Card":   "1910s-tobacco-card",
		"pop art/silkscreen!!": "pop-artsilkscreen",
		// Regression: a dropped punctuation rune with spaces on both sides
		// (the "×" joining a hybrid style label) previously left a bare
		// double hyphen behind, which IDUNA's slug endpoint 400s on --
		// found live 2026-08-18 generating "kawaii × FFXI" x Medusa.
		"kawaii × FFXI": "kawaii-ffxi",
		"-leading-":     "leading",
		"trailing---":   "trailing",
		"a  ×  ×  b":    "a-b",
	}
	for in, want := range cases {
		if got := slugifyPO(in); got != want {
			t.Errorf("slugifyPO(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPromptoverseHatStyle_IsStandaloneItemNotACharacter -- kanban HSG-000 ("promptoverse hat
// gen... a nice pixel art hat"). Real, load-bearing distinction from the existing "game sprite"
// style: this must render the hat/headwear OBJECT alone, not a character wearing it, since
// BRAWLPIT's own hat catalog (WOTAN_HAT_STORE_NORTHSTAR.md Phase 1) needs a standalone item
// icon, not a full-body portrait.
func TestPromptoverseHatStyle_IsStandaloneItemNotACharacter(t *testing.T) {
	var hatStyle *style
	for i := range promptoverseStyles {
		if promptoverseStyles[i].Label == promptoverseHatStyleLabel {
			hatStyle = &promptoverseStyles[i]
			break
		}
	}
	if hatStyle == nil {
		t.Fatal("expected a registered style with label promptoverseHatStyleLabel")
	}
	prompt := hatStyle.Prompt("pirate")
	if !strings.Contains(prompt, "pirate") {
		t.Errorf("expected the subject to appear in the prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "no head or character wearing it") {
		t.Errorf("expected an explicit standalone-item instruction, not a worn-hat portrait: %q", prompt)
	}
	if !strings.Contains(prompt, promptoverseSpriteChromaKeyHex) {
		t.Errorf("expected the same real chroma-key convention the sprite style already uses: %q", prompt)
	}
}

func TestPromptoverseStyles_AllHaveNonEmptyTemplates(t *testing.T) {
	seen := map[string]bool{}
	for _, st := range promptoverseStyles {
		if st.Label == "" {
			t.Error("found a style with an empty Label")
		}
		if seen[st.Label] {
			t.Errorf("duplicate style Label: %q", st.Label)
		}
		seen[st.Label] = true

		if st.Kind != "historical" && st.Kind != "surreal" {
			t.Errorf("style %q has invalid Kind %q", st.Label, st.Kind)
		}

		prompt := st.Prompt("a duck")
		if !strings.Contains(prompt, "a duck") {
			t.Errorf("style %q's prompt template did not include the subject: %q", st.Label, prompt)
		}
		if len(prompt) < 20 {
			t.Errorf("style %q produced a suspiciously short prompt: %q", st.Label, prompt)
		}
	}
}

func TestParseAddPositionalArgs_ExplicitSubjectAndCount(t *testing.T) {
	subject, count, auto, err := parseAddPositionalArgs([]string{"ducks", "4"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "ducks" || count != 4 || auto {
		t.Errorf("got subject=%q count=%d auto=%v", subject, count, auto)
	}
}

func TestParseAddPositionalArgs_BareCountIsAutoSubject(t *testing.T) {
	subject, count, auto, err := parseAddPositionalArgs([]string{"4"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "" || count != 4 || !auto {
		t.Errorf("got subject=%q count=%d auto=%v", subject, count, auto)
	}
}

func TestParseAddPositionalArgs_SubjectAloneWithTagDefaultsCountToOne(t *testing.T) {
	// Founder, real-time: "add fox --tag 'outer space' --force SHOULD SKIP
	// QUEUE AND LIFO ADD TO THE QUEUE AND PROCESS THAT ONE FIRST ONLY IF A
	// NUMBER IS NOT SET."
	subject, count, auto, err := parseAddPositionalArgs([]string{"fox"}, "outer space")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "fox" || count != 1 || auto {
		t.Errorf("got subject=%q count=%d auto=%v, want subject=fox count=1 auto=false", subject, count, auto)
	}
}

func TestParseAddPositionalArgs_SubjectAloneWithoutTagIsStillAnError(t *testing.T) {
	// The new subject-only shape only applies when --tag is set -- a bare
	// non-numeric single arg with no tag has no meaning to fall back to.
	_, _, _, err := parseAddPositionalArgs([]string{"fox"}, "")
	if err == nil {
		t.Fatal("expected an error for a non-numeric single arg with no --tag")
	}
}

func TestParseAddPositionalArgs_NumericSingleArgWithTagIsStillAutoSubjectCount(t *testing.T) {
	// A numeric single arg with --tag set parses the same shape it always
	// has (subject="", count=4, autoSubject=true) -- runPromptOVerseAdd is
	// what changed what THIS shape means (style sweep: 4 different
	// subjects locked to "gladiator", not 4 styles for one auto-picked
	// subject), not the parser.
	subject, count, auto, err := parseAddPositionalArgs([]string{"4"}, "gladiator")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subject != "" || count != 4 || !auto {
		t.Errorf("got subject=%q count=%d auto=%v, want subject=\"\" count=4 auto=true", subject, count, auto)
	}
}

func TestParseAddPositionalArgs_RejectsBadCountAndBadArgCounts(t *testing.T) {
	for _, args := range [][]string{
		{"ducks", "not-a-number"},
		{"ducks", "0"},
		{},
		{"a", "b", "c"},
	} {
		if _, _, _, err := parseAddPositionalArgs(args, ""); err == nil {
			t.Errorf("expected an error for args %v", args)
		}
	}
}

func TestRunPromptOVerseAdd_RejectsBadCount(t *testing.T) {
	if code := runPromptOVerseAdd([]string{"ducks", "not-a-number"}); code != 1 {
		t.Errorf("expected exit code 1 for a non-numeric count, got %d", code)
	}
	if code := runPromptOVerseAdd([]string{"ducks", "0"}); code != 1 {
		t.Errorf("expected exit code 1 for a zero count, got %d", code)
	}
	if code := runPromptOVerseAdd([]string{"ducks"}); code != 1 {
		t.Errorf("expected exit code 1 for missing count arg, got %d", code)
	}
}

func TestRunPromptOVerseAdd_ParsesTagFlagAndStripsFromPositionalArgs(t *testing.T) {
	// --tag <value> (and --tag=value) must be consumed as a flag, not
	// treated as part of the positional <subject>/<count> pair -- verified
	// via the same count-validation early-return the test above uses, so
	// this doesn't require a live IDUNA/network connection.
	if code := runPromptOVerseAdd([]string{"princess", "--tag", "gladiator", "not-a-number"}); code != 1 {
		t.Errorf("expected exit 1 (bad count) once --tag/value were correctly stripped, got %d", code)
	}
	if code := runPromptOVerseAdd([]string{"princess", "--tag=gladiator", "not-a-number"}); code != 1 {
		t.Errorf("expected exit 1 (bad count) for the --tag=value form too, got %d", code)
	}
	if code := runPromptOVerseAdd([]string{"princess", "--tag"}); code != 1 {
		t.Errorf("expected exit 1 for --tag given with no value, got %d", code)
	}
}

func TestRunPromptOVerseAdd_ParsesSlowFlag(t *testing.T) {
	if code := runPromptOVerseAdd([]string{"princess", "--slow", "not-a-number"}); code != 1 {
		t.Errorf("expected exit 1 (bad count) once --slow was correctly stripped, got %d", code)
	}
}

func TestRunPromptOVerseAdd_SinglePositionalArgIsAutoSubjectCount(t *testing.T) {
	// A single positional arg means "auto-pick the subject" -- verified via
	// the same count-validation early-return the other tests use, so this
	// doesn't require a live IDUNA/network connection. Also confirms it's
	// still treated as the count (not the subject) by using an invalid
	// count value.
	if code := runPromptOVerseAdd([]string{"not-a-number"}); code != 1 {
		t.Errorf("expected exit 1 (bad count) for a single bad positional arg, got %d", code)
	}
	if code := runPromptOVerseAdd([]string{}); code != 1 {
		t.Errorf("expected exit 1 (usage) for zero positional args, got %d", code)
	}
	if code := runPromptOVerseAdd([]string{"a", "b", "c"}); code != 1 {
		t.Errorf("expected exit 1 (usage) for 3 positional args, got %d", code)
	}
}

func TestStyleByLabel(t *testing.T) {
	st, ok := styleByLabel("stained glass")
	if !ok {
		t.Fatal("expected to find 'stained glass' in the registry")
	}
	if st.Label != "stained glass" {
		t.Errorf("got wrong style: %+v", st)
	}
	if _, ok := styleByLabel("not a real style"); ok {
		t.Error("expected styleByLabel to report not-found for an unknown label")
	}
}

func TestQueue_RoundTripsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")

	first := []queueItem{{Subject: "ducks", StyleLabel: "stained glass", EnqueuedAt: time.Now().UTC()}}
	if err := appendQueue(path, first); err != nil {
		t.Fatalf("appendQueue (first): %v", err)
	}
	// A later 'add' call must land BEHIND the first request, not ahead of
	// it -- this is the whole point of the founder's FIFO direction.
	second := []queueItem{{Subject: "a red panda", StyleLabel: "claymation", EnqueuedAt: time.Now().UTC()}}
	if err := appendQueue(path, second); err != nil {
		t.Fatalf("appendQueue (second): %v", err)
	}

	items, err := loadQueue(path)
	if err != nil {
		t.Fatalf("loadQueue: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 queued items, got %d", len(items))
	}
	if items[0].Subject != "ducks" || items[1].Subject != "a red panda" {
		t.Errorf("expected FIFO order [ducks, a red panda], got [%s, %s]", items[0].Subject, items[1].Subject)
	}
}

func TestQueue_WriteQueueThenLoad_EmptyMeansNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	items, err := loadQueue(path)
	if err != nil {
		t.Fatalf("loadQueue on a nonexistent file should not error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected no items, got %d", len(items))
	}
}

func TestSelectStylesForSubject_SkipsExcluded(t *testing.T) {
	exclude := map[string]bool{"1910s tobacco card": true, "claymation": true}
	rng := rand.New(rand.NewSource(1))
	selected := selectStylesForSubject(promptoverseStyles, len(promptoverseStyles), exclude, map[string]int{}, rng)
	for _, st := range selected {
		if exclude[st.Label] {
			t.Errorf("expected %q to be excluded (already published/queued for this subject), but it was selected", st.Label)
		}
	}
	if len(selected) != len(promptoverseStyles)-2 {
		t.Errorf("expected %d selectable styles, got %d", len(promptoverseStyles)-2, len(selected))
	}
}

func TestSelectStylesForSubject_FavorsLeastUsedButNotAlways(t *testing.T) {
	// Regression for BOTH founder complaints in tension with each other:
	// "it is favoring the tobacco card and the claymation every time" (the
	// least-used style should usually win) and "it shouldnt always fill in
	// the lowest tags... because it already has a lot of tobacco cards"
	// (but not with absolute certainty -- a heavily-used style must still
	// get an occasional look-in, "watercolor is fire but we havent
	// generated one in quite a few gens now" being the same shape of
	// complaint from the other direction). Weighted random sampling
	// without replacement (the "marble bag") is exactly the smoothed-but-
	// not-uniform middle the founder asked for.
	globalUsage := map[string]int{
		"1910s tobacco card": 20,
		"claymation":         15,
		"underwater":         0,
	}
	exclude := map[string]bool{}
	for _, st := range promptoverseStyles {
		if st.Label != "1910s tobacco card" && st.Label != "claymation" && st.Label != "underwater" {
			exclude[st.Label] = true
		}
	}

	const trials = 300
	underwaterWins := 0
	othersWinAtLeastOnce := false
	for i := 0; i < trials; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		selected := selectStylesForSubject(promptoverseStyles, 1, exclude, globalUsage, rng)
		if len(selected) != 1 {
			t.Fatalf("expected exactly 1 style selected, got %d", len(selected))
		}
		if selected[0].Label == "underwater" {
			underwaterWins++
		} else {
			othersWinAtLeastOnce = true
		}
	}
	if underwaterWins < trials*6/10 {
		t.Errorf("expected the least-used style to win a strong majority of %d trials, won %d", trials, underwaterWins)
	}
	if !othersWinAtLeastOnce {
		t.Error("expected a more-used style to occasionally win too, got the least-used style every single trial")
	}
}

func TestSelectStylesForSubject_ZeroAvailableReturnsEmpty(t *testing.T) {
	exclude := map[string]bool{}
	for _, st := range promptoverseStyles {
		exclude[st.Label] = true
	}
	rng := rand.New(rand.NewSource(1))
	selected := selectStylesForSubject(promptoverseStyles, 5, exclude, map[string]int{}, rng)
	if len(selected) != 0 {
		t.Errorf("expected no styles left to select when every style is excluded, got %+v", selected)
	}
}

func TestSelectStylesForSubject_IncludesNewVarietyStyles(t *testing.T) {
	for _, want := range []string{"outer space", "underwater", "robot", "made of candy"} {
		if _, ok := styleByLabel(want); !ok {
			t.Errorf("expected %q to be a registered style (promoted from the original baseball-card batch for variety)", want)
		}
	}
}

func TestPromptoverseStyles_IncludesFounderNamedAdditions(t *testing.T) {
	for _, want := range []string{"whiteboard", "paper-craft", "anime", "kawaii"} {
		if _, ok := styleByLabel(want); !ok {
			t.Errorf("expected %q to be a registered style (founder: \"add these as top level hard coded styles\")", want)
		}
	}
}

func TestQueue_WriteQueueOverwritesCompletely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	if err := writeQueue(path, []queueItem{
		{Subject: "a", StyleLabel: "claymation"},
		{Subject: "b", StyleLabel: "LEGO minifigure"},
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate draining the front item: rewrite with only the remainder.
	if err := writeQueue(path, []queueItem{{Subject: "b", StyleLabel: "LEGO minifigure"}}); err != nil {
		t.Fatal(err)
	}
	items, err := loadQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Subject != "b" {
		t.Errorf("expected only the remaining item 'b', got %+v", items)
	}
}

func TestAppendQueue_SkipsExactDuplicates(t *testing.T) {
	// Regression for the founder-reported jam: "the queue needs to be
	// cleared out we have some duplicates queued that are not getting
	// deduped so new inputs always fail." A second append for the exact
	// same (subject, style) pair must not add a second copy.
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	first := []queueItem{{Subject: "ghost playing the piano", StyleLabel: "claymation"}}
	if err := appendQueue(path, first); err != nil {
		t.Fatal(err)
	}
	if err := appendQueue(path, first); err != nil {
		t.Fatal(err)
	}
	items, err := loadQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("expected the duplicate append to be skipped, got %d items: %+v", len(items), items)
	}
}

func TestAppendQueue_DifferentStyleSameSubjectBothKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	if err := appendQueue(path, []queueItem{
		{Subject: "ducks", StyleLabel: "claymation"},
		{Subject: "ducks", StyleLabel: "LEGO minifigure"},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := loadQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("expected both distinct styles for the same subject to be kept, got %d: %+v", len(items), items)
	}
}

func TestPrependQueue_PutsNewItemsAheadOfExistingOnes(t *testing.T) {
	// Founder, real-time: "add fox 3 --tag 'outer space' fifos the tag
	// subject combo to the top of the queue and starts on that (force or
	// not force) but the other 2 gens get added end of queue same as
	// always." prependQueue is the mechanism for the first half of that --
	// it must jump ahead of items ALREADY pending, not just items from the
	// same batch.
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	if err := appendQueue(path, []queueItem{
		{Subject: "already pending 1", StyleLabel: "claymation"},
		{Subject: "already pending 2", StyleLabel: "watercolor"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := prependQueue(path, []queueItem{
		{Subject: "fox", StyleLabel: "outer space", Forced: true},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := loadQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items total, got %d: %+v", len(items), items)
	}
	if items[0].Subject != "fox" || items[0].StyleLabel != "outer space" {
		t.Errorf("expected the prepended item first, got %+v", items[0])
	}
	if items[1].Subject != "already pending 1" || items[2].Subject != "already pending 2" {
		t.Errorf("expected the original two items to keep their relative order behind the new one: %+v", items[1:])
	}
}

func TestPrependQueue_SkipsExactDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	item := []queueItem{{Subject: "fox", StyleLabel: "outer space"}}
	if err := appendQueue(path, item); err != nil {
		t.Fatal(err)
	}
	if err := prependQueue(path, item); err != nil {
		t.Fatal(err)
	}
	items, err := loadQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Errorf("expected the duplicate to be skipped, got %d items: %+v", len(items), items)
	}
}

func TestPromptoverseInterRequestDelay_DefaultAndOverride(t *testing.T) {
	t.Setenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS", "")
	if got := promptoverseInterRequestDelay(); got != promptoverseDefaultInterRequestDelay {
		t.Errorf("expected the default %v with no override, got %v", promptoverseDefaultInterRequestDelay, got)
	}

	t.Setenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS", "45")
	if got := promptoverseInterRequestDelay(); got != 45*time.Second {
		t.Errorf("expected the override to win, got %v", got)
	}

	// Malformed/non-positive overrides fall back to the default rather than
	// silently producing a zero or negative sleep.
	for _, bad := range []string{"not-a-number", "0", "-5"} {
		t.Setenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS", bad)
		if got := promptoverseInterRequestDelay(); got != promptoverseDefaultInterRequestDelay {
			t.Errorf("expected a malformed override %q to fall back to the default, got %v", bad, got)
		}
	}
}

func TestPromptoverseEffectiveDelay_GrowsWithSuccessesThisRun(t *testing.T) {
	t.Setenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS", "")
	// Regression for the founder's exact report: "we are still getting
	// apilimited in like our 3rd or 4th gen usually" -- the gap before the
	// 4th request must be meaningfully larger than the gap before the 2nd.
	d0 := promptoverseEffectiveDelay(0, 0, false)
	d1 := promptoverseEffectiveDelay(1, 0, false)
	d3 := promptoverseEffectiveDelay(3, 0, false)
	if !(d0 < d1 && d1 < d3) {
		t.Errorf("expected strictly increasing delay across a run, got d0=%v d1=%v d3=%v", d0, d1, d3)
	}
	if d0 != promptoverseDefaultInterRequestDelay {
		t.Errorf("expected the first gap (0 prior successes) to equal the base delay, got %v", d0)
	}
}

func TestPromptoverseEffectiveDelay_GrowthCaps(t *testing.T) {
	t.Setenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS", "")
	got := promptoverseEffectiveDelay(1000, 0, false)
	want := promptoverseDefaultInterRequestDelay + promptoverseInterRequestGrowthCap
	if got != want {
		t.Errorf("expected growth to cap at %v total, got %v", want, got)
	}
}

func TestPromptoverseEffectiveDelay_AddsBackoffExtraOnTop(t *testing.T) {
	t.Setenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS", "")
	got := promptoverseEffectiveDelay(0, 45*time.Second, false)
	want := promptoverseDefaultInterRequestDelay + 45*time.Second
	if got != want {
		t.Errorf("expected the backoff extra to add on top of the base delay, got %v want %v", got, want)
	}
}

func TestPromptoverseEffectiveDelay_SlowDoublesEverything(t *testing.T) {
	t.Setenv("PROMPTOVERSE_INTER_REQUEST_DELAY_SECONDS", "")
	normal := promptoverseEffectiveDelay(2, 20*time.Second, false)
	slow := promptoverseEffectiveDelay(2, 20*time.Second, true)
	if slow != normal*2 {
		t.Errorf("expected --slow to exactly double the effective delay, got normal=%v slow=%v", normal, slow)
	}
}
