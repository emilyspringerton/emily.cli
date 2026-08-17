package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStyleProposalJSON_Decline(t *testing.T) {
	ds, err := parseStyleProposalJSON(`{"propose": false}`, []string{"claymation"}, "a duck")
	if err != nil {
		t.Fatalf("a decline should not be an error: %v", err)
	}
	if ds != nil {
		t.Errorf("expected nil on decline, got %+v", ds)
	}
}

func TestParseStyleProposalJSON_ValidProposal(t *testing.T) {
	raw := `{"propose": true, "label": "cross-stitch embroidery", "kind": "historical", "template": "%s rendered as a cross-stitch embroidery pattern."}`
	ds, err := parseStyleProposalJSON(raw, []string{"claymation", "stained glass"}, "a duck")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds == nil {
		t.Fatal("expected a non-nil proposal")
	}
	if ds.Label != "cross-stitch embroidery" || ds.Kind != "historical" {
		t.Errorf("unexpected fields: %+v", ds)
	}
	if ds.DiscoveredFor != "a duck" {
		t.Errorf("expected DiscoveredFor to record the triggering subject, got %q", ds.DiscoveredFor)
	}
}

func TestParseStyleProposalJSON_StripsMarkdownFences(t *testing.T) {
	raw := "```json\n" + `{"propose": true, "label": "origami", "kind": "surreal", "template": "%s folded from origami paper."}` + "\n```"
	ds, err := parseStyleProposalJSON(raw, []string{}, "a duck")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds == nil || ds.Label != "origami" {
		t.Errorf("expected fenced JSON to parse, got %+v, err=%v", ds, err)
	}
}

func TestParseStyleProposalJSON_RejectsDuplicateLabel(t *testing.T) {
	raw := `{"propose": true, "label": "Claymation", "kind": "surreal", "template": "%s in claymation."}`
	_, err := parseStyleProposalJSON(raw, []string{"claymation"}, "a duck")
	if err == nil {
		t.Fatal("expected an error for a proposal that duplicates an existing style (case-insensitive)")
	}
}

func TestParseStyleProposalJSON_RejectsMissingPlaceholder(t *testing.T) {
	raw := `{"propose": true, "label": "origami", "kind": "surreal", "template": "a duck folded from origami paper."}`
	_, err := parseStyleProposalJSON(raw, []string{}, "a duck")
	if err == nil {
		t.Fatal("expected an error for a template missing the placeholder")
	}
}

func TestParseStyleProposalJSON_RejectsBadKind(t *testing.T) {
	raw := `{"propose": true, "label": "origami", "kind": "whimsical", "template": "%s folded from origami paper."}`
	_, err := parseStyleProposalJSON(raw, []string{}, "a duck")
	if err == nil {
		t.Fatal("expected an error for an invalid kind (must be historical or surreal)")
	}
}

func TestParseNamedStyleTemplateJSON_ValidResponse(t *testing.T) {
	raw := `{"kind": "surreal", "template": "%s reimagined as a gladiator, leather and bronze armor, dramatic arena lighting."}`
	ds, err := parseNamedStyleTemplateJSON(raw, "gladiator", "princess")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.Label != "gladiator" {
		t.Errorf("expected the label to be the caller-fixed name, not model output, got %q", ds.Label)
	}
	if ds.DiscoveredFor != "princess" {
		t.Errorf("expected DiscoveredFor to record the triggering subject, got %q", ds.DiscoveredFor)
	}
}

func TestParseNamedStyleTemplateJSON_StripsMarkdownFences(t *testing.T) {
	raw := "```json\n" + `{"kind": "historical", "template": "%s depicted as a gladiator."}` + "\n```"
	ds, err := parseNamedStyleTemplateJSON(raw, "gladiator", "princess")
	if err != nil || ds == nil {
		t.Fatalf("expected fenced JSON to parse, got %+v, err=%v", ds, err)
	}
}

