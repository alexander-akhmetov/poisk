package search

import (
	"testing"
)

func TestParseTypedQuery(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []SubQuery
	}{
		{
			name:  "plain query",
			input: "hello world",
			want:  []SubQuery{{Text: "hello world", Mode: "hybrid", Filters: MetadataFilters{}}},
		},
		{
			name:  "lex prefix",
			input: "lex:exact match",
			want:  []SubQuery{{Text: "exact match", Mode: "fts", Filters: MetadataFilters{}}},
		},
		{
			name:  "vec prefix",
			input: "vec:semantic meaning",
			want:  []SubQuery{{Text: "semantic meaning", Mode: "vec", Filters: MetadataFilters{}}},
		},
		{
			name:  "pipe composition",
			input: "lex:exact | vec:similar",
			want: []SubQuery{
				{Text: "exact", Mode: "fts", Filters: MetadataFilters{}},
				{Text: "similar", Mode: "vec", Filters: MetadataFilters{}},
			},
		},
		{
			name:  "pipe with hybrid",
			input: "lex:keyword | general query",
			want: []SubQuery{
				{Text: "keyword", Mode: "fts", Filters: MetadataFilters{}},
				{Text: "general query", Mode: "hybrid", Filters: MetadataFilters{}},
			},
		},
		{
			name:  "empty",
			input: "",
			want:  nil,
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  nil,
		},
		{
			name:  "pipe without spaces is plain",
			input: "hello|world",
			want:  []SubQuery{{Text: "hello|world", Mode: "hybrid", Filters: MetadataFilters{}}},
		},
		{
			name:  "three parts",
			input: "lex:a | vec:b | hybrid c",
			want: []SubQuery{
				{Text: "a", Mode: "fts", Filters: MetadataFilters{}},
				{Text: "b", Mode: "vec", Filters: MetadataFilters{}},
				{Text: "hybrid c", Mode: "hybrid", Filters: MetadataFilters{}},
			},
		},
		{
			name:  "metadata filters with text",
			input: "language:go kind:function_declaration symbol:FetchUser lexical term",
			want: []SubQuery{
				{
					Text: "lexical term",
					Mode: "hybrid",
					Filters: MetadataFilters{
						Languages: []string{"go"},
						Kinds:     []string{"function_declaration"},
						Symbols:   []string{"fetchuser"},
					},
				},
			},
		},
		{
			name:  "metadata aliases and dedupe",
			input: "lang:go language:go chunk_kind:method_declaration sym:Open sym:open",
			want: []SubQuery{
				{
					Text: "",
					Mode: "hybrid",
					Filters: MetadataFilters{
						Languages: []string{"go"},
						Kinds:     []string{"method_declaration"},
						Symbols:   []string{"open"},
					},
				},
			},
		},
		{
			name:  "typed with metadata filter only",
			input: "lex:language:rust symbol:deserialize",
			want: []SubQuery{
				{
					Text: "",
					Mode: "fts",
					Filters: MetadataFilters{
						Languages: []string{"rust"},
						Symbols:   []string{"deserialize"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTypedQuery(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d sub-queries, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].Text != tt.want[i].Text || got[i].Mode != tt.want[i].Mode {
					t.Fatalf("sub-query[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
				if len(got[i].Filters.Languages) != len(tt.want[i].Filters.Languages) {
					t.Fatalf("sub-query[%d] languages = %v, want %v", i, got[i].Filters.Languages, tt.want[i].Filters.Languages)
				}
				for j := range got[i].Filters.Languages {
					if got[i].Filters.Languages[j] != tt.want[i].Filters.Languages[j] {
						t.Fatalf("sub-query[%d] languages[%d] = %q, want %q", i, j, got[i].Filters.Languages[j], tt.want[i].Filters.Languages[j])
					}
				}
				if len(got[i].Filters.Kinds) != len(tt.want[i].Filters.Kinds) {
					t.Fatalf("sub-query[%d] kinds = %v, want %v", i, got[i].Filters.Kinds, tt.want[i].Filters.Kinds)
				}
				for j := range got[i].Filters.Kinds {
					if got[i].Filters.Kinds[j] != tt.want[i].Filters.Kinds[j] {
						t.Fatalf("sub-query[%d] kinds[%d] = %q, want %q", i, j, got[i].Filters.Kinds[j], tt.want[i].Filters.Kinds[j])
					}
				}
				if len(got[i].Filters.Symbols) != len(tt.want[i].Filters.Symbols) {
					t.Fatalf("sub-query[%d] symbols = %v, want %v", i, got[i].Filters.Symbols, tt.want[i].Filters.Symbols)
				}
				for j := range got[i].Filters.Symbols {
					if got[i].Filters.Symbols[j] != tt.want[i].Filters.Symbols[j] {
						t.Fatalf("sub-query[%d] symbols[%d] = %q, want %q", i, j, got[i].Filters.Symbols[j], tt.want[i].Filters.Symbols[j])
					}
				}
			}
		})
	}
}
