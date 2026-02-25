package proxy

import "testing"

func TestComputeCost(t *testing.T) {
	price := map[string]Pricing{
		"model-a": {InputPerMillion: 2, OutputPerMillion: 8},
	}
	result := ComputeCost(price, "model-a", 500000, 250000)
	want := 1.0 + 2.0
	if result.CostUSD != want {
		t.Fatalf("expected %f, got %f", want, result.CostUSD)
	}
	if result.UnknownModel {
		t.Fatal("expected known model")
	}
}

func TestComputeCost_UnknownModel(t *testing.T) {
	price := map[string]Pricing{
		"model-a": {InputPerMillion: 2, OutputPerMillion: 8},
	}
	result := ComputeCost(price, "model-x", 500000, 250000)
	if result.CostUSD != 0 {
		t.Fatalf("expected 0 cost for unknown model, got %f", result.CostUSD)
	}
	if !result.UnknownModel {
		t.Fatal("expected unknown model flag")
	}
}
