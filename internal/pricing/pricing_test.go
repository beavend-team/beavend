package pricing

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/beavend/beavend/internal/core"
)

func TestCatalog_EmbeddedLookup(t *testing.T) {
	cat := DefaultCatalog()

	tests := []struct {
		model          string
		wantFound      bool
		wantInputRate  float64
		wantOutputRate float64
	}{
		{
			model:          "gpt-4o",
			wantFound:      true,
			wantInputRate:  2.50,
			wantOutputRate: 10.00,
		},
		{
			model:          "openai/gpt-4o",
			wantFound:      true,
			wantInputRate:  2.50,
			wantOutputRate: 10.00,
		},
		{
			model:          "gpt-4o-2024-08-06",
			wantFound:      true,
			wantInputRate:  2.50,
			wantOutputRate: 10.00,
		},
		{
			model:          "gpt-4o-mini",
			wantFound:      true,
			wantInputRate:  0.15,
			wantOutputRate: 0.60,
		},
		{
			model:          "anthropic/claude-3-5-sonnet",
			wantFound:      true,
			wantInputRate:  3.00,
			wantOutputRate: 15.00,
		},
		{
			model:          "claude-3-5-sonnet-20241022",
			wantFound:      true,
			wantInputRate:  3.00,
			wantOutputRate: 15.00,
		},
		{
			model:          "gemini-2.5-flash",
			wantFound:      true,
			wantInputRate:  0.10,
			wantOutputRate: 0.40,
		},
		{
			model:          "deepseek/deepseek-chat",
			wantFound:      true,
			wantInputRate:  0.14,
			wantOutputRate: 0.28,
		},
		{
			model:          "non-existent-fake-model-xyz",
			wantFound:      false,
			wantInputRate:  0,
			wantOutputRate: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p, found := cat.Lookup(tt.model)
			if found != tt.wantFound {
				t.Fatalf("Lookup(%q) found=%v, want %v", tt.model, found, tt.wantFound)
			}
			if found {
				if math.Abs(p.InputCostPer1M-tt.wantInputRate) > 0.0001 {
					t.Errorf("InputCostPer1M got %f, want %f", p.InputCostPer1M, tt.wantInputRate)
				}
				if math.Abs(p.OutputCostPer1M-tt.wantOutputRate) > 0.0001 {
					t.Errorf("OutputCostPer1M got %f, want %f", p.OutputCostPer1M, tt.wantOutputRate)
				}
			}
		})
	}
}

func TestCatalog_CalculateCost(t *testing.T) {
	cat := DefaultCatalog()

	// gpt-4o: $2.50 / 1M in, $10.00 / 1M out
	// 1000 input tokens = 0.0025
	// 500 output tokens = 0.0050
	// total = 0.0075
	cost := cat.CalculateCost(1000, 500, "gpt-4o", nil)
	expected := 0.0075
	if math.Abs(cost-expected) > 0.000001 {
		t.Fatalf("expected cost %f, got %f", expected, cost)
	}

	// Test with explicit override
	override := &core.ModelPrice{
		InputCostPer1M:  1.00,
		OutputCostPer1M: 2.00,
	}
	// 1000 in = 0.001
	// 500 out = 0.001
	// total = 0.002
	overrideCost := cat.CalculateCost(1000, 500, "gpt-4o", override)
	if math.Abs(overrideCost-0.002) > 0.000001 {
		t.Fatalf("expected override cost 0.002, got %f", overrideCost)
	}

	// Unknown model without override returns 0
	unknownCost := cat.CalculateCost(1000, 500, "unknown-model", nil)
	if unknownCost != 0.0 {
		t.Fatalf("expected 0 for unknown model, got %f", unknownCost)
	}
}

func TestCatalog_DynamicUpdate(t *testing.T) {
	cat := NewCatalog()
	cat.Update(map[string]ModelPrice{
		"custom-finetuned": {
			InputCostPer1M:  5.0,
			OutputCostPer1M: 20.0,
		},
	})

	p, found := cat.Lookup("custom-finetuned")
	if !found {
		t.Fatal("expected custom-finetuned to be found")
	}
	if p.InputCostPer1M != 5.0 || p.OutputCostPer1M != 20.0 {
		t.Fatalf("unexpected rates: %+v", p)
	}

	cat.ResetDynamic()
	_, found = cat.Lookup("custom-finetuned")
	if found {
		t.Fatal("expected custom-finetuned to be cleared after reset")
	}
}

