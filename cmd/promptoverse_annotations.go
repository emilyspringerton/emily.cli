// cmd/promptoverse_annotations.go — subject-level prompt annotations.
//
// Founder, real-time: gens of "Paimon" kept meaning the Genshin Impact
// character rather than TYLER's own "Paimon, the Court Voice" (a Goetia
// king, multiverse_heroes.md #20) -- risking erroneous third-party-IP
// content flags on a name we have our own real lore for (same class of
// problem as the "Rapunzel is not disney but certain depictions..." dead
// letter precedent). Renaming the SUBJECT itself (e.g. "Paimon (demon)")
// was explicitly rejected -- "we dont also want to fragment our ez
// prompts" -- so the EZ prompt and the taxonomy subject both stay exactly
// "Paimon" forever. Instead, an ANNOTATION sticks to the subject itself
// (not to any one queued item, not per-style) and gets appended to the
// real generation prompt only, pulled from two canonical sources: TYLER's
// Goetia frequency table (lore/field_activation_logs.md, "the field with
// the 72 frequencies") and the hero compendium (multiverse_heroes.md).
//
// A subject can carry MORE THAN ONE annotation, keyed by alias -- founder:
// "we can alias paimon (Genshin Impact) to a different annotation on the
// same subject in our data model." One alias is the subject's default
// (used automatically); others are opt-in per generation via
// --annotation-alias, for the rare case a batch deliberately wants the
// other framing without ever forking the subject/taxonomy itself.
//
// Storage is a flat JSON file, same shape as pity state / discovered
// styles elsewhere in this package -- one small file per concern, not a
// database, since this whole CLI treats EMILY_ROOT/var as its state dir.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

const promptoverseAnnotationsFileName = "promptoverse-subject-annotations.json"

type subjectAnnotation struct {
	Text string `json:"text"`
	// Source records where the text came from -- "manual" or "tyler-lore"
	// -- so a future audit can tell a hand-written disambiguation from an
	// auto-derived one.
	Source string    `json:"source"`
	SetAt  time.Time `json:"set_at"`
}

// subjectAnnotationSet is everything stored against one subject: possibly
// several named annotations, with DefaultAlias marking which one gets
// applied automatically when a generation doesn't ask for a specific one.
type subjectAnnotationSet struct {
	DefaultAlias string                       `json:"default_alias"`
	Aliases      map[string]subjectAnnotation `json:"aliases"`
}

func annotationsPath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverseAnnotationsFileName)
}

// annotationKey normalizes a subject to a lookup key -- case/whitespace
// only, so "Paimon" and "paimon " land on the same entry. Subjects are
// otherwise free text everywhere else in this package; this is the one
// place matching needs to be forgiving.
func annotationKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func loadSubjectAnnotations(path string) (map[string]subjectAnnotationSet, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]subjectAnnotationSet{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return map[string]subjectAnnotationSet{}, nil
	}
	var m map[string]subjectAnnotationSet
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]subjectAnnotationSet{}
	}
	return m, nil
}

func saveSubjectAnnotations(path string, m map[string]subjectAnnotationSet) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// setSubjectAnnotationAlias writes (or overwrites) one named alias under
// subject. makeDefault forces it to become the subject's default even if
// another alias already holds that role; otherwise the first alias ever
// set for a subject becomes the default automatically.
func setSubjectAnnotationAlias(cfg *config.Config, subject, alias, text, source string, makeDefault bool) error {
	path := annotationsPath(cfg)
	m, err := loadSubjectAnnotations(path)
	if err != nil {
		return err
	}
	key := annotationKey(subject)
	set, ok := m[key]
	if !ok {
		set = subjectAnnotationSet{Aliases: map[string]subjectAnnotation{}}
	}
	if set.Aliases == nil {
		set.Aliases = map[string]subjectAnnotation{}
	}
	set.Aliases[alias] = subjectAnnotation{Text: text, Source: source, SetAt: time.Now().UTC()}
	if makeDefault || set.DefaultAlias == "" {
		set.DefaultAlias = alias
	}
	m[key] = set
	return saveSubjectAnnotations(path, m)
}

// getSubjectAnnotation resolves subject's annotation. An empty alias means
// "use the subject's default alias." Returns ok=false if the subject has
// no annotations at all, or the named alias doesn't exist.
func getSubjectAnnotation(cfg *config.Config, subject, alias string) (subjectAnnotation, bool) {
	m, err := loadSubjectAnnotations(annotationsPath(cfg))
	if err != nil {
		return subjectAnnotation{}, false
	}
	set, ok := m[annotationKey(subject)]
	if !ok {
		return subjectAnnotation{}, false
	}
	if alias == "" {
		alias = set.DefaultAlias
	}
	if alias == "" {
		return subjectAnnotation{}, false
	}
	a, ok := set.Aliases[alias]
	return a, ok
}

