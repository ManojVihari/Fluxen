// Package pricing computes request cost from a versioned catalog. It
// contains no I/O — Calculate is a pure function of (usage, model,
// catalog) — because both the gateway (production) and, from Phase 4
// onward, the simulation engine must call the exact same function (Rule 9
// of the implementation specification: never duplicate pricing logic
// between production and simulation).
package pricing

import (
	_ "embed"
	"fmt"

	"gopkg.in/yaml.v3"

	"fluxen/pkg/types"
)

//go:embed catalog.yaml
var catalogYAML []byte

// ModelPrice is one catalog entry: per-million-token input/output prices
// in micro-USD for a specific (provider, model) pair.
type ModelPrice struct {
	ID                 string `yaml:"id"`
	Provider           string `yaml:"provider"`
	InputPerMTokMicro  int64  `yaml:"input_per_mtok_micro"`
	OutputPerMTokMicro int64  `yaml:"output_per_mtok_micro"`
}

// Catalog is a versioned, immutable price list. Load it once at boot
// (LoadEmbedded) and share it — it has no mutable state.
type Catalog struct {
	Version string
	byModel map[string]ModelPrice // keyed by model id, provider-qualified lookups do the same for now since Phase 1 has one provider
}

type catalogFile struct {
	Version string       `yaml:"version"`
	Models  []ModelPrice `yaml:"models"`
}

// LoadEmbedded parses the catalog embedded in the binary at compile time.
// It only returns an error if catalog.yaml itself is malformed, which
// would be a build-time defect, not a runtime condition — callers can
// treat a non-nil error as fatal at boot.
func LoadEmbedded() (*Catalog, error) {
	return parseCatalog(catalogYAML)
}

func parseCatalog(data []byte) (*Catalog, error) {
	var f catalogFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("pricing: failed to parse catalog: %w", err)
	}

	c := &Catalog{
		Version: f.Version,
		byModel: make(map[string]ModelPrice, len(f.Models)),
	}
	for _, m := range f.Models {
		c.byModel[m.ID] = m
	}
	return c, nil
}

// Lookup returns the price entry for a model id, and whether one exists.
// A missing entry is a normal, expected outcome (Part D.2) — it is not an
// error.
func (c *Catalog) Lookup(model string) (ModelPrice, bool) {
	p, ok := c.byModel[model]
	return p, ok
}

// Calculate prices a request's usage against the catalog. If the model
// has no catalog entry, it returns types.CostUnknown with zero cost —
// never a fabricated price (Part D.2). Calculate never mutates usage or
// the catalog; it is a pure function safe to call from both the gateway
// and the simulation engine.
func Calculate(catalog *Catalog, model string, usage types.ResponseUsage) (costInput, costOutput, costTotal types.Money, status types.CostStatus) {
	price, ok := catalog.Lookup(model)
	if !ok {
		return 0, 0, 0, types.CostUnknown
	}

	in := types.Money(int64(usage.InputTokens) * price.InputPerMTokMicro / 1_000_000)
	out := types.Money(int64(usage.OutputTokens) * price.OutputPerMTokMicro / 1_000_000)
	return in, out, in + out, types.CostKnown
}
