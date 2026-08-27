package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Server    ServerConfig    `toml:"server"`
	Folders   []FolderConfig  `toml:"folders"`
}

type ServerConfig struct {
	Listen string `toml:"listen"`
	Token  string `toml:"token"`
}

type LLMConfig struct {
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
	Model   string `toml:"model"`
	// ExtraBody is merged into every chat completion request body. Use it to
	// pass server-specific options poisk has no field for, most importantly
	// the switch that turns a reasoning model's thinking off.
	ExtraBody map[string]any `toml:"extra_body"`
}

type EmbeddingConfig struct {
	BaseURL        string `toml:"base_url"`
	APIKey         string `toml:"api_key"`
	Model          string `toml:"model"`
	Dimensions     int    `toml:"dimensions"`
	BatchSize      int    `toml:"batch_size"`
	SendDimensions bool   `toml:"send_dimensions"` // send dimensions param in API request
	Matryoshka     bool   `toml:"matryoshka"`      // truncate longer API vectors to dimensions and renormalize
	Quantization   string `toml:"quantization"`    // vector storage type: "int8" or "float32"
	// MaxInputBytes caps one embedding input. Every chunker's output is cut
	// down to it, so no single chunk can occupy the provider for minutes.
	MaxInputBytes int `toml:"max_input_bytes"`
	// BatchMaxBytes caps the summed raw text of one embedding request. It
	// counts chunk bytes, not the marshaled JSON body or provider tokens.
	BatchMaxBytes int `toml:"batch_max_bytes"`
}

// MaxInputBytesCeiling is the largest per-input limit the config accepts. A
// higher value would let configuration restore the oversized inputs these
// limits exist to prevent.
const MaxInputBytesCeiling = 8000

// MinInputBytes is the smallest per-input limit that still holds any UTF-8 rune.
const MinInputBytes = 4

