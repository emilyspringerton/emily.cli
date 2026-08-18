// Package mashupjudge asks an LLM whether a Prompt-o-verse subject is a
// genuine compositional mashup of other subjects already in the registry,
// and which other subjects (if any) describe the same hybrid concept in
// different words.
//
// Why this exists instead of string matching: a first implementation
// attempt used pure lexical rules (substring/subset containment for "is a
// component of," word-order-independent equality for "is the same
// hybrid") and was abandoned mid-build once tested against real founder
// examples. "Tuxedo duck" contains the word "tuxedo" but is plausibly
// just a real duck breed -- a single concept, not a mashup -- so
// containment gives false positives. "Tuxedo duck" and "a duck wearing a
// tuxedo" are the same subject despite sharing almost no words, while
// "tuxedo duck" and "duck tuxedo" are NOT the same subject despite
// sharing every word, just reordered -- so word-bag equality gets it
// backwards in both directions. "The president" can pin a specific real
// referent that generic "president" doesn't, so even blind
// leading-article normalization isn't safe. And a subject's identity can
// drift over real-world time (a Rapunzel generation blocked by content
// policy today doesn't say much about 2046, per current trademark
// state) -- meaning at minimum
// there is no purely mechanical rule that survives these cases. Full
// writeup: EMILY/docs/NORTHSTAR_PROMPT_O_VERSE.md §9, emily.cli/README.md
// "Ontology" section.
//
// Founder, real-time: "i think the ontology problem could be solved with
// a very clever query... llm query... lean on claude or gemini api for
// now... build claude gemini parity for that so we can switch to claude
// or we can even run them in paralell for AB testing in the future." No
// free Claude API credits were available at build time, so Gemini/Vertex
// (already wired for style/subject discovery, see cmd/promptoverse_discover.go)
// is the active default; the Claude provider exists for parity and is
// exercised by tests against a mock server, not live, given the stated
// credit constraint.
package mashupjudge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Judgment is one provider's verdict on one subject, evaluated against the
// current subject registry as of a specific reference time (the "fixed
// point" / "zero point" this package's own doc comment names -- a
// judgment is only valid as of AsOf, not eternally, since the underlying
// real-world facts it depends on (what a phrase idiomatically means, what
// a name currently refers to) can drift).
type Judgment struct {
	Subject               string    `json:"subject"`
	Provider              string    `json:"provider"`
	IsCompositionalMashup bool      `json:"is_compositional_mashup"`
	Components            []string  `json:"components,omitempty"`
	ParaphraseEquivalents []string  `json:"paraphrase_equivalents,omitempty"`
	ReferentStability     string    `json:"referent_stability"` // "generic" | "specific_time_bound" | "fixed"
	Reasoning             string    `json:"reasoning,omitempty"`
	AsOf                  time.Time `json:"as_of"`
}

// Provider is one LLM backend capable of judging mashup/hybrid identity.
// Gemini (Vertex AI) and Claude (Anthropic) both implement this so the
// caller can switch between them, or run both for the future A/B-testing
// use the founder named, without touching call sites.
type Provider interface {
	// Name identifies the provider in a persisted Judgment (e.g. "gemini",
	// "claude") -- so results from different providers can be told apart
	// once cached side by side.
	Name() string
	// Judge asks whether `subject` is a compositional mashup of other
	// labels in `registry`, and which labels (if any) are paraphrase-
	// equivalent hybrids of it. asOf is the reference time the judgment
	// should reason as of (see fixed point / zero point in the package
	// doc) -- always the moment the call is made, not user-suppliable,
	// but threaded explicitly rather than read from time.Now() inside the
	// provider so the resulting Judgment's AsOf is trustworthy even if a
	// future caller wants to backfill/replay judgments against a
	// different historical `registry` snapshot.
	Judge(subject string, registry []string, asOf time.Time) (Judgment, error)
}

