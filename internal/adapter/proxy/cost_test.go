package proxy

import "testing"

func TestComputeCostUSD(t *testing.T) {
	price := map[string]Pricing{
		"model-a": {InputPerMillion: 2, OutputPerMillion: 8},
	}
	got := ComputeCostUSD(price, "model-a", 500000, 250000)
	want := 1.0 + 2.0
	if got != want {
		t.Fatalf("expected %f, got %f", want, got)
	}
}
