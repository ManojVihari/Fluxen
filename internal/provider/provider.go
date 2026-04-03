package provider

import "fmt"

func Generate(provider string, apiKey string, model string, prompt string) (string, error) {

	switch provider {
	case "ollama":
		return GenerateOllama(model, prompt)

	case "openai":
		return GenerateOpenAI(model, prompt, apiKey)

	default:
		return "", fmt.Errorf("unknown provider: %s", provider)
	}
}