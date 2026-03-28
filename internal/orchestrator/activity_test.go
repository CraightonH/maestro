package orchestrator

import (
	"strings"
	"testing"
)

func TestTailBufferKeepsNewestBytesWithinLimit(t *testing.T) {
	var buf tailBuffer
	buf.Append([]byte(strings.Repeat("a", runOutputTailBytes-10)))
	buf.Append([]byte(strings.Repeat("b", 32)))

	got := buf.String()
	if len(got) != runOutputTailBytes {
		t.Fatalf("tail length = %d, want %d", len(got), runOutputTailBytes)
	}
	want := strings.Repeat("a", runOutputTailBytes-32) + strings.Repeat("b", 32)
	if got != want {
		t.Fatalf("tail suffix missing newest bytes: %q", got[len(got)-64:])
	}
}
