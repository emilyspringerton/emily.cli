package cmd

import "testing"

func TestParseStyleTags_RealCapturedListCompletion(t *testing.T) {
	// Captured live 2026-08-17 against gpt2-base with the founder's exact
	// seed list ("pop art silkscreen, woodcut block print, underwater,
	// outer space, robot, "), temperature 0.9.
	raw := `xtrotor, laser, laser 3D, geothermal, infrared, photochemical, photochemical imaging, solar cell, sunspot, solar wind, ultraviolet, solar power production, solar thermal control and heating system, solar photovoltaic, sunwires, solar photovoltaic (`
	got := parseStyleTags(raw)
	wantSome := []string{"xtrotor", "laser", "laser 3D", "geothermal", "infrared", "solar cell", "sunspot"}
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range wantSome {
		if !set[w] {
			t.Errorf("expected %q to be parsed out, got %v", w, got)
		}
	}
	// The dangling "solar photovoltaic (" fragment should have its stray
	// paren stripped rather than being dropped or left dirty.
	if set["solar photovoltaic ("] {
		t.Error("expected the trailing '(' to be stripped from the last fragment")
	}
}

func TestParseStyleTags_DriftIntoProseYieldsNothingUsable(t *testing.T) {
	// Captured live 2026-08-17 against gpt2-base at temperature 0.7 -- the
	// model abandoned list format after 2 tokens and wrote a sentence.
	raw := " and more.\n\nThe Art of the Sea is a weekly series of short, animated shorts produced by Disney's studio in Hollywood. These short shorts are designed to show the challenges of building an underwater cruise ship. The series is based on the popular"
	got := parseStyleTags(raw)
	for _, g := range got {
		t.Errorf("expected no usable tags out of prose drift, got %q among %v", g, got)
	}
}

func TestParseStyleTags_DedupesCaseInsensitively(t *testing.T) {
	got := parseStyleTags("Origami, origami, ORIGAMI, mosaic")
	if len(got) != 2 {
		t.Errorf("expected 2 deduped tags, got %v", got)
	}
}

func TestParseStyleTags_StripsQuotesAndBullets(t *testing.T) {
	got := parseStyleTags(`"origami", • mosaic, -stencil art`)
	want := []string{"origami", "mosaic", "stencil art"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("index %d: expected %q, got %q", i, w, got[i])
		}
	}
}

func TestParseStyleTags_DropsLetterlessFragments(t *testing.T) {
	// Captured live 2026-08-17: the base model sometimes opens with a run
	// of underscores (formatting artifact, not a real tag). The tool
	// doesn't try to be a perfect prose-vs-tag classifier -- this is a
	// review-only aid -- but a letterless fragment is an easy, unambiguous
	// case to drop outright.
	got := parseStyleTags("___________ ____________, or other forms of art")
	for _, g := range got {
		if !containsLetter(g) {
			t.Errorf("expected the underscore-only fragment to be dropped, got %q among %v", g, got)
		}
	}
}

func TestParseStyleTags_EmptyInputYieldsEmptyOutput(t *testing.T) {
	got := parseStyleTags("")
	if len(got) != 0 {
		t.Errorf("expected no tags from empty input, got %v", got)
	}
}
