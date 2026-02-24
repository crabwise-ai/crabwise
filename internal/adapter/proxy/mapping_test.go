package proxy

import (
	"testing"
)

func TestNormalizeSelector(t *testing.T) {
	if got := normalizeSelector("$.choices[0].finish_reason"); got != "choices.0.finish_reason" {
		t.Fatalf("unexpected selector normalization: %s", got)
	}
}
