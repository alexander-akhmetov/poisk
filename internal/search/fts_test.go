package search

import (
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "camelCase",
			input: "getHTTPClient",
			want:  []string{"get", "http", "client", "gethttpclient"},
		},
		{
			name:  "snake_case",
			input: "my_var_name",
			want:  []string{"my", "var", "name", "my_var_name"},
		},
		{
			name:  "simple words",
			input: "hello world",
			want:  []string{"hello", "world"},
		},
		{
			name:  "mixed punctuation",
			input: "CFR-1234!",
			want:  []string{"cfr", "1234"},
		},
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "only punctuation",
			input: "!@#$%",
			want:  nil,
		},
		{
			name:  "PascalCase",
			input: "NewSearcher",
			want:  []string{"new", "searcher", "newsearcher"},
		},
		{
			name:  "snake with camel parts",
			input: "get_httpClient",
			want:  []string{"get", "http", "client", "get_httpclient"},
		},
		{
			name:  "single word",
			input: "single",
			want:  []string{"single"},
		},
		{
			name:  "duplicate across split",
			input: "test test",
			want:  []string{"test"},
		},
		{
			name:  "uppercase acronym alone",
			input: "HTTP",
			want:  []string{"http"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenize(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("tokenize(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitCamel(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"getHTTPClient", []string{"get", "HTTP", "Client"}},
		{"simpleWord", []string{"simple", "Word"}},
		{"lower", []string{"lower"}},
		{"HTTP", []string{"HTTP"}},
		{"XMLParser", []string{"XML", "Parser"}},
		{"", nil},
		{"A", []string{"A"}},
		{"getURL", []string{"get", "URL"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitCamel(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitCamel(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitCamel(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildStrictAND(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   string
	}{
		{"multiple", []string{"hello", "world"}, `"hello" AND "world"`},
		{"single", []string{"hello"}, `"hello"`},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildStrictAND(tt.tokens)
			if got != tt.want {
				t.Errorf("buildStrictAND(%v) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestBuildRelaxedOR(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   string
	}{
		{"multiple", []string{"hello", "world"}, `"hello" OR "world"`},
		{"single", []string{"hello"}, `"hello"`},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRelaxedOR(tt.tokens)
			if got != tt.want {
				t.Errorf("buildRelaxedOR(%v) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestBuildPrefixOR(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   string
	}{
		{"multiple", []string{"hel", "wor"}, `"hel"* OR "wor"*`},
		{"single", []string{"hel"}, `"hel"*`},
		{"empty", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPrefixOR(tt.tokens)
			if got != tt.want {
				t.Errorf("buildPrefixOR(%v) = %q, want %q", tt.tokens, got, tt.want)
			}
		})
	}
}

func TestBuildMetadataClause(t *testing.T) {
	tests := []struct {
		name    string
		filters MetadataFilters
		want    string
	}{
		{
			name: "all filters",
			filters: MetadataFilters{
				Languages: []string{"go"},
				Kinds:     []string{"function_declaration"},
				Symbols:   []string{"fetchuser"},
			},
			want: `language:"go" AND chunk_kind:"function_declaration" AND symbol:"fetchuser"`,
		},
		{
			name: "or within same field",
			filters: MetadataFilters{
				Languages: []string{"go", "rust"},
			},
			want: `(language:"go" OR language:"rust")`,
		},
		{
			name: "empty",
			filters: MetadataFilters{
				Languages: nil,
				Kinds:     nil,
				Symbols:   nil,
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMetadataClause(tt.filters)
			if got != tt.want {
				t.Fatalf("buildMetadataClause(%+v) = %q, want %q", tt.filters, got, tt.want)
			}
		})
	}
}

func TestCombineFTSQuery(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		metadata string
		want     string
	}{
		{"both", `"hello" AND "world"`, `language:"go"`, `"hello" AND "world" AND language:"go"`},
		{"text only", `"hello"`, "", `"hello"`},
		{"metadata only", "", `symbol:"open"`, `symbol:"open"`},
		{"neither", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := combineFTSQuery(tt.text, tt.metadata)
			if got != tt.want {
				t.Fatalf("combineFTSQuery(%q, %q) = %q, want %q", tt.text, tt.metadata, got, tt.want)
			}
		})
	}
}
