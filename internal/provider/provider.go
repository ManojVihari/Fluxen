package provider

import (
	"fmt"
	"log"
)

func Generate(provider string, apiKey string, model string, prompt string) (string, error) {
	log.Printf("🌐 [Provider] Selecting provider: %s | Model: %s", provider, model)

	switch provider {
	case "ollama":
		log.Printf("🌐 [Ollama] Calling Ollama API...")
		return GenerateOllama(model, prompt)

	case "openai":
		log.Printf("🌐 [OpenAI] Calling OpenAI API...")
		return GenerateOpenAI(model, prompt, apiKey)

	default:
		return "", fmt.Errorf("❌ unknown provider: %s", provider)
	}
}
