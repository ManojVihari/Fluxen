package pricing

import (
	"testing"

	"fluxen/pkg/types"
)

func TestLoadEmbedded_ParsesRealCatalog(t *testing.T) {
	c, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("unexpected error loading embedded catalog: %v", err)
	}
	if c.Version == "" {
		t.Error("expected a non-empty catalog version")
	}
	if _, ok := c.Lookup("gpt-4o-mini"); !ok {
		t.Error("expected gpt-4o-mini to be in the embedded catalog")
	}
}

func TestCalculate_KnownModel(t *testing.T) {
	catalog, err := parseCatalog([]byte(`
version: "test"
models:
  - id: test-model
    provider: openai
    input_per_mtok_micro: 1000000
    output_per_mtok_micro: 2000000
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usage := types.ResponseUsage{InputTokens: 1000, OutputTokens: 500}
	in, out, total, status := Calculate(catalog, "test-model", usage)

	if status != types.CostKnown {
		t.Fatalf("expected CostKnown, got %v", status)
	}
	// 1000 tokens * 1,000,000 micro / 1,000,000 = 1000 micro-USD input
	if in != 1000 {
		t.Errorf("expected input cost 1000 micro-USD, got %d", in)
	}
	// 500 tokens * 2,000,000 micro / 1,000,000 = 1000 micro-USD output
	if out != 1000 {
		t.Errorf("expected output cost 1000 micro-USD, got %d", out)
	}
	if total != in+out {
		t.Errorf("expected total to equal input+output, got %d != %d", total, in+out)
	}
}

func TestCalculate_UnknownModelNeverFabricatesAPrice(t *testing.T) {
	catalog, err := parseCatalog([]byte(`
version: "test"
models: []
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	usage := types.ResponseUsage{InputTokens: 1000, OutputTokens: 500}
	in, out, total, status := Calculate(catalog, "does-not-exist", usage)

	if status != types.CostUnknown {
		t.Fatalf("expected CostUnknown for a model with no catalog entry, got %v", status)
	}
	if in != 0 || out != 0 || total != 0 {
		t.Errorf("expected zero cost for an unknown model, got in=%d out=%d total=%d", in, out, total)
	}
}
