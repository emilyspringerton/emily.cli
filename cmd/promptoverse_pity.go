// cmd/promptoverse_pity.go — pity-adjusted probability for the rare-style
// and spontaneous-discovery rolls in runPromptOVerseAdd.
//
// Founder direction: "like the same way a legendary pull percentage goes
// up after opening a certain number of packs." A flat, independent
// probability each run (what promptoverseRareStyleChance/
// promptoverseSpontaneousDiscoveryChance started as) can go a very long
// time without firing at all -- exactly the kind of "technically random,
// not satisfying" outcome the founder's marble-bag framing was pushing
// back on for style SELECTION, and the same complaint applies to these
// binary "does it happen this run" rolls too. Standard gacha/loot pity
// pattern: track how many runs in a row the roll has NOT triggered,
// escalate the effective chance each miss, reset to the base on a hit,
// cap at 1.0 so a long enough drought guarantees it fires.
//
// Scope note: pity is applied to whether a rare style becomes ELIGIBLE
// for this run's selection (runPromptOVerseAdd's exclude-by-default-
// unless-rolled step), not to whether it's actually the one picked --
// once eligible it still competes fairly in the marble-bag draw
// (selectStylesForSubject) against everything else. Tracking pity all the
// way through to the final pick would need restructuring that draw to
// special-case one candidate, which is a bigger change than this ask
// needs; escalating the odds of being in the running already directly
// addresses "these should occasionally trigger."
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

const (
	promptoverservePityFileName = "promptoverse-pity.json"
	// promptoverservePityFibScale scales the Fibonacci sequence into the
	// chance escalation per consecutive miss -- founder: "use fibinacci...
	// whatever you think make it more novel" (in place of a flat linear
	// bump). Fibonacci growth starts gentle (fib(1..4) = 1,2,3,5) and
	// accelerates hard (fib(10) = 55, fib(15) = 610), so early misses barely
	// move the odds -- staying close to the honest "somewhat rare" base --
	// while a genuinely long drought closes out fast instead of crawling
	// toward guaranteed at a flat, foreseeable rate.
	promptoverservePityFibScale = 0.01
)

type pityState struct {
	RareStyleRunsSinceTrigger int `json:"rare_style_runs_since_trigger"`
	DiscoveryRunsSinceTrigger int `json:"discovery_runs_since_trigger"`
}

func pityStatePath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverservePityFileName)
}

func loadPityState(path string) (pityState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return pityState{}, nil
	}
	if err != nil {
		return pityState{}, err
	}
	if len(data) == 0 {
		return pityState{}, nil
	}
	var st pityState
	if err := json.Unmarshal(data, &st); err != nil {
		return pityState{}, fmt.Errorf("decode pity state: %w", err)
	}
	return st, nil
}

func savePityState(path string, st pityState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// pityAdjustedChance escalates a base probability by runsSinceTrigger
// consecutive misses, capped at 1.0 (guaranteed). Pure function, testable
// without file IO or real randomness.
func pityAdjustedChance(base float64, runsSinceTrigger int) float64 {
	chance := base + float64(fibonacci(runsSinceTrigger))*promptoverservePityFibScale
	if chance > 1.0 {
		chance = 1.0
	}
	return chance
}

// fibonacci is the standard sequence (fib(0)=0, fib(1)=1, fib(2)=1,
// fib(3)=2, ...), iterative and capped at n=50 -- pityAdjustedChance's
// scale factor guarantees the 1.0 cap well before n gets anywhere near
// that large, the cap here is purely a defensive bound against an
// unexpectedly huge runsSinceTrigger overflowing int64 arithmetic.
func fibonacci(n int) int64 {
	if n < 0 {
		return 0
	}
	if n > 50 {
		n = 50
	}
	var a, b int64 = 0, 1
	for i := 0; i < n; i++ {
		a, b = b, a+b
	}
	return a
}