func TestParseNamedStyleTemplateJSON_RejectsMissingPlaceholder(t *testing.T) {
	raw := `{"kind": "surreal", "template": "a gladiator, no placeholder here."}`
	if _, err := parseNamedStyleTemplateJSON(raw, "gladiator", "princess"); err == nil {
		t.Fatal("expected an error for a template missing the placeholder")
	}
}

func TestParseNamedStyleTemplateJSON_RejectsBadKind(t *testing.T) {
	raw := `{"kind": "whimsical", "template": "%s as a gladiator."}`
	if _, err := parseNamedStyleTemplateJSON(raw, "gladiator", "princess"); err == nil {
		t.Fatal("expected an error for an invalid kind")
	}
}

func TestExtractGeminiText_ConcatenatesParts(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"parts":[{"text":"{\"propose\""},{"text":": false}"}]}}]}`)
	text, err := extractGeminiText(raw)
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"propose": false}` {
		t.Errorf("expected concatenated parts, got %q", text)
	}
}

func TestStyleFromDiscovered_ValidatesPlaceholder(t *testing.T) {
	good := discoveredStyle{Label: "origami", Kind: "surreal", Template: "%s folded from origami paper."}
	if st, ok := styleFromDiscovered(good); !ok || st.Prompt("a duck") != "a duck folded from origami paper." {
		t.Errorf("expected a valid template to convert cleanly, got ok=%v", ok)
	}

	bad := discoveredStyle{Label: "broken", Kind: "surreal", Template: "no placeholder here"}
	if _, ok := styleFromDiscovered(bad); ok {
		t.Error("expected a template with no placeholder to be rejected")
	}
}

func TestCombinedStylePool_IncludesHardcodedAndDiscovered(t *testing.T) {
	discovered := []discoveredStyle{{Label: "origami", Kind: "surreal", Template: "%s made of origami."}}
	pool := combinedStylePool(discovered)
	wantBase := len(promptoverseStyles) + len(promptoverseRareStyles)
	if len(pool) != wantBase+1 {
		t.Fatalf("expected %d styles, got %d", wantBase+1, len(pool))
	}
	if _, ok := styleByLabelInPool(pool, "origami"); !ok {
		t.Error("expected the discovered style to be present in the combined pool")
	}
	if _, ok := styleByLabelInPool(pool, "claymation"); !ok {
		t.Error("expected a hardcoded style to still be present in the combined pool")
	}
}

func TestCombinedStylePool_SkipsMalformedDiscovered(t *testing.T) {
	discovered := []discoveredStyle{{Label: "broken", Kind: "surreal", Template: "no placeholder"}}
	pool := combinedStylePool(discovered)
	wantBase := len(promptoverseStyles) + len(promptoverseRareStyles)
	if len(pool) != wantBase {
		t.Errorf("expected the malformed discovered style to be dropped, got %d styles (base %d)", len(pool), wantBase)
	}
}

func TestDiscoveredStyles_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discovered.json")
	ds := discoveredStyle{Label: "origami", Kind: "surreal", Template: "%s made of origami.", DiscoveredFor: "a duck"}
	if err := appendDiscoveredStyle(path, ds); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDiscoveredStyles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Label != "origami" {
		t.Errorf("expected 1 round-tripped style, got %+v", loaded)
	}
}

func TestDiscoveredStyles_LoadNonexistentFileReturnsEmpty(t *testing.T) {
	loaded, err := loadDiscoveredStyles(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file should not error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected no styles, got %+v", loaded)
	}
}

func TestParseStyleProposalJSON_MalformedJSONErrors(t *testing.T) {
	_, err := parseStyleProposalJSON("not json at all", []string{}, "a duck")
	if err == nil {
		t.Fatal("expected an error for unparseable text")
	}
	if !strings.Contains(err.Error(), "decode style proposal JSON") {
		t.Errorf("expected a decode error, got: %v", err)
	}
}
