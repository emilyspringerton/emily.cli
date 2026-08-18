// cmd/promptoverse_deadletter.go — persisted record of permanently
// content-policy-blocked (subject, style) attempts.
//
// Founder direction: "we need a dead letter queue that tracks content
// violations like our rapunzel icecream so we can start to track a
// dataset that has IP implications (rapunzel is not disney but certain
// depictions of her are so icecream or candy rapunzle may proportionately
// cause more content sensitive api responses)" + "dead letter queue should
// not be retried" (already true -- drainQueue drops these, see the
// errVertexContentBlocked skip branch) + "the data should just be
// tracked" (no remediation logic here, purely a durable log) + "add a
// page on iduna to view that data" (folded into the same discovery page/
// endpoint as the style registry and GPT-2 candidates, not a separate
// page -- see IDUNA's DiscoveryHandler).
//
// Append-only JSONL, same shape as the queue file, but items are NEVER
// removed or replayed -- this is a dataset, not a work queue.
package cmd

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

const promptoverseDeadLetterFileName = "promptoverse-content-blocked.jsonl"

// deadLetterEntry is one permanently-blocked generation attempt.
type deadLetterEntry struct {
	Subject    string    `json:"subject"`
	StyleLabel string    `json:"style_label"`
	Reason     string    `json:"reason"`  // Vertex finishReason, e.g. IMAGE_PROHIBITED_CONTENT
	Message    string    `json:"message"` // Vertex finishMessage, if any
	BlockedAt  time.Time `json:"blocked_at"`
}

func deadLetterPath(cfg *config.Config) string {
	root := cfg.EmilyRoot
	if root == "" {
		root = "/home/fatbaby/EMILY"
	}
	return filepath.Join(root, "var", promptoverseDeadLetterFileName)
}

// appendDeadLetter records one blocked attempt. Best-effort by design of
// its callers (a failure here should never mask or block on the actual
// generation failure it's recording) -- returns the error so the caller
// can decide how loud to be about it, rather than swallowing it here.
func appendDeadLetter(path string, entry deadLetterEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func loadDeadLetters(path string) ([]deadLetterEntry, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []deadLetterEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e deadLetterEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip a corrupt line rather than fail the whole read
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
