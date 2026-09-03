// cmd/blog.go — emily blog post
// Publishes a post to okemily.com's blog via IDUNA's real POST /api/v1/blog/posts endpoint.
// No CLI support existed for this before (kanban BLOGREPORT-0111, "long and detailed blog post
// with a list of literally all of the tickets we closed in the last 12 hours") -- every prior
// post ("The Full Eighteen," the State of the Ecosystem series) was published via a raw, one-off
// curl call, hand-minting an M2M token each time. This is real, reusable infra for the same
// recurring "publish a real ecosystem update" need those posts already established as a pattern.

package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func RunBlog(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily blog post [flags]")
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "post":
		return runBlogPost(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q — use post\n", sub)
		return 1
	}
}

func runBlogPost(args []string) int {
	fs := flag.NewFlagSet("blog post", flag.ContinueOnError)
	slug := fs.String("slug", "", "URL slug, lowercase letters/numbers/hyphens (required)")
	title := fs.String("title", "", "post title (required)")
	author := fs.String("author", "Claude Code (guest)", "byline, matches the existing guest-post convention")
	bodyFile := fs.String("body-file", "", "path to a file containing the post body (required unless -body is set)")
	body := fs.String("body", "", "post body text (prefer -body-file for anything longer than a line)")
	adLine := fs.String("ad-line", "", "optional ad line")
	adCTA := fs.String("ad-cta", "", "optional ad call-to-action text")
	adHref := fs.String("ad-href", "", "optional ad link")

	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *slug == "" || *title == "" {
		fmt.Fprintln(os.Stderr, "error: -slug and -title are required")
		return 1
	}
	bodyText := *body
	if *bodyFile != "" {
		raw, err := os.ReadFile(*bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: reading -body-file: %v\n", err)
			return 1
		}
		bodyText = string(raw)
	}
	if bodyText == "" {
		fmt.Fprintln(os.Stderr, "error: -body or -body-file is required")
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	if cfg.IDUNAAgentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set and not found in secrets file")
		return 2
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	url, err := client.PostBlog(iduna.BlogPost{
		Slug:   *slug,
		Title:  *title,
		Author: *author,
		Body:   bodyText,
		AdLine: *adLine,
		AdCTA:  *adCTA,
		AdHref: *adHref,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Published: %s\n", url)
	return 0
}
