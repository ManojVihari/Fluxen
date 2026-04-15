package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"math/bits"
	"regexp"
	"strings"
	"time"

	"github.com/ManojVihari/Fluxen/internal/models"
)

const (
	CacheTTL            = 60 * time.Second
	SimilarityThreshold = 0.85
	SimHashBandWidth    = 8 // bits per band for LSH bucketing
	semanticIndexKey    = "semcache:index"
)

type CachedEntry struct {
	Response models.OpenAIResponse `json:"response"`
	Model    string                `json:"model"`
	Tokens   []string              `json:"tokens"`
	SimHash  uint64                `json:"simhash"`
}

// Lookup checks the cache for a semantically similar prompt.
// Tier 1: exact match on normalized hash.
// Tier 2: SimHash bucket scan + Jaccard similarity verification.
func Lookup(ctx context.Context, model string, messages []models.Message) (*models.OpenAIResponse, bool) {
	if Client == nil {
		return nil, false
	}

	normalized := normalizeMessages(messages)
	exactKey := exactCacheKey(model, normalized)

	// --- Tier 1: exact match ---
	val, err := Client.Get(ctx, exactKey).Result()
	if err == nil {
		var entry CachedEntry
		if json.Unmarshal([]byte(val), &entry) == nil {
			log.Println("Cache HIT (exact)")
			return &entry.Response, true
		}
	}

	// --- Tier 2: semantic similarity ---
	tokens := tokenize(normalized)
	sh := simhash(tokens)
	band := simhashBand(sh)
	bandKey := fmt.Sprintf("semcache:band:%s:%s", model, band)

	members, err := Client.SMembers(ctx, bandKey).Result()
	if err != nil || len(members) == 0 {
		return nil, false
	}

	for _, memberKey := range members {
		if memberKey == exactKey {
			continue // already tried
		}
		raw, err := Client.Get(ctx, memberKey).Result()
		if err != nil {
			continue
		}
		var entry CachedEntry
		if json.Unmarshal([]byte(raw), &entry) != nil {
			continue
		}

		// Quick pre-filter: hamming distance on SimHash
		if hammingDistance(sh, entry.SimHash) > 10 {
			continue
		}

		// Verify with Jaccard similarity on token sets
		sim := jaccardSimilarity(tokens, entry.Tokens)
		if sim >= SimilarityThreshold {
			log.Printf("Cache HIT (semantic, similarity=%.2f)", sim)
			return &entry.Response, true
		}
	}

	return nil, false
}

// Store saves a response in the cache keyed by both exact hash and SimHash band.
func Store(ctx context.Context, model string, messages []models.Message, response models.OpenAIResponse) {
	if Client == nil {
		return
	}

	normalized := normalizeMessages(messages)
	tokens := tokenize(normalized)
	sh := simhash(tokens)
	exactKey := exactCacheKey(model, normalized)
	band := simhashBand(sh)
	bandKey := fmt.Sprintf("semcache:band:%s:%s", model, band)

	entry := CachedEntry{
		Response: response,
		Model:    model,
		Tokens:   tokens,
		SimHash:  sh,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	pipe := Client.Pipeline()
	pipe.Set(ctx, exactKey, data, CacheTTL)
	pipe.SAdd(ctx, bandKey, exactKey)
	pipe.Expire(ctx, bandKey, CacheTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("Cache store error: %v", err)
	}
}

// --- normalization ---

var whitespaceRe = regexp.MustCompile(`\s+`)
var punctuationRe = regexp.MustCompile(`[^\w\s]`)

func normalizeMessages(messages []models.Message) string {
	var b strings.Builder
	for _, msg := range messages {
		b.WriteString(normalizeText(msg.Role))
		b.WriteString(":")
		b.WriteString(normalizeText(msg.Content))
		b.WriteString("|")
	}
	return b.String()
}

func normalizeText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = whitespaceRe.ReplaceAllString(text, " ")
	return text
}

func exactCacheKey(model, normalized string) string {
	h := sha256.Sum256([]byte(model + ":" + normalized))
	return "semcache:exact:" + hex.EncodeToString(h[:])
}

// --- tokenization ---

func tokenize(text string) []string {
	cleaned := punctuationRe.ReplaceAllString(text, "")
	cleaned = whitespaceRe.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil
	}
	return strings.Split(cleaned, " ")
}

// --- SimHash (locality-sensitive hash) ---

func simhash(tokens []string) uint64 {
	var v [64]int

	for _, token := range tokens {
		h := fnv.New64a()
		h.Write([]byte(token))
		hash := h.Sum64()

		for i := 0; i < 64; i++ {
			if hash&(1<<uint(i)) != 0 {
				v[i]++
			} else {
				v[i]--
			}
		}
	}

	var sh uint64
	for i := 0; i < 64; i++ {
		if v[i] > 0 {
			sh |= 1 << uint(i)
		}
	}
	return sh
}

func simhashBand(sh uint64) string {
	// Use the top bits as the band key for LSH bucketing
	band := sh >> (64 - SimHashBandWidth)
	return fmt.Sprintf("%x", band)
}

func hammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// --- Jaccard similarity ---

func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	setA := make(map[string]struct{}, len(a))
	for _, t := range a {
		setA[t] = struct{}{}
	}

	setB := make(map[string]struct{}, len(b))
	for _, t := range b {
		setB[t] = struct{}{}
	}

	intersection := 0
	for t := range setA {
		if _, ok := setB[t]; ok {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}
