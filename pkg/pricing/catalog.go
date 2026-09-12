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
	"sort"

	"gopkg.in/yaml.v3"

	"fluxen/pkg/types"
)

//go:embed catalog.yaml
var catalogYAML []byte

// ModelPrice is one catalog entry: per-million-token input/output prices
// in micro-USD for a specific (provider, model) pair, plus the shape
// fields (Part H.1) the Model Cost detector (Part G.3.1) needs to decide
// whether a request could plausibly run on a cheaper candidate.
type ModelPrice struct {
	ID                 string   `yaml:"id"`
	Provider           string   `yaml:"provider"`
	InputPerMTokMicro  int64    `yaml:"input_per_mtok_micro"`
	OutputPerMTokMicro int64    `yaml:"output_per_mtok_micro"`
	ContextWindow      int      `yaml:"context_window"`
	MaxOutput          int      `yaml:"max_output"`
	Capabilities       []string `yaml:"capabilities"`
	Tier               string   `yaml:"tier"`
	// DowngradeCandidatesFor lists the model ids this entry is a declared
	// cheaper stand-in for — the *only* source of candidates the Model
	// Cost detector considers (Part G.3.1: "never invented at detection
	// time").
	DowngradeCandidatesFor []string `yaml:"downgrade_candidates_for"`
}

// HasCapability reports whether the model declares a capability
// (Part H.1's capabilities list: tools, vision, json_schema, streaming).
func (m ModelPrice) HasCapability(cap string) bool {
	for _, c := range m.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
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

// DowngradeCandidates returns every catalog entry that declares model as
// one of its downgrade_candidates_for — the Model Cost detector's entire
// candidate set for that model (Part G.3.1), sorted by id for a
// deterministic iteration order.
func (c *Catalog) DowngradeCandidates(model string) []ModelPrice {
	var out []ModelPrice
	for _, p := range c.byModel {
		for _, target := range p.DowngradeCandidatesFor {
			if target == model {
				out = append(out, p)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