type SearchConfig struct {
	RRFk                int           `toml:"rrf_k"` // RRF constant, default 60
	SimilarityThreshold float64       `toml:"similarity_threshold"`
	DefaultTopK         int           `toml:"default_top_k"`
	VecWeight           float64       `toml:"vec_weight"`
	FTSWeight           float64       `toml:"fts_weight"`
	OriginalQueryWeight float64       `toml:"original_query_weight"`
	ExpandedQueryWeight float64       `toml:"expanded_query_weight"`
	EmbeddingTimeout    time.Duration `toml:"embedding_timeout"`
	QueryExpansion      bool          `toml:"query_expansion"`
	Rerank              bool          `toml:"rerank"`
	RerankTopN          int           `toml:"rerank_top_n"`
	RerankTopWeight     float64       `toml:"rerank_retrieval_weight_top"`
	RerankBottomWeight  float64       `toml:"rerank_retrieval_weight_bottom"`
	MinScore            float64       `toml:"min_score"`
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
	MaxFileSizeKB   int               `toml:"max_file_size_kb"`
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

// EffectiveMaxFileSizeKB returns the folder-level cap if set (> 0), otherwise
// the global value. Session folders set a larger cap because pi session files
// routinely exceed the 512KB default.
func (f *FolderConfig) EffectiveMaxFileSizeKB(global int) int {
	if f.MaxFileSizeKB > 0 {
		return f.MaxFileSizeKB
	}
	return global
}

func DefaultConfig() Config {
	return Config{
		Embedding: EmbeddingConfig{
			BaseURL:        "http://localhost:11434/v1",
			Model:          "qwen3-embedding:4b",
			Dimensions:     1024,
			BatchSize:      50,
			SendDimensions: true,
			Quantization:   "int8",
			MaxInputBytes:  MaxInputBytesCeiling,
			BatchMaxBytes:  65536,
		},
		Search: SearchConfig{
			RRFk:                60,
			SimilarityThreshold: 0.3,
			DefaultTopK:         20,
			VecWeight:           1.0,
			FTSWeight:           1.1,
			OriginalQueryWeight: 1.0,
			ExpandedQueryWeight: 0.25,
			EmbeddingTimeout:    5 * time.Second,
			Rerank:              true,
			RerankTopN:          20,
			RerankTopWeight:     0.8,
			RerankBottomWeight:  0.2,
			MinScore:            0.005,
		},
		Index: IndexConfig{
			ExcludePatterns: []string{".git", "node_modules", "vendor", "__pycache__", ".venv"},
			MaxFileSizeKB:   512,
		},
		Server: ServerConfig{
			Listen: "127.0.0.1:8765",
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
	if c.Embedding.Quantization != "int8" && c.Embedding.Quantization != "float32" {
		return fmt.Errorf("embedding.quantization must be \"int8\" or \"float32\", got %q", c.Embedding.Quantization)
	}
	if c.Embedding.MaxInputBytes < MinInputBytes || c.Embedding.MaxInputBytes > MaxInputBytesCeiling {
		return fmt.Errorf("embedding.max_input_bytes must be between %d and %d, got %d",
			MinInputBytes, MaxInputBytesCeiling, c.Embedding.MaxInputBytes)
	}
	if c.Embedding.BatchMaxBytes < c.Embedding.MaxInputBytes {
		return fmt.Errorf("embedding.batch_max_bytes must be >= embedding.max_input_bytes, got %d < %d",
			c.Embedding.BatchMaxBytes, c.Embedding.MaxInputBytes)
	}
	return c.Search.validate()
}

func (s *SearchConfig) validate() error {
	if s.DefaultTopK <= 0 {
		return fmt.Errorf("search.default_top_k must be > 0")
	}
	if s.RRFk < 0 {
		return fmt.Errorf("search.rrf_k must be >= 0, got %v", s.RRFk)
	}
	if s.SimilarityThreshold < 0 || s.SimilarityThreshold > 1 {
		return fmt.Errorf("search.similarity_threshold must be between 0 and 1, got %v", s.SimilarityThreshold)
	}
	if err := validatePositiveFloat(s.VecWeight, "search.vec_weight"); err != nil {
		return err
	}
	if err := validatePositiveFloat(s.FTSWeight, "search.fts_weight"); err != nil {
		return err
	}
	if err := validatePositiveFloat(s.OriginalQueryWeight, "search.original_query_weight"); err != nil {
		return err
	}
	if err := validatePositiveFloat(s.ExpandedQueryWeight, "search.expanded_query_weight"); err != nil {
		return err
	}
	if s.EmbeddingTimeout <= 0 {
		return fmt.Errorf("search.embedding_timeout must be > 0, got %v", s.EmbeddingTimeout)
	}
	if s.RerankTopN <= 0 {
		return fmt.Errorf("search.rerank_top_n must be > 0, got %v", s.RerankTopN)
	}
	if err := validateWeight(s.RerankTopWeight, "search.rerank_retrieval_weight_top"); err != nil {
		return err
	}
	if err := validateWeight(s.RerankBottomWeight, "search.rerank_retrieval_weight_bottom"); err != nil {
		return err
	}
	if s.RerankTopWeight < s.RerankBottomWeight {
		return fmt.Errorf("search.rerank_retrieval_weight_top must be >= search.rerank_retrieval_weight_bottom, got %v < %v", s.RerankTopWeight, s.RerankBottomWeight)
	}
	if s.MinScore < 0 || math.IsNaN(s.MinScore) || math.IsInf(s.MinScore, 0) {
		return fmt.Errorf("search.min_score must be >= 0, got %v", s.MinScore)
	}
	return nil
}

func validatePositiveFloat(v float64, name string) error {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("%s must be > 0, got %v", name, v)
	}
	return nil
}

func validateWeight(v float64, name string) error {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
		return fmt.Errorf("%s must be between 0 and 1, got %v", name, v)
	}
	return nil
}
