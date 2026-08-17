// cmd/promptoverse.go — emily promptoverse add <subject> <count>
//
// Generates real images for a new Subject (e.g. "ducks") across N existing
// reusable Styles (e.g. "stained glass", "LEGO minifigure"), via Vertex AI's
// gemini-2.5-flash-image ("Nano Banana"), and publishes each as a new leaf
// node to the live Prompt-o-verse gallery (IDUNA's promptoverse.write API).
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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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
	promptoverseVertexProject = "project-d24a71e9-2daf-4b2d-917"
	promptoverseVertexRegion  = "us-central1"
	promptoverseVertexModel   = "gemini-2.5-flash-image"
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

var promptoverseStyles = []style{
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
}

func RunPromptOVerse(args []string) int {
	if len(args) == 0 {
		return promptoverseUsage()
	}
	switch args[0] {
	case "add":
		return runPromptOVerseAdd(args[1:])
	case "styles":
		return runPromptOVerseStyles()
	default:
		fmt.Fprintf(os.Stderr, "emily promptoverse: unknown subcommand %q\n\n", args[0])
		return promptoverseUsage()
	}
}

func promptoverseUsage() int {
	fmt.Print(`emily promptoverse — generate + publish Prompt-o-verse gallery nodes

Subcommands:
  emily promptoverse add <subject> <count>   Generate <count> existing styles applied to <subject>
  emily promptoverse styles                  List the reusable style registry

Example:
  emily promptoverse add ducks 6

Requires: gcloud ADC already authenticated (this box's existing setup),
IDUNA_AGENT_SECRET for EMILY-PRIME (promptoverse.write), iduna.service running.
`)
	return 1
}

func runPromptOVerseStyles() int {
	fmt.Println("Reusable style registry:")
	for i, st := range promptoverseStyles {
		fmt.Printf("  %2d. %s (%s)\n", i+1, st.Label, st.Kind)
	}
	return 0
}

func runPromptOVerseAdd(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: emily promptoverse add <subject> <count>")
		return 1
	}
	subject := strings.TrimSpace(args[0])
	count, err := strconv.Atoi(args[1])
	if err != nil || count <= 0 {
		fmt.Fprintf(os.Stderr, "emily promptoverse add: <count> must be a positive integer, got %q\n", args[1])
		return 1
	}
	if count > len(promptoverseStyles) {
		fmt.Printf("only %d styles in the registry — generating all of them (run 'emily promptoverse styles' to see the list)\n", len(promptoverseStyles))
		count = len(promptoverseStyles)
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
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

	subjectSlug := slugifyPO(subject)
	ok, failed := 0, 0
	for i, st := range promptoverseStyles[:count] {
		fmt.Fprintf(os.Stderr, "generating %s x %s...\n", st.Label, subject)
		prompt := st.Prompt(subject)
		img, err := vertexGenerateImage(token, prompt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED: %v\n", err)
			failed++
			if strings.Contains(err.Error(), "429") {
				// Re-mint in case the failure was auth-adjacent; the real
				// fix for 429 is time, not a new token, but this is cheap.
				if t2, err2 := gcloudAccessToken(); err2 == nil {
					token = t2
				}
			}
			time.Sleep(5 * time.Second)
			continue
		}

		slug := fmt.Sprintf("%s-%s", subjectSlug, slugifyPO(st.Label))
		node := iduna.PromptOVerseNode{
			Slug:           slug,
			Label:          st.Label,
			Subject:        subject,
			Kind:           st.Kind,
			EZPrompt:       fmt.Sprintf("%s %s", st.Label, subject),
			ExpandedPrompt: prompt,
			ImageBase64:    base64.StdEncoding.EncodeToString(img),
			Tags:           map[string]string{"style": st.Label, "subject": subject},
		}
		url, err := client.PostPromptOVerseNode(node)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  publish failed for %s: %v\n", slug, err)
			failed++
			time.Sleep(2 * time.Second)
			continue
		}
		fmt.Printf("  OK -> %s\n", url)
		ok++
		if i < count-1 {
			time.Sleep(5 * time.Second) // mind the rate limits
		}
	}

	fmt.Printf("\n%d/%d succeeded, %d failed.\n", ok, ok+failed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func gcloudAccessToken() (string, error) {
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex ai %d: %s", resp.StatusCode, trimMsgPO(raw))
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						Data string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
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
	return nil, fmt.Errorf("no image data in response")
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
	}
	return b.String()
}

func trimMsgPO(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
