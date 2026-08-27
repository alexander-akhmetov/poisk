package search

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/alexander-akhmetov/poisk/internal/store"
)

func TestExecVecQueryAppliesFolderMetadataFiltersAndThreshold(t *testing.T) {
	for _, quantization := range []string{store.QuantizationInt8, store.QuantizationFloat32} {
		t.Run(quantization, func(t *testing.T) {
			testExecVecQueryFiltersAndThreshold(t, quantization)
		})
	}
}

func testExecVecQueryFiltersAndThreshold(t *testing.T, quantization string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vec-filters.db")
	s, err := store.Open(dbPath, 3, quantization)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}

	if err := s.InsertEntries("repo-a", "target.go", []store.Entry{
		{
			LineNum:   10,
			EndLine:   20,
			Text:      "fetch user by id",
			Embedding: []float32{1, 0, 0},
			Folder:    "repo-a",
			Language:  "go",
			Kind:      "function_declaration",
			Symbol:    "FetchUser",
		},
	}); err != nil {
		t.Fatalf("insert target entry: %v", err)
	}
	if err := s.InsertEntries("repo-b", "other-source.go", []store.Entry{
		{
			LineNum:   10,
			EndLine:   20,
			Text:      "fetch user by id",
			Embedding: []float32{1, 0, 0},
			Folder:    "repo-b",
			Language:  "go",
			Kind:      "function_declaration",
			Symbol:    "FetchUser",
		},
	}); err != nil {
		t.Fatalf("insert cross-folder noise entry: %v", err)
	}
	if err := s.InsertEntries("repo-a", "wrong-language.go", []store.Entry{
		{
			LineNum:   10,
			EndLine:   20,
			Text:      "fetch user by id",
			Embedding: []float32{1, 0, 0},
			Folder:    "repo-a",
			Language:  "rust",
			Kind:      "function_item",
			Symbol:    "fetch_user",
		},
	}); err != nil {
		t.Fatalf("insert metadata noise entry: %v", err)
	}

	queryBlob := store.Float32sToBlob([]float32{1, 0, 0})
	results, err := execVecQuery(s, queryBlob, 5, 50, []string{"repo-a"}, MetadataFilters{
		Languages: []string{"go"},
		Kinds:     []string{"function_declaration"},
		Symbols:   []string{"fetchuser"},
	}, 0.9)
	if err != nil {
		t.Fatalf("execVecQuery failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one filtered vec result, got %d", len(results))
	}
	if results[0].FilePath != "target.go" {
		t.Fatalf("expected target.go, got %s", results[0].FilePath)
	}
	if results[0].Language != "go" || results[0].Kind != "function_declaration" || results[0].Symbol != "FetchUser" {
		t.Fatalf("unexpected metadata in vec result: %+v", results[0])
	}
	if results[0].Score < 0.9 {
		t.Fatalf("expected vec score >= 0.9, got %.3f", results[0].Score)
	}
	// Fusion keys on the row id, so a vec result without one would collapse
	// into any FTS result on the same file and line.
	if results[0].RowID == 0 {
		t.Fatal("vec result carries no row id")
	}
}

func TestSearchVecRetriesWhenFilteredResultsAreBelowTopK(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vec-retry.db")
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}

	for i := range 15 {
		if err := s.InsertEntries("repo", fmt.Sprintf("noise-%02d.go", i), []store.Entry{
			{
				LineNum:   1,
				EndLine:   1,
				Text:      "python helper",
				Embedding: []float32{1, 0, 0},
				Folder:    "repo",
				Language:  "python",
				Kind:      "function_definition",
				Symbol:    fmt.Sprintf("noise%d", i),
			},
		}); err != nil {
			t.Fatalf("insert noise entry %d: %v", i, err)
		}
	}
	if err := s.InsertEntries("repo", "target.go", []store.Entry{
		{
			LineNum:   1,
			EndLine:   1,
			Text:      "go helper",
			Embedding: []float32{0.8, 0.2, 0},
			Folder:    "repo",
			Language:  "go",
			Kind:      "function_declaration",
			Symbol:    "target",
		},
	}); err != nil {
		t.Fatalf("insert target entry: %v", err)
	}

	queryBlob := store.Float32sToBlob([]float32{1, 0, 0})
	results, err := searchVec(s, queryBlob, 3, nil, MetadataFilters{
		Languages: []string{"go"},
	}, 0.0)
	if err != nil {
		t.Fatalf("searchVec failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one go result after filtered retry, got %d", len(results))
	}
	if results[0].Language != "go" || results[0].FilePath != "target.go" {
		t.Fatalf("unexpected retry result: %+v", results[0])
	}
}

// openScopedRecallStore builds a corpus where the requested source holds a
// small fraction of the vectors and every other vector is closer to the query.
// A folder filter applied after the KNN would return nothing on a corpus shaped
// like this.
func openScopedRecallStore(t *testing.T, noiseChunks int) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "vec-scoped.db")
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}

	addNoise(t, s, "noise-repo", "noise.go", noiseChunks)

	target := make([]store.Entry, 10)
	for i := range target {
		target[i] = store.Entry{
			LineNum:   i + 1,
			EndLine:   i + 1,
			Text:      fmt.Sprintf("target chunk %d", i),
			Embedding: []float32{0.6, 0.8, 0},
			Folder:    "target-repo",
			Language:  "go",
		}
	}
	if err := s.InsertEntries("target-repo", "target.go", target); err != nil {
		t.Fatalf("insert target entries: %v", err)
	}
	return s
}

