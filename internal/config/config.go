package config

import (
	"fmt"
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
	Search    SearchConfig    `toml:"search"`
	Index     IndexConfig     `toml:"index"`
	Folders   []FolderConfig  `toml:"folders"`
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
	VectorWeight        float64 `toml:"vector_weight"`  // deprecated: kept for config compat
	TextWeight          float64 `toml:"text_weight"`    // deprecated: kept for config compat
	RRFk                int     `toml:"rrf_k"`          // RRF constant, default 60
	SimilarityThreshold float64 `toml:"similarity_threshold"`
	DefaultTopK         int     `toml:"default_top_k"`
}

type IndexConfig struct {
	Extensions      []string `toml:"extensions"`       // deprecated: use Languages
	Languages       []string `toml:"languages"`
	ExcludePatterns []string `toml:"exclude_patterns"`
	MaxFileSizeKB   int      `toml:"max_file_size_kb"`
}

type FolderConfig struct {
	Path        string `toml:"path"`
	Description string `toml:"description"`
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
			VectorWeight:        0.7,
			TextWeight:          0.3,
			RRFk:                60,
			SimilarityThreshold: 0.3,
			DefaultTopK:         20,
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
			cfg.Index.Languages = []string{"go", "python", "rust", "javascript", "typescript"}
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

	// Default languages only when neither languages nor legacy extensions were set
	if len(cfg.Index.Languages) == 0 && len(cfg.Index.Extensions) == 0 {
		cfg.Index.Languages = []string{"go", "python", "rust", "javascript", "typescript"}
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
	return nil
}
