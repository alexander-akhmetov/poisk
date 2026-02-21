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
			want:  []SubQuery{{Text: "hello world", Mode: "hybrid"}},
		},
		{
			name:  "lex prefix",
			input: "lex:exact match",
			want:  []SubQuery{{Text: "exact match", Mode: "fts"}},
		},
		{
			name:  "vec prefix",
			input: "vec:semantic meaning",
			want:  []SubQuery{{Text: "semantic meaning", Mode: "vec"}},
		},
		{
			name:  "pipe composition",
			input: "lex:exact | vec:similar",
			want: []SubQuery{
				{Text: "exact", Mode: "fts"},
				{Text: "similar", Mode: "vec"},
			},
		},
		{
			name:  "pipe with hybrid",
			input: "lex:keyword | general query",
			want: []SubQuery{
				{Text: "keyword", Mode: "fts"},
				{Text: "general query", Mode: "hybrid"},
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
			want:  []SubQuery{{Text: "hello|world", Mode: "hybrid"}},
		},
		{
			name:  "three parts",
			input: "lex:a | vec:b | hybrid c",
			want: []SubQuery{
				{Text: "a", Mode: "fts"},
				{Text: "b", Mode: "vec"},
				{Text: "hybrid c", Mode: "hybrid"},
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
			}
		})
	}
}