func TestParseCatalogData_LiteLLMFormat(t *testing.T) {
	raw := `{
		"test-litellm-model": {
			"input_cost_per_token": 0.000003,
			"output_cost_per_token": 0.000015
		}
	}`

	prices, err := ParseCatalogData(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p, ok := prices["test-litellm-model"]
	if !ok {
		t.Fatal("missing test-litellm-model")
	}
	if math.Abs(p.InputCostPer1M-3.0) > 0.001 {
		t.Errorf("expected input 3.0, got %f", p.InputCostPer1M)
	}
	if math.Abs(p.OutputCostPer1M-15.0) > 0.001 {
		t.Errorf("expected output 15.0, got %f", p.OutputCostPer1M)
	}
}

func TestParseCatalogData_OpenRouterFormat(t *testing.T) {
	raw := `{
		"data": [
			{
				"id": "openrouter/sample-model",
				"pricing": {
					"prompt": "0.000002",
					"completion": "0.000008"
				}
			}
		]
	}`

	prices, err := ParseCatalogData(bytes.NewReader([]byte(raw)))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	p, ok := prices["openrouter/sample-model"]
	if !ok {
		t.Fatal("missing openrouter/sample-model")
	}
	if math.Abs(p.InputCostPer1M-2.0) > 0.001 {
		t.Errorf("expected input 2.0, got %f", p.InputCostPer1M)
	}
	if math.Abs(p.OutputCostPer1M-8.0) > 0.001 {
		t.Errorf("expected output 8.0, got %f", p.OutputCostPer1M)
	}
}

func TestCatalog_CalculateCostWithSource_CachedTokens(t *testing.T) {
	cat := DefaultCatalog()

	t.Run("embedded model with cached input discount", func(t *testing.T) {
		// gpt-4o: input 2.50, output 10.00. Default cached rate = 1.25 (50%)
		// 1000 input tokens total, 400 cached (so 600 non-cached @ 2.50, 400 cached @ 1.25)
		// 500 output tokens @ 10.00
		// Cost = (600*2.50 + 400*1.25 + 500*10.00) / 1,000,000 = (1500 + 500 + 5000) / 1M = 7000 / 1M = 0.0070
		cost, source := cat.CalculateCostWithSource(1000, 500, 400, "gpt-4o", nil)
		if source != "catalog_embedded" {
			t.Errorf("expected source 'catalog_embedded', got %q", source)
		}
		expectedCost := 0.0070
		if math.Abs(cost-expectedCost) > 1e-6 {
			t.Errorf("expected cost %f, got %f", expectedCost, cost)
		}
	})

	t.Run("override with custom cached input rate", func(t *testing.T) {
		cachedRate := 0.50
		override := &core.ModelPrice{
			InputCostPer1M:       2.00,
			OutputCostPer1M:      8.00,
			CachedInputCostPer1M: &cachedRate,
			ContextWindow:        128000,
			SupportsCache:        true,
			SupportsTools:        true,
			SupportsReasoning:    true,
		}
		// 1,000,000 input tokens, 500,000 cached:
		// nonCached = 500k @ 2.00 = $1.00
		// cached = 500k @ 0.50 = $0.25
		// output = 100k @ 8.00 = $0.80
		// Total = 1.00 + 0.25 + 0.80 = $2.05
		cost, source := cat.CalculateCostWithSource(1000000, 100000, 500000, "custom-model", override)
		if source != "override" {
			t.Errorf("expected source 'override', got %q", source)
		}
		expectedCost := 2.05
		if math.Abs(cost-expectedCost) > 1e-4 {
			t.Errorf("expected cost %f, got %f", expectedCost, cost)
		}
	})

	t.Run("unknown model returns source none", func(t *testing.T) {
		cost, source := cat.CalculateCostWithSource(100, 100, 0, "nonexistent-model-xyz", nil)
		if source != "none" {
			t.Errorf("expected source 'none', got %q", source)
		}
		if cost != 0.0 {
			t.Errorf("expected cost 0.0, got %f", cost)
		}
	})
}
