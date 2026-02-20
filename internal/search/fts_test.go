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
