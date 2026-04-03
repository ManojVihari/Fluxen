package analyzer

func EstimateTokens(text string) int {
	return len(text) / 4
}

func EstimateCost(tokens int) float64 {
	costPer1K := 0.002
	return (float64(tokens) / 1000.0) * costPer1K
}