package proxy

type CostResult struct {
	CostUSD      float64
	UnknownModel bool
}

func ComputeCost(pricing map[string]Pricing, model string, inputTokens, outputTokens int64) CostResult {
	if len(pricing) == 0 || model == "" {
		return CostResult{UnknownModel: model != ""}
	}

	p, ok := pricing[model]
	if !ok {
		return CostResult{UnknownModel: true}
	}

	inCost := (float64(inputTokens) / 1_000_000.0) * p.InputPerMillion
	outCost := (float64(outputTokens) / 1_000_000.0) * p.OutputPerMillion
	return CostResult{CostUSD: inCost + outCost}
}
