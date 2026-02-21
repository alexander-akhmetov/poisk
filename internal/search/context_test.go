package search

import (
	"testing"
)

func TestResolveContext(t *testing.T) {
	tests := []struct {
		name       string
		filePath   string
		folderPath string
		contextMap map[string]string
		want       []string
	}{
		{
			name:       "no context map",
			filePath:   "/repo/src/main.go",
			folderPath: "/repo",
			contextMap: nil,
			want:       nil,
		},
		{
			name:       "root context",
			filePath:   "/repo/anything.go",
			folderPath: "/repo",
			contextMap: map[string]string{".": "Project Root"},
			want:       []string{"Project Root"},
		},
		{
			name:       "single prefix match",
			filePath:   "/repo/internal/search/search.go",
			folderPath: "/repo",
			contextMap: map[string]string{
				"internal/search": "Search Engine",
			},
			want: []string{"Search Engine"},
		},
		{
			name:       "nested prefixes general to specific",
			filePath:   "/repo/internal/search/fts.go",
			folderPath: "/repo",
			contextMap: map[string]string{
				".":               "Poisk Project",
				"internal":        "Core Implementation",
				"internal/search": "Search Engine",
			},
			want: []string{"Poisk Project", "Core Implementation", "Search Engine"},
		},
		{
			name:       "no match",
			filePath:   "/repo/cmd/main.go",
			folderPath: "/repo",
			contextMap: map[string]string{
				"internal": "Core",
			},
			want: nil,
		},
		{
			name:       "empty prefix matches all",
			filePath:   "/repo/anything.go",
			folderPath: "/repo",
			contextMap: map[string]string{
				"": "Everything",
			},
			want: []string{"Everything"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveContext(tt.filePath, tt.folderPath, tt.contextMap)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("context[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
