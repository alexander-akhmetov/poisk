package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Embedding EmbeddingConfig `toml:"embedding"`
	Search    SearchConfig    `toml:"search"`
	Index     IndexConfig     `toml:"index"`
	Folders   []FolderConfig  `toml:"folders"`
}

type EmbeddingConfig struct {
	BaseURL    string `toml:"base_url"`
	APIKey     string `toml:"api_key"`
	Model      string `toml:"model"`
	Dimensions int    `toml:"dimensions"`
	BatchSize  int    `toml:"batch_size"`
}

type SearchConfig struct {
	VectorWeight        float64 `toml:"vector_weight"`
	TextWeight          float64 `toml:"text_weight"`
	SimilarityThreshold float64 `toml:"similarity_threshold"`
	DefaultTopK         int     `toml:"default_top_k"`
}

type IndexConfig struct {
	Extensions      []string `toml:"extensions"`
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
			SimilarityThreshold: 0.3,
			DefaultTopK:         20,
		},
		Index: IndexConfig{
			Extensions:      []string{"go", "py", "rs", "js", "ts", "md", "txt", "org"},
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
	if c.Search.VectorWeight < 0 || c.Search.VectorWeight > 1 {
		return fmt.Errorf("search.vector_weight must be between 0 and 1, got %v", c.Search.VectorWeight)
	}
	if c.Search.TextWeight < 0 || c.Search.TextWeight > 1 {
		return fmt.Errorf("search.text_weight must be between 0 and 1, got %v", c.Search.TextWeight)
	}
	if c.Search.SimilarityThreshold < 0 || c.Search.SimilarityThreshold > 1 {
		return fmt.Errorf("search.similarity_threshold must be between 0 and 1, got %v", c.Search.SimilarityThreshold)
	}
	return nil
}
