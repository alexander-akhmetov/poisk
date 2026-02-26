package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

type Config struct {
	Embedding EmbeddingConfig `toml:"embedding"`
	LLM       LLMConfig       `toml:"llm"`
	Search    SearchConfig    `toml:"search"`
	Index     IndexConfig     `toml:"index"`
	Folders   []FolderConfig  `toml:"folders"`
}

type LLMConfig struct {
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
	Model   string `toml:"model"`
}

type EmbeddingConfig struct {
	BaseURL        string `toml:"base_url"`
	APIKey         string `toml:"api_key"`
	Model          string `toml:"model"`
	Dimensions     int    `toml:"dimensions"`
	BatchSize      int    `toml:"batch_size"`
	SendDimensions bool   `toml:"send_dimensions"` // send dimensions param in API request
}

type SearchConfig struct {
	RRFk                int     `toml:"rrf_k"` // RRF constant, default 60
	SimilarityThreshold float64 `toml:"similarity_threshold"`
	DefaultTopK         int     `toml:"default_top_k"`
	VecWeight           float64 `toml:"vec_weight"`
	FTSWeight           float64 `toml:"fts_weight"`
	OriginalQueryWeight float64 `toml:"original_query_weight"`
	ExpandedQueryWeight float64 `toml:"expanded_query_weight"`
	QueryExpansion      bool    `toml:"query_expansion"`
	Rerank              bool    `toml:"rerank"`
	RerankTopN          int     `toml:"rerank_top_n"`
	RerankTopWeight     float64 `toml:"rerank_retrieval_weight_top"`
	RerankBottomWeight  float64 `toml:"rerank_retrieval_weight_bottom"`
}

type IndexConfig struct {
	ExcludePatterns []string `toml:"exclude_patterns"`
	IncludePatterns []string `toml:"include_patterns"`
	MaxFileSizeKB   int      `toml:"max_file_size_kb"`
}

type FolderConfig struct {
	Path            string            `toml:"path"`
	Description     string            `toml:"description"`
	Context         map[string]string `toml:"context"`
	ExcludePatterns []string          `toml:"exclude_patterns"`
	IncludePatterns []string          `toml:"include_patterns"`
}

// EffectiveExcludePatterns returns folder-level patterns if set, otherwise global.
// nil = not configured (use global), empty slice = explicitly no excludes.
func (f *FolderConfig) EffectiveExcludePatterns(global []string) []string {
	if f.ExcludePatterns != nil {
		return f.ExcludePatterns
	}
	return global
}

// EffectiveIncludePatterns returns folder-level patterns if set, otherwise global.
func (f *FolderConfig) EffectiveIncludePatterns(global []string) []string {
	if f.IncludePatterns != nil {
		return f.IncludePatterns
	}
	return global
}

func DefaultConfig() Config {
	return Config{
		Embedding: EmbeddingConfig{
			BaseURL:    "http://localhost:11434/v1",
			Model:      "nomic-embed-text",
			Dimensions: 768,
			BatchSize:  50,
		},
		Search: SearchConfig{
			RRFk:                60,
			SimilarityThreshold: 0.3,
			DefaultTopK:         20,
			VecWeight:           1.0,
			FTSWeight:           1.1,
			OriginalQueryWeight: 1.0,
			ExpandedQueryWeight: 0.25,
			Rerank:              true,
			RerankTopN:          20,
			RerankTopWeight:     0.8,
			RerankBottomWeight:  0.2,
		},
		Index: IndexConfig{
			ExcludePatterns: []string{".git", "node_modules", "vendor", "__pycache__", ".venv"},
			MaxFileSizeKB:   512,
		},
	}
}

func configPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "poisk", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "poisk", "config.toml")
}

func dbPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "poisk", "poisk.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "poisk", "poisk.db")
}

func DBPath() string {
	return dbPath()
}

func Load() (*Config, error) {
	cfg := DefaultConfig()
	path := configPath()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	// Expand ~ in folder paths
	home, _ := os.UserHomeDir()
	for i := range cfg.Folders {
		cfg.Folders[i].Path = expandHome(cfg.Folders[i].Path, home)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Embedding.Dimensions <= 0 {
		return fmt.Errorf("embedding.dimensions must be > 0")
	}
	if c.Embedding.BatchSize <= 0 {
		return fmt.Errorf("embedding.batch_size must be > 0")
	}
	if c.Search.DefaultTopK <= 0 {
		return fmt.Errorf("search.default_top_k must be > 0")
	}
	if c.Search.RRFk < 0 {
		return fmt.Errorf("search.rrf_k must be >= 0, got %v", c.Search.RRFk)
	}
	if c.Search.SimilarityThreshold < 0 || c.Search.SimilarityThreshold > 1 {
		return fmt.Errorf("search.similarity_threshold must be between 0 and 1, got %v", c.Search.SimilarityThreshold)
	}
	if c.Search.VecWeight <= 0 || math.IsNaN(c.Search.VecWeight) || math.IsInf(c.Search.VecWeight, 0) {
		return fmt.Errorf("search.vec_weight must be > 0, got %v", c.Search.VecWeight)
	}
	if c.Search.FTSWeight <= 0 || math.IsNaN(c.Search.FTSWeight) || math.IsInf(c.Search.FTSWeight, 0) {
		return fmt.Errorf("search.fts_weight must be > 0, got %v", c.Search.FTSWeight)
	}
	if c.Search.OriginalQueryWeight <= 0 || math.IsNaN(c.Search.OriginalQueryWeight) || math.IsInf(c.Search.OriginalQueryWeight, 0) {
		return fmt.Errorf("search.original_query_weight must be > 0, got %v", c.Search.OriginalQueryWeight)
	}
	if c.Search.ExpandedQueryWeight <= 0 || math.IsNaN(c.Search.ExpandedQueryWeight) || math.IsInf(c.Search.ExpandedQueryWeight, 0) {
		return fmt.Errorf("search.expanded_query_weight must be > 0, got %v", c.Search.ExpandedQueryWeight)
	}
	if c.Search.RerankTopN <= 0 {
		return fmt.Errorf("search.rerank_top_n must be > 0, got %v", c.Search.RerankTopN)
	}
	if math.IsNaN(c.Search.RerankTopWeight) || math.IsInf(c.Search.RerankTopWeight, 0) || c.Search.RerankTopWeight < 0 || c.Search.RerankTopWeight > 1 {
		return fmt.Errorf("search.rerank_retrieval_weight_top must be between 0 and 1, got %v", c.Search.RerankTopWeight)
	}
	if math.IsNaN(c.Search.RerankBottomWeight) || math.IsInf(c.Search.RerankBottomWeight, 0) || c.Search.RerankBottomWeight < 0 || c.Search.RerankBottomWeight > 1 {
		return fmt.Errorf("search.rerank_retrieval_weight_bottom must be between 0 and 1, got %v", c.Search.RerankBottomWeight)
	}
	if c.Search.RerankTopWeight < c.Search.RerankBottomWeight {
		return fmt.Errorf("search.rerank_retrieval_weight_top must be >= search.rerank_retrieval_weight_bottom, got %v < %v", c.Search.RerankTopWeight, c.Search.RerankBottomWeight)
	}
	return nil
}
