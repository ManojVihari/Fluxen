package analyzer

import (
	"strings"
	"unicode"
)

// ModelPricing holds per-1K-token costs for input (prompt) and output (completion).
type ModelPricing struct {
	InputPer1K  float64
	OutputPer1K float64
}

// pricing is based on published per-1K-token rates (USD).
// Ollama models use GPT-4o-mini pricing for testing purposes.
var pricing = map[string]ModelPricing{
	// OpenAI
	"gpt-4o":        {InputPer1K: 0.0025, OutputPer1K: 0.0100},
	"gpt-4o-mini":   {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"gpt-4-turbo":   {InputPer1K: 0.0100, OutputPer1K: 0.0300},
	"gpt-4":         {InputPer1K: 0.0300, OutputPer1K: 0.0600},
	"gpt-3.5-turbo": {InputPer1K: 0.0005, OutputPer1K: 0.0015},
	"o1":            {InputPer1K: 0.0150, OutputPer1K: 0.0600},
	"o1-mini":       {InputPer1K: 0.0030, OutputPer1K: 0.0120},
	// Ollama (using GPT-4o-mini pricing for testing)
	"llama3":         {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"llama3:70b":     {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"mistral":        {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"mistral:latest": {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"codellama":      {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"neural-chat":    {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"ollama":         {InputPer1K: 0.00015, OutputPer1K: 0.0006},
}

// DefaultPricing is used when the model is not in the pricing table.
var DefaultPricing = ModelPricing{InputPer1K: 0.002, OutputPer1K: 0.002}

// GetPricing returns the pricing for a model, falling back to DefaultPricing.
func GetPricing(model string) ModelPricing {
	if p, ok := pricing[model]; ok {
		return p
	}
	return DefaultPricing
}

// EstimateTokens approximates the token count using a word/subword heuristic
// that's closer to BPE tokenization than a flat len/4 ratio.
//
// Rules:
//   - Split on whitespace
//   - Each word counts as 1 token + 1 extra token per 4 chars beyond the first 4
//   - Punctuation-only tokens count as 1
//   - Add 3 tokens overhead for message framing (<|im_start|> etc.)
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}

	words := strings.Fields(text)
	tokens := 0

	for _, word := range words {
		runes := []rune(word)
		pureLen := 0
		punctCount := 0

		for _, r := range runes {
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				punctCount++
			} else {
				pureLen++
			}
		}

		// Each punctuation mark is roughly its own token
		tokens += punctCount

		// Word tokens: 1 base + 1 per 4 extra characters
		if pureLen > 0 {
			tokens += 1 + (pureLen / 5)
		}
	}

	// Message framing overhead
	tokens += 3

	return tokens
}

// EstimateCost calculates cost in USD given prompt/completion tokens and model.
func EstimateCost(model string, promptTokens, completionTokens int) float64 {
	p := GetPricing(model)
	inputCost := (float64(promptTokens) / 1000.0) * p.InputPer1K
	outputCost := (float64(completionTokens) / 1000.0) * p.OutputPer1K
	return inputCost + outputCost
}
