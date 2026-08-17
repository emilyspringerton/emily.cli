package cmd

import (
	"strings"
	"testing"
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
