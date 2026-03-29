package main

import (
	"strings"
	"testing"
)

func TestClipPromptPreviewClipsLongPrompts(t *testing.T) {
	var input strings.Builder
	for i := 0; i < dryRunPromptPreviewLines+5; i++ {
		input.WriteString("line\n")
	}

	got := clipPromptPreview(input.String())
	if !strings.Contains(got, "...[clipped]") {
		t.Fatalf("clipped prompt = %q, want clipped marker", got)
	}
}
