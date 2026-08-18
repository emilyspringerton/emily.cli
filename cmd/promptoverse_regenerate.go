// cmd/promptoverse_regenerate.go — "regenerate with variation" (S176-30).
//
// Founder, real-time: "i need to gen lil wayne papercraft with a red
// hoodie instead of a grey goodie [hoodie]" then, on how to build it,
// "add that feature to the cli whatever affordance makes sense to get
// that functionality." Scoped correctly only after an initial wrong
// design (overwrite-in-place) was caught mid-build: "we need to keep
// both and i think for seo reasons we should condense the forced feature
// leaf nodes onto the same html page" -- a regeneration is ADDITIVE
// (IDUNA POST /api/v1/promptoverse/nodes/{slug}/variants), never a
// replace, and lands on the SAME leaf page as the original.
package cmd

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func runPromptOVerseRegenerate(args []string) int {
	note := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--note":
			if i+1 >= len(args) {
				fmt.Fprintln(os.Stderr, "emily promptoverse regenerate: --note requires a value")
				return 1
			}
			note = strings.TrimSpace(args[i+1])
			i++
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse regenerate <slug> --note \"what should be different\"")
		return 1
	}
	slug := rest[0]
	if note == "" {
		fmt.Fprintln(os.Stderr, "emily promptoverse regenerate: --note is required (what should be different about this variant)")
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	existing, err := client.GetPromptOVerseNodeBySlug(slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "look up %q: %v\n", slug, err)
		return 1
	}

	token, err := gcloudAccessToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gcloud auth: %v\n", err)
		return 2
	}

	revisePrompt := fmt.Sprintf(`Revise this image-generation prompt to incorporate ONE specific change, keeping everything else about the style/composition/subject identical:

Original prompt: %q

Requested change: %q

Respond with ONLY the revised prompt text -- no commentary, no quotes, no markdown.`, existing.ExpandedPrompt, note)
	revisedExpanded, err := vertexTextGenerate(token, revisePrompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "revise prompt: %v\n", err)
		return 1
	}
	revisedExpanded = strings.TrimSpace(revisedExpanded)
	revisedEZ := existing.EZPrompt
	if revisedEZ == "" {
		revisedEZ = existing.Label + " " + existing.Subject
	}

	fmt.Printf("generating variant of %q: %s\n", slug, note)
	imgBytes, err := vertexGenerateImage(token, revisedExpanded)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate image: %v\n", err)
		return 1
	}

	url, err := client.AddPromptOVerseVariant(slug, iduna.PromptOVerseVariant{
		EZPrompt:       revisedEZ,
		ExpandedPrompt: revisedExpanded,
		ImageBase64:    base64.StdEncoding.EncodeToString(imgBytes),
		Note:           note,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "add variant: %v\n", err)
		return 1
	}
	fmt.Printf("OK -> %s (variant, original kept)\n", url)
	return 0
}