func addNoise(t *testing.T, s *store.Store, source, filePath string, count int) {
	t.Helper()
	noise := make([]store.Entry, count)
	for i := range noise {
		noise[i] = store.Entry{
			LineNum:   i + 1,
			EndLine:   i + 1,
			Text:      fmt.Sprintf("noise chunk %d", i),
			Embedding: []float32{1, 0, 0},
			Folder:    source,
			Language:  "go",
		}
	}
	if err := s.InsertEntries(source, filePath, noise); err != nil {
		t.Fatalf("insert noise entries into %s: %v", source, err)
	}
}

func TestSearchVecScopedToSmallSourceReturnsAllItsResults(t *testing.T) {
	s := openScopedRecallStore(t, 1990)

	queryBlob := store.Float32sToBlob([]float32{1, 0, 0})
	// Default top_k, with far more closer noise vectors than any fetch limit
	// covers. Only a KNN restricted to the partition finds the target rows.
	results, err := searchVec(s, queryBlob, 20, []string{"target-repo"}, MetadataFilters{}, 0.0)
	if err != nil {
		t.Fatalf("searchVec failed: %v", err)
	}
	if len(results) != 10 {
		t.Fatalf("scoped vec search returned %d results, want all 10 from the requested source", len(results))
	}
	for _, r := range results {
		if r.Folder != "target-repo" {
			t.Fatalf("result from unrequested source: %+v", r)
		}
	}

	// Growing the unrelated sources must not push the scoped results out.
	addNoise(t, s, "other-repo", "more-noise.go", 2000)

	results, err = searchVec(s, queryBlob, 20, []string{"target-repo"}, MetadataFilters{}, 0.0)
	if err != nil {
		t.Fatalf("searchVec after growth failed: %v", err)
	}
	if len(results) != 10 {
		t.Fatalf("scoped vec search returned %d results after the index grew, want 10", len(results))
	}
	for _, r := range results {
		if r.Folder != "target-repo" {
			t.Fatalf("result from unrequested source after growth: %+v", r)
		}
	}
}

func TestSearchVecCapsResultsWhenScopedToMultipleSources(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "vec-multi.db")
	s, err := store.Open(dbPath, 3, store.QuantizationInt8)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.VecAvailable() {
		t.Skip("vec0 not available")
	}

	// vec0 applies k per partition, so two sources return twice the rows
	// internally. The caller must still see at most topK.
	addNoise(t, s, "repo-a", "a.go", 40)
	addNoise(t, s, "repo-b", "b.go", 40)

	queryBlob := store.Float32sToBlob([]float32{1, 0, 0})
	results, err := searchVec(s, queryBlob, 10, []string{"repo-a", "repo-b"}, MetadataFilters{}, 0.0)
	if err != nil {
		t.Fatalf("searchVec failed: %v", err)
	}
	if len(results) != 10 {
		t.Fatalf("got %d results across two partitions, want the requested 10", len(results))
	}
}

func TestSearchVecRunsWithTopKAboveTheSqliteVecCeiling(t *testing.T) {
	s := openScopedRecallStore(t, 200)

	queryBlob := store.Float32sToBlob([]float32{1, 0, 0})
	for _, tc := range []struct {
		name    string
		folders []string
	}{
		{"unscoped", nil},
		{"scoped", []string{"target-repo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// topK*5 and topK*10 both exceed the sqlite-vec limit of 4096, which
			// fails the query outright and silently disables vector search.
			results, err := searchVec(s, queryBlob, 5000, tc.folders, MetadataFilters{}, 0.0)
			if err != nil {
				t.Fatalf("searchVec with top_k=5000: %v", err)
			}
			if len(results) == 0 {
				t.Fatal("expected the vector leg to contribute results at top_k=5000")
			}
		})
	}
}

func TestVecFetchLimits(t *testing.T) {
	tests := []struct {
		name       string
		topK       int
		scoped     bool
		wantFetch  int
		wantRetry  int
		wantWidens bool
	}{
		{name: "default unscoped", topK: 20, wantFetch: 100, wantRetry: 1000, wantWidens: true},
		{name: "default scoped", topK: 20, scoped: true, wantFetch: 200, wantRetry: 1000, wantWidens: true},
		{name: "fetch clamped, retry still widens", topK: 900, wantFetch: store.MaxVecK, wantRetry: store.MaxVecK},
		{name: "scoped at the ceiling", topK: 1000, scoped: true, wantFetch: store.MaxVecK, wantRetry: store.MaxVecK},
		{name: "far above the ceiling", topK: 5000, scoped: true, wantFetch: store.MaxVecK, wantRetry: store.MaxVecK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetch, retry := vecFetchLimits(tt.topK, tt.scoped)
			if fetch != tt.wantFetch {
				t.Fatalf("fetchLimit=%d, want %d", fetch, tt.wantFetch)
			}
			if retry != tt.wantRetry {
				t.Fatalf("retryLimit=%d, want %d", retry, tt.wantRetry)
			}
			if fetch > store.MaxVecK || retry > store.MaxVecK {
				t.Fatalf("limits above the sqlite-vec ceiling: fetch=%d retry=%d", fetch, retry)
			}
			if retry < fetch {
				t.Fatalf("retry %d is narrower than the first attempt %d", retry, fetch)
			}
			if widens := retry > fetch; widens != tt.wantWidens {
				t.Fatalf("retry widens=%v, want %v", widens, tt.wantWidens)
			}
		})
	}
}
