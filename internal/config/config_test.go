package config

import (
	"math"
	"strings"
	"testing"
)

func TestDefaultConfigHasFusionWeights(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.validate(); err != nil {
		t.Fatalf("default config should validate, got error: %v", err)
	}
	if cfg.Search.VecWeight <= 0 || cfg.Search.FTSWeight <= 0 {
		t.Fatalf("modality weights must be positive: vec=%v fts=%v", cfg.Search.VecWeight, cfg.Search.FTSWeight)
	}
	if cfg.Search.OriginalQueryWeight <= 0 || cfg.Search.ExpandedQueryWeight <= 0 {
		t.Fatalf("query source weights must be positive: original=%v expanded=%v", cfg.Search.OriginalQueryWeight, cfg.Search.ExpandedQueryWeight)
	}
	if cfg.Search.ExpandedQueryWeight >= cfg.Search.OriginalQueryWeight {
		t.Fatalf("default expanded query weight should be lower than original: original=%v expanded=%v", cfg.Search.OriginalQueryWeight, cfg.Search.ExpandedQueryWeight)
	}
}

func TestValidateRejectsInvalidFusionWeights(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{
			name: "vec weight <= 0",
			edit: func(c *Config) { c.Search.VecWeight = 0 },
			want: "search.vec_weight",
		},
		{
			name: "fts weight <= 0",
			edit: func(c *Config) { c.Search.FTSWeight = -1 },
			want: "search.fts_weight",
		},
		{
			name: "original query weight <= 0",
			edit: func(c *Config) { c.Search.OriginalQueryWeight = 0 },
			want: "search.original_query_weight",
		},
		{
			name: "expanded query weight <= 0",
			edit: func(c *Config) { c.Search.ExpandedQueryWeight = 0 },
			want: "search.expanded_query_weight",
		},
		{
			name: "vec weight nan",
			edit: func(c *Config) { c.Search.VecWeight = math.NaN() },
			want: "search.vec_weight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.edit(&cfg)
			err := cfg.validate()
			if err == nil {
				t.Fatalf("expected validation error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}
