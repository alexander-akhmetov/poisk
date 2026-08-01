package search

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/store"
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

func TestQueryFTSNullFolder(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nullfolder.db")
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}

	// Pre-existing rows can hold a NULL folder; insert one directly and index it.
	if _, err := s.DB().Exec(
		"INSERT INTO embeddings (source, file_path, line_num, chunk_text, folder, end_line, language, chunk_kind, symbol) VALUES ('src', 'a.go', 3, 'nullfolderprobe text', NULL, 7, 'go', 'function_declaration', 'probe')",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec("INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')"); err != nil {
		t.Fatal(err)
	}

	results, err := queryFTS(s, `"nullfolderprobe"`, 10, nil)
	if err != nil {
		t.Fatalf("queryFTS: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Folder != "" {
		t.Errorf("folder = %q, want empty string for NULL folder", r.Folder)
	}
	if r.LineNum != 3 || r.EndLine != 7 {
		t.Errorf("lines = %d-%d, want 3-7", r.LineNum, r.EndLine)
	}
	if r.Text != "nullfolderprobe text" {
		t.Errorf("text = %q, want %q", r.Text, "nullfolderprobe text")
	}
}

// seedFTSCorpus writes a corpus large enough to exceed every candidate fetch
// limit. Each entry matches "alpha"; the first bothCount also match "beta".
func seedFTSCorpus(t *testing.T, s *store.Store, total, bothCount int) {
	t.Helper()
	entries := make([]store.Entry, total)
	for i := range entries {
		text := fmt.Sprintf("alpha filler line number %d", i)
		if i < bothCount {
			text = fmt.Sprintf("alpha beta filler line number %d", i)
		}
		entries[i] = store.Entry{
			LineNum:   i + 1,
			EndLine:   i + 1,
			Text:      text,
			Embedding: []float32{1, 0, 0},
			Folder:    "src",
			Language:  "go",
		}
	}
	if err := s.InsertEntries("src", "corpus.go", entries); err != nil {
		t.Fatalf("seed corpus: %v", err)
	}
}

func openFTSCorpusStore(t *testing.T, total, bothCount int) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "fts-limits.db")
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.FTSAvailable() {
		t.Skip("FTS5 not available")
	}
	seedFTSCorpus(t, s, total, bothCount)
	return s
}

func TestQueryFTSAppliesTheClampedFetchLimit(t *testing.T) {
	s := openFTSCorpusStore(t, maxFTSFetchLimit+500, 0)

	// The clamped limit has to reach the SQL LIMIT, not just the caller.
	results, err := queryFTS(s, `"alpha"`, ftsFetchLimit(maxTopK), nil)
	if err != nil {
		t.Fatalf("queryFTS: %v", err)
	}
	if len(results) != maxFTSFetchLimit {
		t.Fatalf("got %d candidate rows, want the clamped %d", len(results), maxFTSFetchLimit)
	}

	// An unclamped topK*5 would have asked for 25000 rows.
	results, err = queryFTS(s, `"alpha"`, ftsFetchLimit(5000), nil)
	if err != nil {
		t.Fatalf("queryFTS at oversized topK: %v", err)
	}
	if len(results) != maxFTSFetchLimit {
		t.Fatalf("got %d candidate rows at topK=5000, want the clamped %d", len(results), maxFTSFetchLimit)
	}
}

func TestSearchFTSAtMaxTopKRunsEveryStageWithinTheLimit(t *testing.T) {
	// Strict AND matches only 400 rows, below topK, so the relaxed OR and
	// prefix OR stages both run.
	s := openFTSCorpusStore(t, maxFTSFetchLimit+500, 400)

	results, err := searchFTS(s, "alpha beta", maxTopK, nil, MetadataFilters{})
	if err != nil {
		t.Fatalf("searchFTS: %v", err)
	}
	if len(results) != maxTopK {
		t.Fatalf("got %d results, want the requested %d", len(results), maxTopK)
	}

	// The first 400 rows carry both terms, so the strict AND stage must have
	// contributed them before the broader stages filled the rest.
	both := 0
	for _, r := range results {
		if strings.Contains(r.Text, "beta") {
			both++
		}
	}
	if both != 400 {
		t.Fatalf("%d results carry both terms, want all 400 from the strict AND stage", both)
	}
}
