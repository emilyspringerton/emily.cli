package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

func testCfg(t *testing.T) *config.Config {
	t.Helper()
	emilyRoot := t.TempDir()
	tylerRoot := t.TempDir()
	return &config.Config{EmilyRoot: emilyRoot, TylerRoot: tylerRoot}
}

func TestAnnotationKey_NormalizesCaseAndWhitespace(t *testing.T) {
	if annotationKey("  Paimon ") != "paimon" {
		t.Errorf("got %q", annotationKey("  Paimon "))
	}
}

func TestLoadSubjectAnnotations_MissingFileReturnsEmpty(t *testing.T) {
	m, err := loadSubjectAnnotations(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty map, got %+v", m)
	}
}

func TestSetGetSubjectAnnotation_DefaultAliasRoundTrip(t *testing.T) {
	cfg := testCfg(t)
	if err := setSubjectAnnotationAlias(cfg, "Paimon", "tyler-lore", "context text", "tyler-lore", true); err != nil {
		t.Fatalf("set: %v", err)
	}
	a, ok := getSubjectAnnotation(cfg, "paimon", "") // different case, empty alias -> default
	if !ok {
		t.Fatal("expected annotation to be found")
	}
	if a.Text != "context text" || a.Source != "tyler-lore" {
		t.Errorf("unexpected annotation: %+v", a)
	}
}

func TestGetSubjectAnnotation_UnknownSubject(t *testing.T) {
	cfg := testCfg(t)
	if _, ok := getSubjectAnnotation(cfg, "nobody", ""); ok {
		t.Error("expected no annotation for an unknown subject")
	}
}

func TestSetSubjectAnnotationAlias_SecondAliasIsNotDefaultUnlessForced(t *testing.T) {
	cfg := testCfg(t)
	if err := setSubjectAnnotationAlias(cfg, "Paimon", "tyler-lore", "court voice text", "tyler-lore", false); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if err := setSubjectAnnotationAlias(cfg, "Paimon", "genshin-impact", "genshin text", "manual", false); err != nil {
		t.Fatalf("set second: %v", err)
	}
	// Default should still be the first alias, since makeDefault was false
	// and a default already existed.
	def, ok := getSubjectAnnotation(cfg, "Paimon", "")
	if !ok || def.Text != "court voice text" {
		t.Errorf("expected default alias to remain tyler-lore, got %+v (ok=%v)", def, ok)
	}
	// The second alias is still reachable by name.
	alt, ok := getSubjectAnnotation(cfg, "Paimon", "genshin-impact")
	if !ok || alt.Text != "genshin text" {
		t.Errorf("expected genshin-impact alias to be reachable, got %+v (ok=%v)", alt, ok)
	}
}

func TestAnnotatePrompt_AppendsWhenPresent_NoOpWhenAbsent(t *testing.T) {
	cfg := testCfg(t)
	if got := annotatePrompt(cfg, "Nobody", "", "base prompt"); got != "base prompt" {
		t.Errorf("expected no-op, got %q", got)
	}
	if err := setSubjectAnnotationAlias(cfg, "Paimon", "tyler-lore", "extra context", "tyler-lore", true); err != nil {
		t.Fatalf("set: %v", err)
	}
	got := annotatePrompt(cfg, "Paimon", "", "base prompt")
	if !strings.Contains(got, "base prompt") || !strings.Contains(got, "extra context") {
		t.Errorf("expected annotation appended, got %q", got)
	}
	if !strings.HasPrefix(got, "base prompt") {
		t.Errorf("expected base prompt to come first (EZ-prompt-equivalent ordering), got %q", got)
	}
}

func TestAnnotatePrompt_NamedAliasOverridesDefault(t *testing.T) {
	cfg := testCfg(t)
	if err := setSubjectAnnotationAlias(cfg, "Paimon", "tyler-lore", "default text", "tyler-lore", true); err != nil {
		t.Fatalf("set default: %v", err)
	}
	if err := setSubjectAnnotationAlias(cfg, "Paimon", "genshin-impact", "alias text", "manual", false); err != nil {
		t.Fatalf("set alias: %v", err)
	}
	got := annotatePrompt(cfg, "Paimon", "genshin-impact", "base")
	if !strings.Contains(got, "alias text") || strings.Contains(got, "default text") {
		t.Errorf("expected the named alias's text, got %q", got)
	}
}

