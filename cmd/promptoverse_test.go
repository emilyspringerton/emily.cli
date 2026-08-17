package cmd

import (
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
	}
	for in, want := range cases {
		if got := slugifyPO(in); got != want {
			t.Errorf("slugifyPO(%q) = %q, want %q", in, got, want)
		}
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
	selected := selectStylesForSubject(promptoverseStyles, len(promptoverseStyles), exclude, map[string]int{})
	for _, st := range selected {
		if exclude[st.Label] {
			t.Errorf("expected %q to be excluded (already published/queued for this subject), but it was selected", st.Label)
		}
	}
	if len(selected) != len(promptoverseStyles)-2 {
		t.Errorf("expected %d selectable styles, got %d", len(promptoverseStyles)-2, len(selected))
	}
}

func TestSelectStylesForSubject_PrefersLeastGloballyUsed(t *testing.T) {
	// Regression for the exact founder complaint: "it is favoring the
	// tobacco card and the claymation every time" -- if those two are the
	// most globally-used styles, a request for 1 new style must NOT pick
	// either of them while a less-used style is available.
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
	selected := selectStylesForSubject(promptoverseStyles, 1, exclude, globalUsage)
	if len(selected) != 1 || selected[0].Label != "underwater" {
		t.Errorf("expected the single least-used style 'underwater' to be picked first, got %+v", selected)
	}
}

func TestSelectStylesForSubject_ZeroAvailableReturnsEmpty(t *testing.T) {
	exclude := map[string]bool{}
	for _, st := range promptoverseStyles {
		exclude[st.Label] = true
	}
	selected := selectStylesForSubject(promptoverseStyles, 5, exclude, map[string]int{})
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
