// cmd/promptoverse_style_sweep.go — `add <count> --tag <style>` with NO
// subject given.
//
// Founder, real-time: "i need a way to force generations of a style like
// game sprite or pixel art if i dont specify a subject but do specify
// count and do set a tag all of the styles should lock to that style tag
// of the count specified." The normal `add [<subject>] <count>` shape
// treats <count> as "how many different STYLES for this one subject" --
// wrong for a request like "give me 10 game sprites": that wants <count>
// DIFFERENT auto-picked SUBJECTS, all locked to the one forced style, not
// one subject repeated across styles. runPromptOVerseAdd detects this
// shape (autoSubject && tag != "") and delegates here instead of running
// its normal single-subject path.
package cmd

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func runPromptOVerseAddStyleSweep(cfg *config.Config, tags []string, count int, force, slow bool) int {
	// Same tag-combination rule add's normal path uses: 2+ --tag values
	// become one new hybrid style label instead of N separate styles (see
	// ComponentStyles' doc comment in promptoverse_discover.go). A style
	// sweep locked to a hybrid is the same idea one level up: <count>
	// different subjects, all in that one blended style.
	tag := tags[0]
	var hybridComponents []string
	if len(tags) > 1 {
		hybridComponents = tags
		tag = strings.Join(tags, " × ")
	}

	client := iduna.New(cfg.IDUNABaseURL, cfg.IDUNAAgentName, cfg.IDUNAAgentSecret)
	existing, err := client.ListPromptOVerseNodes()
	if err != nil {
		fmt.Fprintf(os.Stderr, "list existing nodes (needed to dedupe): %v\n", err)
		return 1
	}

	discoveredPath := discoveredStylesPath(cfg)
	discovered, err := loadDiscoveredStyles(discoveredPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load discovered styles: %v\n", err)
		return 1
	}
	pool := combinedStylePool(discovered)

	// Resolve or create the locked style ONCE, up front -- <count>
	// subjects sharing one style should only ever create/expand that
	// style's template a single time, not once per subject.
	st, ok := styleByLabelInPool(pool, tag)
	if !ok {
		token, tokErr := gcloudAccessToken()
		if tokErr != nil {
			fmt.Fprintf(os.Stderr, "cannot create new style %q (gcloud auth: %v)\n", tag, tokErr)
			return 2
		}
		newStyle, expErr := expandNamedStyle(token, tag, "(style sweep, no single triggering subject)")
		if expErr != nil {
			fmt.Fprintf(os.Stderr, "failed to create style %q: %v\n", tag, expErr)
			return 1
		}
		newStyle.ComponentStyles = hybridComponents
		if err := appendDiscoveredStyle(discoveredPath, *newStyle); err != nil {
			fmt.Fprintf(os.Stderr, "failed to persist style %q: %v\n", tag, err)
			return 1
		}
		st, ok = styleFromDiscovered(*newStyle)
		if !ok {
			fmt.Fprintf(os.Stderr, "model produced a malformed template for %q\n", tag)
			return 1
		}
		fmt.Printf("created style %q and locked this sweep to it\n", tag)
	} else {
		fmt.Printf("locking this sweep to existing style %q\n", tag)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pityPath := pityStatePath(cfg)
	pity, err := loadPityState(pityPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load pity state: %v\n", err)
		return 1
	}

	// Subjects that already have this exact style, published or queued --
	// a sweep must never redundantly re-request a (subject, style) pair
	// that already exists, same guarantee the normal add path gives.
	haveStyle := map[string]bool{}
	for _, n := range existing {
		if n.Label == st.Label {
			haveStyle[n.Subject] = true
		}
	}
	path := queuePath(cfg)
	pending, err := loadQueue(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read queue (needed to dedupe): %v\n", err)
		return 1
	}
	for _, it := range pending {
		if it.StyleLabel == st.Label {
			haveStyle[it.Subject] = true
		}
	}

	pickedThisSweep := map[string]bool{}
	now := time.Now().UTC()
	var items []queueItem
	for i := 0; i < count; i++ {
		extraExclude := make(map[string]bool, len(haveStyle)+len(pickedThisSweep))
		for s := range haveStyle {
			extraExclude[s] = true
		}
		for s := range pickedThisSweep {
			extraExclude[s] = true
		}
		picked, perr := pickSubject(cfg, existing, rng, &pity, extraExclude)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "stopped after %d/%d -- no more eligible subjects: %v\n", i, count, perr)
			break
		}
		pickedThisSweep[picked] = true
		items = append(items, queueItem{Subject: picked, StyleLabel: st.Label, EnqueuedAt: now, Forced: true})
		fmt.Printf("  queued %q x %q\n", st.Label, picked)
	}

	if err := savePityState(pityPath, pity); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to persist pity state: %v\n", err)
	}

	if len(items) == 0 {
		fmt.Printf("nothing queued -- every eligible subject already has %q (published or queued)\n", st.Label)
		return 0
	}

	// Forced (--tag-style) requests jump the FRONT of the whole queue and
	// drain first, same convention the normal single-subject forced-tag
	// path uses.
	if err := prependQueue(path, items); err != nil {
		fmt.Fprintf(os.Stderr, "enqueue (front): %v\n", err)
		return 1
	}
	fmt.Printf("queued %d/%d request(s) for style %q at the FRONT of the queue\n", len(items), count, st.Label)

	return drainQueue(cfg, force, slow)
}