const heroFixture = `# THE MULTIVERSE HERO COMPENDIUM

## FACTION 1 — TEST

19. **Zagan, the Peer-Reviewed Alchemist** ("Zagan") [MYTHIC] — *Turns anything into anything, on paper.*
    Some lore paragraph here that spans a line.
    *Field signature:* 0.618 Hz · Δφ 47° — well outside the Golden Band, still being peer-reviewed · seed phrase: "the standstill confesses"

20. **Paimon, the Court Voice** ("Paimon") [MYTHIC] — *A king who commands two hundred legions and would rather talk than fight.*
    Traditional Goetia rank preserved: teaches all arts, speaks with total authority.
    *Field signature:* 20.0 Hz · Δφ 30° — Golden Band, center of the range · seed phrase: "let the room keep its own tongue"

21. **Furfur, the Storm Oath** ("Furfur") [MYTHIC] — *Cannot tell the truth outside a binding circle.*
    Some other lore paragraph.
`

func writeHeroFixture(t *testing.T, tylerRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(tylerRoot, "multiverse_heroes.md"), []byte(heroFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func TestFindHeroEntry_MatchesByNickname(t *testing.T) {
	tylerRoot := t.TempDir()
	writeHeroFixture(t, tylerRoot)

	entry, ok, err := findHeroEntry(tylerRoot, "Paimon")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected a match")
	}
	if entry.FullName != "Paimon, the Court Voice" {
		t.Errorf("unexpected FullName: %q", entry.FullName)
	}
	if entry.FieldSignature != `20.0 Hz · Δφ 30° — Golden Band, center of the range · seed phrase: "let the room keep its own tongue"` {
		t.Errorf("unexpected FieldSignature: %q", entry.FieldSignature)
	}
	if !strings.Contains(entry.Hook, "commands two hundred legions") {
		t.Errorf("unexpected Hook: %q", entry.Hook)
	}
}

func TestFindHeroEntry_NoMatch(t *testing.T) {
	tylerRoot := t.TempDir()
	writeHeroFixture(t, tylerRoot)

	_, ok, err := findHeroEntry(tylerRoot, "Master Chief")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected no match for a subject not in the compendium")
	}
}

func TestFindHeroEntry_DoesNotBleedIntoNextEntry(t *testing.T) {
	tylerRoot := t.TempDir()
	writeHeroFixture(t, tylerRoot)

	entry, ok, err := findHeroEntry(tylerRoot, "Furfur")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected a match")
	}
	if entry.FieldSignature != "" {
		t.Errorf("Furfur's fixture entry has no Field signature line -- expected empty, got %q", entry.FieldSignature)
	}
	if strings.Contains(entry.Hook, "binding circle") == false {
		t.Errorf("unexpected Hook: %q", entry.Hook)
	}
}

func TestDeriveLoreAnnotation_BuildsDisambiguatingText(t *testing.T) {
	tylerRoot := t.TempDir()
	writeHeroFixture(t, tylerRoot)

	text, ok, err := deriveLoreAnnotation(tylerRoot, "Paimon")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected a derived annotation")
	}
	for _, want := range []string{"Paimon, the Court Voice", "20.0 Hz", "not the identically-named character"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected derived annotation to contain %q, got %q", want, text)
		}
	}
}

func TestDeriveLoreAnnotation_NoMatchIsNotAnError(t *testing.T) {
	tylerRoot := t.TempDir()
	writeHeroFixture(t, tylerRoot)

	_, ok, err := deriveLoreAnnotation(tylerRoot, "Master Chief")
	if err != nil {
		t.Fatalf("expected no error for an unmatched subject, got %v", err)
	}
	if ok {
		t.Error("expected ok=false for an unmatched subject")
	}
}
