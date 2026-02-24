package proxy

func ComputeCostUSD(pricing map[string]Pricing, model string, inputTokens, outputTokens int64) float64 {
	if len(pricing) == 0 || model == "" {
		return 0
	}

	p, ok := pricing[model]
	if !ok {
		return 0
	}

	inCost := (float64(inputTokens) / 1_000_000.0) * p.InputPerMillion
	outCost := (float64(outputTokens) / 1_000_000.0) * p.OutputPerMillion
	return inCost + outCost
}