// buildPrompt is shared by every provider so their judgments stay
// comparable (same question asked the same way) -- the whole point of
// keeping providers swappable/A-B-testable. Deliberately encodes the
// lessons from the abandoned lexical-matching attempt directly into the
// prompt, not just this package's doc comment, since the LLM has never
// seen that history.
func buildPrompt(subject string, registry []string, asOf time.Time) string {
	registryList := "(none yet)"
	if len(registry) > 0 {
		registryList = strings.Join(registry, ", ")
	}
	return fmt.Sprintf(`You are judging subject identity for Prompt-o-verse, a generative art taxonomy. Subjects are short labels like "Raccoon" or "Fractal Raccoon."

Evaluate this reference time: %s. Some subjects have a meaning that depends on when the question is asked (e.g. "the president" refers to whoever holds the office right now, and a trademark/IP holder's rights can change over years) -- reason about the subject AS OF this date, not as an eternal fact.

Current known subjects already in the registry: %s

Subject to judge: %q

Answer two separate questions, and do not let lexical similarity alone answer either one:

1. IS THIS A COMPOSITIONAL MASHUP? True only if the subject is a deliberate combination of two or more DISTINCT concepts that also exist as their own subjects in the registry above -- not merely a phrase that happens to contain another subject's words. A compound noun that names one single, real, well-established thing (a species, a color pattern, an idiom, a proper noun) is NOT a mashup even if part of its name matches another subject -- e.g. "tuxedo duck" is plausibly just a real duck color-morph name, not a mashup of "tuxedo" clothing and a "duck" animal, even if both those words/subjects exist in the registry.

2. WHICH OTHER SUBJECTS (if any) DESCRIBE THE EXACT SAME CONCEPT, JUST WORDED DIFFERENTLY? Equivalence is about meaning, not word overlap -- two labels with almost no words in common can be the same concept ("tuxedo duck" and "a duck wearing a tuxedo" plausibly ARE the same), while two labels that are word-for-word reorderings of each other are usually NOT the same concept unless you have a real reason to think so ("tuxedo duck" and "duck tuxedo" are plausibly NOT the same -- one is a recognizable compound, the other is just a shuffled phrase with no established meaning).

Also judge REFERENT STABILITY: "generic" (means the same thing regardless of when/where asked, e.g. "a raccoon"), "specific_time_bound" (the correct answer depends on real-world state that changes over time, e.g. "the president," or trademark/IP status of a named character), or "fixed" (pinned to one unchanging real thing, e.g. a specific historical event).

Respond with ONLY raw JSON (no markdown fences, no commentary), in exactly this shape:
{"is_compositional_mashup": bool, "components": ["existing subject label", ...], "paraphrase_equivalents": ["existing subject label", ...], "referent_stability": "generic"|"specific_time_bound"|"fixed", "reasoning": "one or two sentences"}

"components" and "paraphrase_equivalents" must only contain labels that are already in the registry list above (or empty arrays) -- never invent new ones here.`,
		asOf.Format("2006-01-02"), registryList, subject)
}

// parseJudgmentResponse decodes a provider's raw text response into a
// Judgment, filling in Subject/Provider/AsOf (which the model is never
// asked to produce -- those are known to the caller, not the model).
func parseJudgmentResponse(text, subject, provider string, asOf time.Time) (Judgment, error) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var parsed struct {
		IsCompositionalMashup bool     `json:"is_compositional_mashup"`
		Components            []string `json:"components"`
		ParaphraseEquivalents []string `json:"paraphrase_equivalents"`
		ReferentStability     string   `json:"referent_stability"`
		Reasoning             string   `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return Judgment{}, fmt.Errorf("%s: decode judgment JSON: %w (raw: %s)", provider, err, trimForError(text))
	}
	switch parsed.ReferentStability {
	case "generic", "specific_time_bound", "fixed":
	case "":
		parsed.ReferentStability = "generic"
	default:
		return Judgment{}, fmt.Errorf("%s: unrecognized referent_stability %q", provider, parsed.ReferentStability)
	}

	return Judgment{
		Subject:               subject,
		Provider:              provider,
		IsCompositionalMashup: parsed.IsCompositionalMashup,
		Components:            parsed.Components,
		ParaphraseEquivalents: parsed.ParaphraseEquivalents,
		ReferentStability:     parsed.ReferentStability,
		Reasoning:             parsed.Reasoning,
		AsOf:                  asOf,
	}, nil
}

func trimForError(s string) string {
	const max = 200
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