// annotatePrompt appends subject's resolved annotation (if any) to the
// real generation prompt. The EZ prompt shown in the gallery is never
// touched -- callers must apply this only to the expanded/generation
// prompt. An empty alias resolves to the subject's default.
func annotatePrompt(cfg *config.Config, subject, alias, prompt string) string {
	a, ok := getSubjectAnnotation(cfg, subject, alias)
	if !ok || strings.TrimSpace(a.Text) == "" {
		return prompt
	}
	return prompt + "\n\n" + a.Text
}

// heroEntryPattern matches one numbered entry header in
// TYLER/multiverse_heroes.md, e.g.:
//  20. **Paimon, the Court Voice** ("Paimon") [MYTHIC] — *A king who...*
//
// Capture groups: (1) full name, (2) nickname, (3) hook sentence(s) up to
// the closing '*'.
var heroEntryPattern = regexp.MustCompile(`(?m)^\d+\.\s+\*\*(.+?)\*\*\s+\("(.+?)"\)\s+\[[^\]]+\]\s+—\s+\*(.+?)\*`)

// fieldSignaturePattern matches the "*Field signature:* ..." line that
// follows a hero entry's lore paragraph.
var fieldSignaturePattern = regexp.MustCompile(`(?m)^\s*\*Field signature:\*\s+(.+)$`)

type heroLoreEntry struct {
	FullName       string
	Nickname       string
	Hook           string
	FieldSignature string
}

// findHeroEntry searches TYLER's hero compendium for an entry whose full
// name or nickname matches subject (case-insensitive), and pulls the hook
// sentence plus its Field signature line (the Goetia-frequency-table data
// point per entry -- multiverse_heroes.md embeds this per hero rather than
// requiring a second lookup into field_activation_logs.md for entries
// that already carry their own signature line).
func findHeroEntry(tylerRoot, subject string) (heroLoreEntry, bool, error) {
	path := filepath.Join(tylerRoot, "multiverse_heroes.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return heroLoreEntry{}, false, err
	}
	content := string(b)
	needle := annotationKey(subject)

	matches := heroEntryPattern.FindAllStringSubmatchIndex(content, -1)
	for i, m := range matches {
		fullName := content[m[2]:m[3]]
		nickname := content[m[4]:m[5]]
		hook := content[m[6]:m[7]]
		if annotationKey(nickname) != needle && !strings.Contains(annotationKey(fullName), needle) {
			continue
		}
		// Entry body runs from this header to the next entry's header (or
		// end of file) -- the Field signature line lives somewhere in that
		// span, after the lore paragraph.
		end := len(content)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		body := content[m[0]:end]
		sig := ""
		if sm := fieldSignaturePattern.FindStringSubmatch(body); sm != nil {
			sig = strings.TrimSpace(sm[1])
		}
		return heroLoreEntry{FullName: fullName, Nickname: nickname, Hook: strings.TrimSpace(hook), FieldSignature: sig}, true, nil
	}
	return heroLoreEntry{}, false, nil
}

// deriveLoreAnnotation builds disambiguating annotation text from the
// TYLER hero compendium for subject, pulling in the Goetia frequency
// signature where the entry has one. Returns ok=false (not an error) if
// no entry matches -- an unmatched subject just doesn't get an
// auto-derived annotation, it isn't a failure of the lookup itself.
func deriveLoreAnnotation(tylerRoot, subject string) (string, bool, error) {
	entry, ok, err := findHeroEntry(tylerRoot, subject)
	if err != nil || !ok {
		return "", false, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Context for %q in this generation: this refers to %s, from the EINHORN_INDUSTRIAL TYLER hero compendium — %s",
		subject, entry.FullName, entry.Hook)
	if entry.FieldSignature != "" {
		fmt.Fprintf(&b, " Field signature: %s.", entry.FieldSignature)
	}
	b.WriteString(" This is not the identically-named character from any unrelated third-party media property.")
	return b.String(), true, nil
}

