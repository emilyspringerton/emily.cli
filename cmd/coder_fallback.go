// cmd/coder_fallback.go — Qwen2.5-Coder sometimes emits its tool call as text the vLLM hermes
// parser does not recognise (observed: <tools>{"name":..,"arguments":{..}}</tools>, and bare or
// fenced JSON), so tool_calls comes back null with the call sitting in content. Recover those.
package cmd

import (
	"encoding/json"
	"regexp"
	"strings"
)

var tagCallRe = regexp.MustCompile(`(?s)<(?:tool_call|tools|tool|function_call)>\s*(.*?)\s*</(?:tool_call|tools|tool|function_call)>`)
var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// extractToolCalls returns OpenAI-shaped tool_calls found in content, only for known tool names.
func extractToolCalls(content string, known map[string]bool) []any {
	var cands []string
	for _, m := range tagCallRe.FindAllStringSubmatch(content, -1) {
		cands = append(cands, m[1])
	}
	for _, m := range fenceRe.FindAllStringSubmatch(content, -1) {
		cands = append(cands, m[1])
	}
	if t := strings.TrimSpace(content); strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}") {
		cands = append(cands, t)
	}
	var out []any
	for i, c := range cands {
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal([]byte(c), &call) != nil || !known[call.Name] {
			continue
		}
		args := string(call.Arguments)
		if len(args) > 0 && args[0] == '"' { // arguments given as a JSON-encoded string
			var s string
			json.Unmarshal(call.Arguments, &s)
			args = s
		}
		if args == "" || args == "null" {
			args = "{}"
		}
		out = append(out, map[string]any{
			"id": "fallback-" + string(rune('a'+i)), "type": "function",
			"function": map[string]any{"name": call.Name, "arguments": args},
		})
	}
	return out
}
