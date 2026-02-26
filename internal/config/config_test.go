package config

import (
	"math"
	"os"
	"path/filepath"
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
	if !cfg.Search.Rerank {
		t.Fatalf("default rerank should be enabled")
	}
	if cfg.Search.RerankTopN <= 0 {
		t.Fatalf("default rerank_top_n must be positive, got %d", cfg.Search.RerankTopN)
	}
	if cfg.Search.RerankTopWeight < cfg.Search.RerankBottomWeight {
		t.Fatalf("default rerank blend should favor higher retrieval weight at top: top=%v bottom=%v", cfg.Search.RerankTopWeight, cfg.Search.RerankBottomWeight)
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
		{
			name: "rerank topn <= 0",
			edit: func(c *Config) { c.Search.RerankTopN = 0 },
			want: "search.rerank_top_n",
		},
		{
			name: "rerank top weight out of range",
			edit: func(c *Config) { c.Search.RerankTopWeight = 1.2 },
			want: "search.rerank_retrieval_weight_top",
		},
		{
			name: "rerank bottom weight out of range",
			edit: func(c *Config) { c.Search.RerankBottomWeight = -0.1 },
			want: "search.rerank_retrieval_weight_bottom",
		},
		{
			name: "rerank top below bottom",
			edit: func(c *Config) {
				c.Search.RerankTopWeight = 0.2
				c.Search.RerankBottomWeight = 0.8
			},
			want: "search.rerank_retrieval_weight_top",
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

func TestExpandHome(t *testing.T) {
	home := "/home/testuser"
	tests := []struct {
		name string
		path string
		want string
	}{
		{"tilde only", "~", "/home/testuser"},
		{"tilde prefix", "~/projects", "/home/testuser/projects"},
		{"absolute", "/usr/local", "/usr/local"},
		{"relative", "relative/path", "relative/path"},
		{"tilde other user", "~other/path", "~other/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandHome(tt.path, home)
			if got != tt.want {
				t.Errorf("expandHome(%q, %q) = %q, want %q", tt.path, home, got, tt.want)
			}
		})
	}
}

func TestConfigPath(t *testing.T) {
	t.Run("XDG_CONFIG_HOME set", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmp)
		got := configPath()
		want := filepath.Join(tmp, "poisk", "config.toml")
		if got != want {
			t.Errorf("configPath() = %q, want %q", got, want)
		}
	})

	t.Run("XDG_CONFIG_HOME unset", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := configPath()
		if !strings.HasSuffix(got, filepath.Join(".config", "poisk", "config.toml")) {
			t.Errorf("configPath() = %q, expected suffix .config/poisk/config.toml", got)
		}
	})
}

func TestDBPath(t *testing.T) {
	t.Run("XDG_DATA_HOME set", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmp)
		got := DBPath()
		want := filepath.Join(tmp, "poisk", "poisk.db")
		if got != want {
			t.Errorf("DBPath() = %q, want %q", got, want)
		}
	})

	t.Run("XDG_DATA_HOME unset", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", "")
		got := DBPath()
		if !strings.HasSuffix(got, filepath.Join(".local", "share", "poisk", "poisk.db")) {
			t.Errorf("DBPath() = %q, expected suffix .local/share/poisk/poisk.db", got)
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("missing file returns defaults", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmp)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		def := DefaultConfig()
		if cfg.Embedding.Model != def.Embedding.Model {
			t.Errorf("model = %q, want default %q", cfg.Embedding.Model, def.Embedding.Model)
		}
	})

	t.Run("valid TOML", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmp)

		dir := filepath.Join(tmp, "poisk")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := `
[embedding]
base_url = "http://localhost:9999"
model = "custom-model"
dimensions = 128
batch_size = 10

[[folders]]
path = "/tmp/test"
description = "test folder"
`
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Embedding.Model != "custom-model" {
			t.Errorf("model = %q, want %q", cfg.Embedding.Model, "custom-model")
		}
		if cfg.Embedding.Dimensions != 128 {
			t.Errorf("dimensions = %d, want 128", cfg.Embedding.Dimensions)
		}
		if len(cfg.Folders) != 1 || cfg.Folders[0].Path != "/tmp/test" {
			t.Errorf("folders = %v, want [{/tmp/test}]", cfg.Folders)
		}
	})

	t.Run("invalid TOML", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmp)

		dir := filepath.Join(tmp, "poisk")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("{{invalid toml"), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := Load()
		if err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(err.Error(), "parse config") {
			t.Errorf("error = %q, want 'parse config' substring", err.Error())
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmp)

		dir := filepath.Join(tmp, "poisk")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := `
[embedding]
dimensions = 0
batch_size = 10
`
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := Load()
		if err == nil {
			t.Fatal("expected validation error")
		}
		if !strings.Contains(err.Error(), "config validation") {
			t.Errorf("error = %q, want 'config validation' substring", err.Error())
		}
	})
}

func TestLoadFolderPatternOverrides(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "poisk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
[index]
exclude_patterns = [".git", "node_modules"]
include_patterns = ["*.go", "*.md"]

[[folders]]
path = "/tmp/with-overrides"
description = "overrides both"
exclude_patterns = ["vendor"]
include_patterns = ["*.rs"]

[[folders]]
path = "/tmp/no-overrides"
description = "uses globals"

[[folders]]
path = "/tmp/empty-overrides"
description = "explicitly empty"
exclude_patterns = []
include_patterns = []
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	globalExclude := cfg.Index.ExcludePatterns
	globalInclude := cfg.Index.IncludePatterns

	// Folder with overrides uses its own patterns
	f0 := cfg.Folders[0]
	if got := f0.EffectiveExcludePatterns(globalExclude); len(got) != 1 || got[0] != "vendor" {
		t.Errorf("folder[0] exclude = %v, want [vendor]", got)
	}
	if got := f0.EffectiveIncludePatterns(globalInclude); len(got) != 1 || got[0] != "*.rs" {
		t.Errorf("folder[0] include = %v, want [*.rs]", got)
	}

	// Folder without overrides falls back to global
	f1 := cfg.Folders[1]
	if got := f1.EffectiveExcludePatterns(globalExclude); len(got) != 2 {
		t.Errorf("folder[1] exclude = %v, want global %v", got, globalExclude)
	}
	if got := f1.EffectiveIncludePatterns(globalInclude); len(got) != 2 {
		t.Errorf("folder[1] include = %v, want global %v", got, globalInclude)
	}

	// Folder with explicit empty overrides gets empty (not global)
	f2 := cfg.Folders[2]
	if got := f2.EffectiveExcludePatterns(globalExclude); len(got) != 0 {
		t.Errorf("folder[2] exclude = %v, want empty", got)
	}
	if got := f2.EffectiveIncludePatterns(globalInclude); len(got) != 0 {
		t.Errorf("folder[2] include = %v, want empty", got)
	}
}

func TestLoadTildeExpansion(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "poisk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
[[folders]]
path = "~/projects/test"
description = "test"
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Folders) == 0 {
		t.Fatal("expected at least one folder")
	}
	if strings.HasPrefix(cfg.Folders[0].Path, "~") {
		t.Errorf("tilde not expanded: %q", cfg.Folders[0].Path)
	}
}