func runPromptOVerseAnnotations(args []string) int {
	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	if len(args) > 0 {
		switch args[0] {
		case "set":
			return runPromptOVerseAnnotationsSet(cfg, args[1:])
		case "clear":
			return runPromptOVerseAnnotationsClear(cfg, args[1:])
		}
	}
	m, err := loadSubjectAnnotations(annotationsPath(cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load annotations: %v\n", err)
		return 1
	}
	if len(m) == 0 {
		fmt.Println("no subject annotations set")
		return 0
	}
	for subject, set := range m {
		fmt.Printf("%s (default alias: %s)\n", subject, set.DefaultAlias)
		for alias, a := range set.Aliases {
			marker := " "
			if alias == set.DefaultAlias {
				marker = "*"
			}
			fmt.Printf("  %s %s [%s, %s]\n      %s\n", marker, alias, a.Source, a.SetAt.Format(time.RFC3339), a.Text)
		}
	}
	return 0
}

func runPromptOVerseAnnotationsSet(cfg *config.Config, args []string) int {
	fromLore := false
	makeDefault := false
	text := ""
	alias := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--from-lore":
			fromLore = true
		case args[i] == "--default":
			makeDefault = true
		case args[i] == "--text":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--text requires a value")
				return 1
			}
			text = args[i+1]
			i++
		case args[i] == "--alias":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--alias requires a value")
				return 1
			}
			alias = args[i+1]
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse annotations set <subject> [--alias NAME] [--text \"...\" | --from-lore] [--default]")
		return 1
	}
	subject := strings.Join(rest, " ")

	if fromLore {
		derived, ok, err := deriveLoreAnnotation(cfg.TylerRoot, subject)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lore lookup: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "no TYLER hero compendium entry matches %q\n", subject)
			return 1
		}
		text = derived
		if alias == "" {
			alias = "tyler-lore"
		}
	}
	if strings.TrimSpace(text) == "" {
		fmt.Fprintln(os.Stderr, "nothing to set -- pass --text \"...\" or --from-lore")
		return 1
	}
	if alias == "" {
		alias = "manual"
	}
	source := "manual"
	if fromLore {
		source = "tyler-lore"
	}
	if err := setSubjectAnnotationAlias(cfg, subject, alias, text, source, makeDefault); err != nil {
		fmt.Fprintf(os.Stderr, "save annotation: %v\n", err)
		return 1
	}
	fmt.Printf("annotation %q set for %q (%s):\n  %s\n", alias, subject, source, text)
	return 0
}

// runPromptOVerseBackfillAnnotation stamps every already-published node
// for subject with a marker pointing at the annotation now attached to
// that subject -- founder, real-time: "gens that did not include the
// annotation need to be marked as pre annotated with a link to the
// annotation now attached to the top level subject." Reuses the node's
// existing generic Tags field (via IDUNA's new PATCH .../tags endpoint)
// rather than rewriting the node's ExpandedPrompt after the fact, which
// would misrepresent what the image was actually generated from.
func runPromptOVerseBackfillAnnotation(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse backfill-annotation <subject> [--alias NAME]")
		return 1
	}
	alias := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--alias" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--alias requires a value")
				return 1
			}
			alias = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	subject := strings.Join(rest, " ")

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	a, ok := getSubjectAnnotation(cfg, subject, alias)
	if !ok {
		fmt.Fprintf(os.Stderr, "%q has no stored annotation%s -- set one first with `emily promptoverse annotations set`\n",
			subject, aliasSuffix(alias))
		return 1
	}
	resolvedAlias := alias
	if resolvedAlias == "" {
		m, _ := loadSubjectAnnotations(annotationsPath(cfg))
		resolvedAlias = m[annotationKey(subject)].DefaultAlias
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	nodes, err := client.ListPromptOVerseNodes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list existing nodes: %v\n", err)
		return 1
	}

	marked := 0
	for _, n := range nodes {
		if n.Subject != subject {
			continue
		}
		tags := map[string]string{
			"pre_annotation":     "true",
			"annotation_subject": subject,
			"annotation_alias":   resolvedAlias,
		}
		if err := client.MergeNodeTags(n.Slug, tags); err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED to mark %s: %v\n", n.Slug, err)
			continue
		}
		fmt.Printf("  marked %s as pre-annotation (alias %q)\n", n.Slug, resolvedAlias)
		marked++
	}
	fmt.Printf("marked %d existing node(s) for %q with a link to the %q annotation:\n  %s\n", marked, subject, resolvedAlias, a.Text)
	return 0
}

func aliasSuffix(alias string) string {
	if alias == "" {
		return ""
	}
	return fmt.Sprintf(" alias %q", alias)
}

func runPromptOVerseAnnotationsClear(cfg *config.Config, args []string) int {
	alias := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--alias" {
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "--alias requires a value")
				return 1
			}
			alias = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse annotations clear <subject> [--alias NAME]")
		return 1
	}
	subject := strings.Join(rest, " ")
	path := annotationsPath(cfg)
	m, err := loadSubjectAnnotations(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load annotations: %v\n", err)
		return 1
	}
	key := annotationKey(subject)
	if alias == "" {
		delete(m, key)
		fmt.Printf("cleared all annotations for %q\n", subject)
	} else if set, ok := m[key]; ok {
		delete(set.Aliases, alias)
		if set.DefaultAlias == alias {
			set.DefaultAlias = ""
			for remaining := range set.Aliases {
				set.DefaultAlias = remaining
				break
			}
		}
		m[key] = set
		fmt.Printf("cleared alias %q for %q\n", alias, subject)
	}
	if err := saveSubjectAnnotations(path, m); err != nil {
		fmt.Fprintf(os.Stderr, "save annotations: %v\n", err)
		return 1
	}
	return 0
}
