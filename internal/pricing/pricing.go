package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/beavend/beavend/internal/core"
)

//go:embed model_prices.json
var embeddedPricesJSON []byte

// ModelPrice defines rates in USD per 1 Million tokens, cached token discounts, and capability flags.
type ModelPrice struct {
	InputCostPer1M       float64  `json:"input_cost_per_1m"`
	OutputCostPer1M      float64  `json:"output_cost_per_1m"`
	CachedInputCostPer1M *float64 `json:"cached_input_cost_per_1m,omitempty"`
	PerImageCost         *float64 `json:"per_image_cost,omitempty"`
	PerAudioSecondCost   *float64 `json:"per_audio_second_cost,omitempty"`
	PerVideoSecondCost   *float64 `json:"per_video_second_cost,omitempty"`
	ContextWindow        int      `json:"context_window,omitempty"`
	SupportsTools        bool     `json:"supports_tools,omitempty"`
	SupportsCache        bool     `json:"supports_cache,omitempty"`
	SupportsReasoning    bool     `json:"supports_reasoning,omitempty"`
}

var (
	dateSuffixPattern = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$|-\d{8}$`)
	knownPrefixes     = []string{
		"openai/", "anthropic/", "google/", "google_vertex/", "vertex_ai/",
		"azure/", "bedrock/", "groq/", "openrouter/", "mistral/", "deepseek/",
		"together_ai/", "together/", "fireworks_ai/", "fireworks/", "cohere/",
	}
)

// Catalog manages embedded baseline pricing and dynamically synced pricing.
// It is thread-safe and safe for concurrent reads on the request hot path.
type Catalog struct {
	mu       sync.RWMutex
	embedded map[string]ModelPrice
	dynamic  map[string]ModelPrice
}

var (
	defaultCatalogInstance *Catalog
	defaultCatalogOnce     sync.Once
)

// DefaultCatalog returns the singleton pricing catalog initialized with embedded prices.
func DefaultCatalog() *Catalog {
	defaultCatalogOnce.Do(func() {
		defaultCatalogInstance = NewCatalog()
		if err := defaultCatalogInstance.loadEmbedded(); err != nil {
			// Fallback: catalog remains empty but initialized
			_ = err
		}
	})
	return defaultCatalogInstance
}

// NewCatalog constructs an empty Catalog.
func NewCatalog() *Catalog {
	return &Catalog{
		embedded: make(map[string]ModelPrice),
		dynamic:  make(map[string]ModelPrice),
	}
}

func (c *Catalog) loadEmbedded() error {
	var prices map[string]ModelPrice
	if err := json.Unmarshal(embeddedPricesJSON, &prices); err != nil {
		return fmt.Errorf("failed to unmarshal embedded model prices: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range prices {
		c.embedded[normalizeModelKey(k)] = v
	}
	return nil
}

// Lookup finds the pricing for a given model identifier.
// Dynamic updates take precedence over embedded baseline prices.
func (c *Catalog) Lookup(model string) (ModelPrice, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	norm := normalizeModelKey(model)
	if norm == "" {
		return ModelPrice{}, false
	}

	// 1. Check exact normalized match in dynamic prices
	if p, ok := c.dynamic[norm]; ok {
		return p, true
	}
	// 2. Check exact normalized match in embedded prices
	if p, ok := c.embedded[norm]; ok {
		return p, true
	}

	// 3. Strip date suffix (e.g. gpt-4o-2024-08-06 -> gpt-4o)
	base := stripDateSuffix(norm)
	if base != norm {
		if p, ok := c.dynamic[base]; ok {
			return p, true
		}
		if p, ok := c.embedded[base]; ok {
			return p, true
		}
	}

	// 4. Try stripping common instruct/chat suffixes
	if simplified := stripModelVariant(base); simplified != base {
		if p, ok := c.dynamic[simplified]; ok {
			return p, true
		}
		if p, ok := c.embedded[simplified]; ok {
			return p, true
		}
	}

	return ModelPrice{}, false
}

// Update replaces or adds dynamic model prices.
func (c *Catalog) Update(prices map[string]ModelPrice) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, v := range prices {
		norm := normalizeModelKey(k)
		if norm != "" {
			c.dynamic[norm] = v
		}
	}
}

// ResetDynamic clears dynamically synced prices, returning to the embedded baseline.
func (c *Catalog) ResetDynamic() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dynamic = make(map[string]ModelPrice)
}

// CalculateCostWithSource computes total estimated USD cost and returns the pricing attribution source.
// It accounts for cached input token rates when cachedInputTokens > 0.
func (c *Catalog) CalculateCostWithSource(inputTokens, outputTokens, cachedInputTokens int, model string, override *core.ModelPrice) (float64, string) {
	var (
		inputRate  float64
		outputRate float64
		cachedRate *float64
		source     string
	)

	if override != nil && (override.InputCostPer1M > 0 || override.OutputCostPer1M > 0) {
		inputRate = override.InputCostPer1M
		outputRate = override.OutputCostPer1M
		cachedRate = override.CachedInputCostPer1M
		source = "override"
	} else if p, found := c.Lookup(model); found {
		inputRate = p.InputCostPer1M
		outputRate = p.OutputCostPer1M
		cachedRate = p.CachedInputCostPer1M
		c.mu.RLock()
		norm := normalizeModelKey(model)
		if _, isDyn := c.dynamic[norm]; isDyn {
			source = "catalog_dynamic"
		} else {
			source = "catalog_embedded"
		}
		c.mu.RUnlock()
	} else {
		return 0.0, "none"
	}

	nonCachedInput := inputTokens
	effectiveCached := 0
	if cachedInputTokens > 0 {
		if cachedInputTokens >= inputTokens {
			effectiveCached = inputTokens
			nonCachedInput = 0
		} else {
			effectiveCached = cachedInputTokens
			nonCachedInput = inputTokens - cachedInputTokens
		}
	}

	actualCachedRate := inputRate * 0.5 // Default 50% discount if not explicitly set
	if cachedRate != nil {
		actualCachedRate = *cachedRate
	}

	cost := (float64(nonCachedInput)*inputRate + float64(effectiveCached)*actualCachedRate + float64(outputTokens)*outputRate) / 1_000_000.0
	if cost < 0 {
		return 0.0, source
	}
	return cost, source
}

// CalculateCost computes total estimated USD cost for token consumption.
// If an explicit override is provided (e.g. from ModelRouting), it is used.
func (c *Catalog) CalculateCost(inputTokens, outputTokens int, model string, override *core.ModelPrice) float64 {
	cost, _ := c.CalculateCostWithSource(inputTokens, outputTokens, 0, model, override)
	return cost
}

// CalculateMediaCost computes estimated USD cost for generated media (images, audio seconds, video seconds).
func (c *Catalog) CalculateMediaCost(images int, audioSec float64, videoSec float64, model string, override *core.ModelPrice) float64 {
	var (
		imgRate   float64
		audioRate float64
		videoRate float64
	)

	if override != nil {
		if override.PerImageCost != nil {
			imgRate = *override.PerImageCost
		}
		if override.PerAudioSecondCost != nil {
			audioRate = *override.PerAudioSecondCost
		}
		if override.PerVideoSecondCost != nil {
			videoRate = *override.PerVideoSecondCost
		}
	} else if p, found := c.Lookup(model); found {
		if p.PerImageCost != nil {
			imgRate = *p.PerImageCost
		}
		if p.PerAudioSecondCost != nil {
			audioRate = *p.PerAudioSecondCost
		}
		if p.PerVideoSecondCost != nil {
			videoRate = *p.PerVideoSecondCost
		}
	}

	// Default fallback baseline estimates if not set in model prices:
	// e.g. DALL-E 3 ~$0.04/image, TTS ~$0.00025/sec, Video ~$0.05/sec
	if imgRate == 0 && images > 0 {
		imgRate = 0.04
	}
	if audioRate == 0 && audioSec > 0 {
		audioRate = 0.00025
	}
	if videoRate == 0 && videoSec > 0 {
		videoRate = 0.05
	}

	total := float64(images)*imgRate + audioSec*audioRate + videoSec*videoRate
	if total < 0 {
		return 0.0
	}
	return total
}

// CalculateCost is a package-level helper that delegates to DefaultCatalog.
func CalculateCost(inputTokens, outputTokens int, model string, override *core.ModelPrice) float64 {
	return DefaultCatalog().CalculateCost(inputTokens, outputTokens, model, override)
}

// CalculateCostWithSource is a package-level helper that delegates to DefaultCatalog.
func CalculateCostWithSource(inputTokens, outputTokens, cachedInputTokens int, model string, override *core.ModelPrice) (float64, string) {
	return DefaultCatalog().CalculateCostWithSource(inputTokens, outputTokens, cachedInputTokens, model, override)
}

// CalculateMediaCost is a package-level helper that delegates to DefaultCatalog.
func CalculateMediaCost(images int, audioSec, videoSec float64, model string, override *core.ModelPrice) float64 {
	return DefaultCatalog().CalculateMediaCost(images, audioSec, videoSec, model, override)
}

// ParseCatalogData parses JSON data from LiteLLM, OpenRouter, or native Beavend formats.
func ParseCatalogData(r io.Reader) (map[string]ModelPrice, error) {
	var rawData map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&rawData); err != nil {
		return nil, fmt.Errorf("decode pricing json: %w", err)
	}

	results := make(map[string]ModelPrice)

	// Check if this is OpenRouter format: { "data": [ { "id": "...", "pricing": { "prompt": "...", "completion": "..." } } ] }
	if dataRaw, ok := rawData["data"]; ok {
		var openRouterModels []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string  `json:"prompt"`
				Completion string  `json:"completion"`
				Image      *string `json:"image"`
			} `json:"pricing"`
		}
		if err := json.Unmarshal(dataRaw, &openRouterModels); err == nil && len(openRouterModels) > 0 {
			for _, m := range openRouterModels {
				var promptRate, compRate float64
				fmt.Sscanf(m.Pricing.Prompt, "%f", &promptRate)
				fmt.Sscanf(m.Pricing.Completion, "%f", &compRate)
				price := ModelPrice{
					InputCostPer1M:  promptRate * 1_000_000.0,
					OutputCostPer1M: compRate * 1_000_000.0,
				}
				if m.Pricing.Image != nil {
					var imgRate float64
					fmt.Sscanf(*m.Pricing.Image, "%f", &imgRate)
					if imgRate > 0 {
						price.PerImageCost = &imgRate
					}
				}
				results[m.ID] = price
			}
			return results, nil
		}
	}

	// Iterate each top-level key (LiteLLM or native Beavend format)
	for modelName, raw := range rawData {
		if modelName == "sample_spec" {
			continue
		}

		// Try Beavend native: { "input_cost_per_1m": 2.5, "output_cost_per_1m": 10.0, "per_image_cost": 0.04 }
		var native ModelPrice
		if err := json.Unmarshal(raw, &native); err == nil && (native.InputCostPer1M > 0 || native.OutputCostPer1M > 0 || native.PerImageCost != nil || native.PerAudioSecondCost != nil || native.PerVideoSecondCost != nil) {
			results[modelName] = native
			continue
		}

		// Try LiteLLM format: { "input_cost_per_token": 0.0000025, "output_cost_per_token": 0.00001, "input_cost_per_image": 0.04 }
		var litellmFormat struct {
			InputCostPerToken  *float64 `json:"input_cost_per_token"`
			OutputCostPerToken *float64 `json:"output_cost_per_token"`
			InputCostPerImage  *float64 `json:"input_cost_per_image"`
			InputCostPerSecond *float64 `json:"input_cost_per_second"`
		}
		if err := json.Unmarshal(raw, &litellmFormat); err == nil && (litellmFormat.InputCostPerToken != nil || litellmFormat.OutputCostPerToken != nil || litellmFormat.InputCostPerImage != nil || litellmFormat.InputCostPerSecond != nil) {
			var inRate, outRate float64
			if litellmFormat.InputCostPerToken != nil {
				inRate = *litellmFormat.InputCostPerToken * 1_000_000.0
			}
			if litellmFormat.OutputCostPerToken != nil {
				outRate = *litellmFormat.OutputCostPerToken * 1_000_000.0
			}
			price := ModelPrice{
				InputCostPer1M:     inRate,
				OutputCostPer1M:    outRate,
				PerImageCost:       litellmFormat.InputCostPerImage,
				PerAudioSecondCost: litellmFormat.InputCostPerSecond,
			}
			results[modelName] = price
		}
	}

	return results, nil
}

func normalizeModelKey(model string) string {
	s := strings.ToLower(strings.TrimSpace(model))
	for _, p := range knownPrefixes {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			break
		}
	}
	// Also strip leading/trailing slashes if any
	s = strings.Trim(s, "/")
	return s
}

func stripDateSuffix(model string) string {
	return dateSuffixPattern.ReplaceAllString(model, "")
}

func stripModelVariant(model string) string {
	for _, suffix := range []string{"-instruct", "-chat", "-it"} {
		if strings.HasSuffix(model, suffix) {
			return strings.TrimSuffix(model, suffix)
		}
	}
	return model
}
