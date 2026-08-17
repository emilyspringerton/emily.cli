package cmd

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// Captured live 2026-08-17: a real gemini-2.5-flash-image response for
// "anime x Rapunzel" -- Vertex's own IP-content filter blocked it, and
// before this fix the resulting "no image data in response" looked
// indistinguishable from an auth/key problem while also permanently
// jamming everything queued behind it.
const rapunzelBlockedResponse = `{
  "candidates": [
    {
      "content": { "role": "model" },
      "finishReason": "IMAGE_PROHIBITED_CONTENT",
      "finishMessage": "Unable to show the generated image due to interests of third-party content providers. Please edit your prompt and try again. If you think this was an error, send feedback. Support code: 35561575."
    }
  ],
  "usageMetadata": { "promptTokenCount": 34, "totalTokenCount": 34 },
  "modelVersion": "gemini-2.5-flash-image"
}`

func TestParseVertexImageResponse_ContentBlockedIsSentinelError(t *testing.T) {
	_, err := parseVertexImageResponse([]byte(rapunzelBlockedResponse))
	if err == nil {
		t.Fatal("expected an error for a content-blocked response")
	}
	if !errors.Is(err, errVertexContentBlocked) {
		t.Errorf("expected errors.Is(err, errVertexContentBlocked) to hold, got: %v", err)
	}
	if !strings.Contains(err.Error(), "IMAGE_PROHIBITED_CONTENT") {
		t.Errorf("expected the finish reason in the error text for diagnosability, got: %v", err)
	}
}

func TestParseVertexImageResponse_ValidImageDecodes(t *testing.T) {
	want := []byte("not a real png but bytes")
	raw := []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"data":"` +
		base64.StdEncoding.EncodeToString(want) + `"}}]}}]}`)
	got, err := parseVertexImageResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("expected decoded image bytes %q, got %q", want, got)
	}
}

func TestParseVertexImageResponse_UnknownFinishReasonIsNotTheSentinel(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"role":"model"},"finishReason":"MAX_TOKENS"}]}`)
	_, err := parseVertexImageResponse(raw)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, errVertexContentBlocked) {
		t.Error("expected a non-policy finish reason to NOT match the content-blocked sentinel (drainQueue must still stop-and-preserve for this, not skip)")
	}
}
