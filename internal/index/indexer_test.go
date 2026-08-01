package index

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

type testEmbeddingRequest struct {
	Input []string `json:"input"`
}

type testEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// embeddingRecorder records the size of every embedding request the indexer
// sends, so tests can assert how chunks were grouped into requests.
type embeddingRecorder struct {
	mu    sync.Mutex
	sizes []int
}

func (r *embeddingRecorder) record(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sizes = append(r.sizes, n)
}

func (r *embeddingRecorder) requestSizes() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.sizes...)
}

func newTestEmbeddingServer(t *testing.T, dims int, failOnCall int32) *httptest.Server {
	t.Helper()
	server, _ := newRecordingEmbeddingServer(t, dims, failOnCall)
	return server
}

func newRecordingEmbeddingServer(t *testing.T, dims int, failOnCall int32) (*httptest.Server, *embeddingRecorder) {
	t.Helper()

	rec := &embeddingRecorder{}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if failOnCall > 0 && call >= failOnCall {
			http.Error(w, "embedding failure", http.StatusInternalServerError)
			return
		}

		rec.record(writeEmbeddings(t, w, r, dims))
	}))
	return server, rec
}

// writeEmbeddings answers an embedding request with unit vectors and returns
// the number of inputs it was given.
func writeEmbeddings(t *testing.T, w http.ResponseWriter, r *http.Request, dims int) int {
	t.Helper()

	var req testEmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode request: %v", err)
	}

	resp := testEmbeddingResponse{
		Data: make([]struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}, len(req.Input)),
	}
	for i := range req.Input {
		vec := make([]float32, dims)
		vec[0] = 1.0
		resp.Data[i].Embedding = vec
		resp.Data[i].Index = i
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return len(req.Input)
}

// newCancellingEmbeddingServer answers requests until cancelOnCall, then cancels
// the indexing context instead of replying. That is what Ctrl-C during an
// embedding request looks like to the indexer.
func newCancellingEmbeddingServer(t *testing.T, dims int, cancelOnCall int32, cancel context.CancelFunc) *httptest.Server {
	t.Helper()

	var calls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) >= cancelOnCall {
			// Draining the body lets the server see the aborting client and end
			// this request; the deadline keeps a stuck one from hanging the test.
			_, _ = io.Copy(io.Discard, r.Body)
			cancel()
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
				t.Error("client did not abort the cancelled embedding request")
			}
			return
		}
		writeEmbeddings(t, w, r, dims)
	}))
}

func newTestIndexer(t *testing.T, folder, baseURL string) (*Indexer, *store.Store) {
	t.Helper()
	return newTestIndexerWithConfig(t, folder, baseURL, nil)
}

func newTestIndexerWithConfig(t *testing.T, folder, baseURL string, tweak func(*config.Config)) (*Indexer, *store.Store) {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = baseURL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = 3
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Folders = []config.FolderConfig{
		{Path: folder, Description: "test"},
	}
	if tweak != nil {
		tweak(&cfg)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, cfg.Embedding.Dimensions, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	client := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, cfg.Embedding.Dimensions, false, false)
	return NewIndexer(db, client, &cfg), db
}

func writeFileWithMtime(t *testing.T, filePath, content string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filePath, err)
	}
	if err := os.Chtimes(filePath, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", filePath, err)
	}
}

func TestIndexFolderClearsEntriesWhenFileProducesNoChunks(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	filePath := filepath.Join(dir, "doc.txt")

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, db := newTestIndexer(t, dir, server.URL)

	t1 := time.Unix(1_700_000_000, 111)
	writeFileWithMtime(t, filePath, "this line is long enough to be indexed as one chunk", t1)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	count, err := db.Count(dir)
	if err != nil {
		t.Fatalf("count after first index: %v", err)
	}
	if count == 0 {
		t.Fatal("expected indexed chunks after first index")
	}

	t2 := time.Unix(1_700_000_000, 222)
	writeFileWithMtime(t, filePath, "short", t2)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("second index: %v", err)
	}

	count, err = db.Count(dir)
	if err != nil {
		t.Fatalf("count after second index: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected stale chunks to be removed, count=%d", count)
	}

	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	if tracked[filePath] != t2.UnixNano() {
		t.Fatalf("tracked mtime=%d, want %d", tracked[filePath], t2.UnixNano())
	}
}

func TestIndexFolderUsesNanosecondMtimeForChangeDetection(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	filePath := filepath.Join(dir, "doc.txt")

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, db := newTestIndexer(t, dir, server.URL)

	t1 := time.Unix(1_700_000_001, 100)
	writeFileWithMtime(t, filePath, "first-version-token content long enough for indexing", t1)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Same second, different nanoseconds. With second-precision mtimes this change would be missed.
	t2 := time.Unix(1_700_000_001, 900)
	writeFileWithMtime(t, filePath, "second-version-token content long enough for indexing", t2)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("second index: %v", err)
	}

	entries, err := db.GetEntriesByPath(dir, filePath)
	if err != nil {
		t.Fatalf("get entries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries after second index")
	}

	foundUpdated := false
	for _, e := range entries {
		if strings.Contains(e.Text, "second-version-token") {
			foundUpdated = true
			break
		}
	}
	if !foundUpdated {
		t.Fatal("expected second content to be indexed after same-second mtime update")
	}
}

func TestIndexFolderRemovesOldDataWhenEmbeddingFails(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	filePath := filepath.Join(dir, "doc.txt")

	// First embedding call succeeds, second fails.
	server := newTestEmbeddingServer(t, 3, 2)
	defer server.Close()
	indexer, db := newTestIndexer(t, dir, server.URL)

	t1 := time.Unix(1_700_000_002, 100)
	writeFileWithMtime(t, filePath, "initial content long enough for indexing", t1)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	count, err := db.Count(dir)
	if err != nil {
		t.Fatalf("count after first index: %v", err)
	}
	if count == 0 {
		t.Fatal("expected indexed chunks after first index")
	}

	t2 := time.Unix(1_700_000_002, 200)
	writeFileWithMtime(t, filePath, "updated content still long enough for indexing", t2)
	stats, err := indexer.IndexFolder(context.Background(), dir)
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Errors == 0 {
		t.Fatal("expected indexing errors when embedding fails")
	}

	count, err = db.Count(dir)
	if err != nil {
		t.Fatalf("count after failed index: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected old chunks removed after embedding failure, count=%d", count)
	}

	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	if tracked[filePath] != t1.UnixNano() {
		t.Fatalf("tracked mtime=%d, want previous successful mtime %d", tracked[filePath], t1.UnixNano())
	}
}

// sectionedMarkdown returns markdown that chunks into exactly sections chunks.
func sectionedMarkdown(sections int) string {
	var sb strings.Builder
	for i := range sections {
		fmt.Fprintf(&sb, "## Section %d\n\nThis is section %d with enough words in the body to make a real chunk that is not discarded as too short.\n\n", i, i)
	}
	return sb.String()
}

func TestIndexFolderGroupsSmallFilesIntoOneEmbeddingRequest(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	const files, chunksPerFile = 8, 6
	mtime := time.Unix(1_700_001_000, 100)
	for i := range files {
		writeFileWithMtime(t, filepath.Join(dir, fmt.Sprintf("doc-%d.md", i)), sectionedMarkdown(chunksPerFile), mtime)
	}
	// A file that produces no chunks must stay off the batching path entirely.
	writeFileWithMtime(t, filepath.Join(dir, "empty.md"), "short", mtime)

	server, rec := newRecordingEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, db := newTestIndexerWithConfig(t, dir, server.URL, func(cfg *config.Config) {
		cfg.Embedding.BatchSize = 50
	})

	stats, err := indexer.IndexFolder(context.Background(), dir)
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	sizes := rec.requestSizes()
	if len(sizes) != 1 || sizes[0] != files*chunksPerFile {
		t.Fatalf("embedding requests = %v, want one request of %d chunks", sizes, files*chunksPerFile)
	}
	if stats.FilesProcessed != files {
		t.Fatalf("FilesProcessed=%d, want %d", stats.FilesProcessed, files)
	}
	if stats.ChunksCreated != files*chunksPerFile {
		t.Fatalf("ChunksCreated=%d, want %d", stats.ChunksCreated, files*chunksPerFile)
	}
	if stats.Errors != 0 {
		t.Fatalf("Errors=%d, want 0", stats.Errors)
	}

	// InsertEntries fully replaces a file's chunks, so a file split across two
	// calls would keep only the last call's chunks.
	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	for i := range files {
		fp := filepath.Join(dir, fmt.Sprintf("doc-%d.md", i))
		entries, err := db.GetEntriesByPath(dir, fp)
		if err != nil {
			t.Fatalf("get entries for %s: %v", fp, err)
		}
		if len(entries) != chunksPerFile {
			t.Fatalf("%s has %d entries, want %d from a single InsertEntries call", fp, len(entries), chunksPerFile)
		}
		if tracked[fp] != mtime.UnixNano() {
			t.Fatalf("%s mtime=%d, want %d", fp, tracked[fp], mtime.UnixNano())
		}
	}

	// The zero-chunk file is tracked but contributes nothing to the batch.
	emptyEntries, err := db.GetEntriesByPath(dir, filepath.Join(dir, "empty.md"))
	if err != nil {
		t.Fatalf("get entries for empty.md: %v", err)
	}
	if len(emptyEntries) != 0 {
		t.Fatalf("empty.md has %d entries, want 0", len(emptyEntries))
	}
	if tracked[filepath.Join(dir, "empty.md")] != mtime.UnixNano() {
		t.Fatal("empty.md mtime was not advanced")
	}

	// A second run changes nothing and sends no further requests.
	stats, err = indexer.IndexFolder(context.Background(), dir)
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.FilesSkipped != files+1 || stats.Errors != 0 {
		t.Fatalf("second run: skipped=%d errors=%d, want skipped=%d errors=0", stats.FilesSkipped, stats.Errors, files+1)
	}
	if got := len(rec.requestSizes()); got != 1 {
		t.Fatalf("second run sent %d total requests, want the original 1", got)
	}
}

func TestIndexFolderFailedGroupClearsEveryFileInIt(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	const files, chunksPerFile = 4, 6
	t1 := time.Unix(1_700_002_000, 100)
	for i := range files {
		writeFileWithMtime(t, filepath.Join(dir, fmt.Sprintf("doc-%d.md", i)), sectionedMarkdown(chunksPerFile), t1)
	}

	// The first grouped request succeeds, the second fails.
	server, _ := newRecordingEmbeddingServer(t, 3, 2)
	defer server.Close()
	indexer, db := newTestIndexerWithConfig(t, dir, server.URL, func(cfg *config.Config) {
		cfg.Embedding.BatchSize = 50
	})

	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}
	count, err := db.Count(dir)
	if err != nil {
		t.Fatalf("count after first index: %v", err)
	}
	if count != files*chunksPerFile {
		t.Fatalf("count after first index=%d, want %d", count, files*chunksPerFile)
	}

	t2 := time.Unix(1_700_002_000, 200)
	for i := range files {
		writeFileWithMtime(t, filepath.Join(dir, fmt.Sprintf("doc-%d.md", i)), sectionedMarkdown(chunksPerFile+1), t2)
	}

	stats, err := indexer.IndexFolder(context.Background(), dir)
	if err != nil {
		t.Fatalf("second index: %v", err)
	}
	if stats.Errors != files {
		t.Fatalf("Errors=%d after a failed group, want one per grouped file (%d)", stats.Errors, files)
	}
	if stats.FilesProcessed != 0 {
		t.Fatalf("FilesProcessed=%d after a failed group, want 0", stats.FilesProcessed)
	}

	count, err = db.Count(dir)
	if err != nil {
		t.Fatalf("count after failed group: %v", err)
	}
	if count != 0 {
		t.Fatalf("count=%d after failed group, want every grouped file cleared", count)
	}

	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	for i := range files {
		fp := filepath.Join(dir, fmt.Sprintf("doc-%d.md", i))
		if tracked[fp] != t1.UnixNano() {
			t.Fatalf("%s mtime=%d, want the previous successful mtime %d", fp, tracked[fp], t1.UnixNano())
		}
	}
}

func TestIndexFolderCancelledGroupKeepsItsIndexedData(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	const files, chunksPerFile = 4, 6
	t1 := time.Unix(1_700_004_000, 100)
	for i := range files {
		writeFileWithMtime(t, filepath.Join(dir, fmt.Sprintf("doc-%d.md", i)), sectionedMarkdown(chunksPerFile), t1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The first run's request is answered; the second run's is cancelled.
	server := newCancellingEmbeddingServer(t, 3, 2, cancel)
	defer server.Close()
	indexer, db := newTestIndexerWithConfig(t, dir, server.URL, func(cfg *config.Config) {
		cfg.Embedding.BatchSize = 50
	})

	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	t2 := time.Unix(1_700_004_000, 200)
	for i := range files {
		writeFileWithMtime(t, filepath.Join(dir, fmt.Sprintf("doc-%d.md", i)), sectionedMarkdown(chunksPerFile+1), t2)
	}

	stats, err := indexer.IndexFolder(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run returned %v, want context.Canceled", err)
	}
	if stats.Errors != files {
		t.Fatalf("Errors=%d after a cancelled group, want one per grouped file (%d)", stats.Errors, files)
	}

	// A cancellation says nothing about the files in the group, so their
	// previously indexed chunks must survive it.
	count, err := db.Count(dir)
	if err != nil {
		t.Fatalf("count after cancelled group: %v", err)
	}
	if count != files*chunksPerFile {
		t.Fatalf("count=%d after a cancelled group, want the %d chunks from the last successful run", count, files*chunksPerFile)
	}

	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	for i := range files {
		fp := filepath.Join(dir, fmt.Sprintf("doc-%d.md", i))
		if tracked[fp] != t1.UnixNano() {
			t.Fatalf("%s mtime=%d, want the previous successful mtime %d", fp, tracked[fp], t1.UnixNano())
		}
	}
}

func TestIndexFolderKeepsOversizedFileOnItsOwnRequests(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())

	const batchSize, bigChunks, smallChunks = 4, 6, 2
	mtime := time.Unix(1_700_003_000, 100)
	bigPath := filepath.Join(dir, "a-big.md")
	smallPath := filepath.Join(dir, "b-small.md")
	writeFileWithMtime(t, bigPath, sectionedMarkdown(bigChunks), mtime)
	writeFileWithMtime(t, smallPath, sectionedMarkdown(smallChunks), mtime)

	server, rec := newRecordingEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, db := newTestIndexerWithConfig(t, dir, server.URL, func(cfg *config.Config) {
		cfg.Embedding.BatchSize = batchSize
	})

	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("index: %v", err)
	}

	// The oversized file splits across requests of BatchSize and never shares
	// one with the file that follows it.
	want := []int{batchSize, bigChunks - batchSize, smallChunks}
	sizes := rec.requestSizes()
	if len(sizes) != len(want) {
		t.Fatalf("embedding requests = %v, want %v", sizes, want)
	}
	for i := range want {
		if sizes[i] != want[i] {
			t.Fatalf("embedding requests = %v, want %v", sizes, want)
		}
	}

	// All of the oversized file's chunks survive, so they arrived in one
	// InsertEntries call.
	entries, err := db.GetEntriesByPath(dir, bigPath)
	if err != nil {
		t.Fatalf("get entries: %v", err)
	}
	if len(entries) != bigChunks {
		t.Fatalf("oversized file has %d entries, want %d", len(entries), bigChunks)
	}
}

func TestIndexFolderDeletedFile(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	filePath := filepath.Join(dir, "doc.txt")

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, db := newTestIndexer(t, dir, server.URL)

	t1 := time.Unix(1_700_000_010, 100)
	writeFileWithMtime(t, filePath, "this line is long enough to be indexed as one chunk for deletion test", t1)
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("first index: %v", err)
	}

	count, err := db.Count(dir)
	if err != nil {
		t.Fatalf("count after first index: %v", err)
	}
	if count == 0 {
		t.Fatal("expected indexed chunks after first index")
	}

	// Delete the file from disk
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	// Re-index — the deleted file's stale entries should be pruned
	if _, err := indexer.IndexFolder(context.Background(), dir); err != nil {
		t.Fatalf("second index: %v", err)
	}

	count, err = db.Count(dir)
	if err != nil {
		t.Fatalf("count after re-index: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected stale chunks removed after file deletion, count=%d", count)
	}

	tracked, err := db.TrackedFiles(dir)
	if err != nil {
		t.Fatalf("tracked files: %v", err)
	}
	if _, ok := tracked[filePath]; ok {
		t.Fatal("expected tracked mtime removed for deleted file")
	}
}

func TestIndexFolderContextCancelled(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFileWithMtime(t, filepath.Join(dir, "doc.txt"),
		"this line is long enough to be indexed as one chunk for context test",
		time.Unix(1_700_000_020, 100))

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()
	indexer, _ := newTestIndexer(t, dir, server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := indexer.IndexFolder(ctx, dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestIndexAllPrunesRemovedFolders(t *testing.T) {
	dir1, _ := filepath.EvalSymlinks(t.TempDir())
	dir2, _ := filepath.EvalSymlinks(t.TempDir())

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = server.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = 3
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Folders = []config.FolderConfig{
		{Path: dir1, Description: "folder1"},
		{Path: dir2, Description: "folder2"},
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, cfg.Embedding.Dimensions, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, cfg.Embedding.Dimensions, false, false)
	indexer := NewIndexer(db, client, &cfg)

	// Write files in both folders
	t1 := time.Unix(1_700_000_000, 100)
	writeFileWithMtime(t, filepath.Join(dir1, "a.txt"), "this line is long enough to be indexed as one chunk in folder one", t1)
	writeFileWithMtime(t, filepath.Join(dir2, "b.txt"), "this line is long enough to be indexed as one chunk in folder two", t1)

	if _, err := indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("first IndexAll: %v", err)
	}

	count1, _ := db.Count(dir1)
	count2, _ := db.Count(dir2)
	if count1 == 0 || count2 == 0 {
		t.Fatalf("expected chunks in both folders, got %d and %d", count1, count2)
	}

	// Remove dir2 from config
	cfg.Folders = []config.FolderConfig{
		{Path: dir1, Description: "folder1"},
	}

	if _, err := indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("second IndexAll: %v", err)
	}

	count1, _ = db.Count(dir1)
	count2, _ = db.Count(dir2)
	if count1 == 0 {
		t.Fatal("expected folder1 chunks to remain")
	}
	if count2 != 0 {
		t.Fatalf("expected folder2 chunks pruned, got %d", count2)
	}

	// Verify meta is cleared too
	mc, _ := db.ModelChanged(dir2, "test-embedding", 3, cfg.Embedding.Quantization)
	if !mc.Changed {
		t.Fatal("expected meta cleared for pruned folder")
	}
}

func TestIndexFolderRebuildsOnQuantizationChange(t *testing.T) {
	folder, _ := filepath.EvalSymlinks(t.TempDir())

	server := newTestEmbeddingServer(t, 3, 0)
	defer server.Close()

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = server.URL
	cfg.Embedding.Model = "test-embedding"
	cfg.Embedding.Dimensions = 3
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Folders = []config.FolderConfig{
		{Path: folder, Description: "test"},
	}

	writeFileWithMtime(t, filepath.Join(folder, "a.txt"), "this line is long enough to be indexed as one chunk", time.Unix(1_700_000_000, 0))

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, cfg.Embedding.Dimensions, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if !db.VecAvailable() {
		_ = db.Close()
		t.Skip("vec0 not available")
	}
	client := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, cfg.Embedding.Dimensions, false, false)

	stats, err := NewIndexer(db, client, &cfg).IndexFolder(context.Background(), folder)
	if err != nil {
		t.Fatalf("first index: %v", err)
	}
	if stats.FilesProcessed == 0 {
		t.Fatal("expected file processed on first index")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	// Reopen with float32 and reindex — the unchanged file must be re-embedded
	cfg.Embedding.Quantization = "float32"
	db2, err := store.Open(dbPath, cfg.Embedding.Dimensions, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = db2.Close() })

	stats, err = NewIndexer(db2, client, &cfg).IndexFolder(context.Background(), folder)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if stats.FilesProcessed == 0 {
		t.Fatal("expected re-embed after quantization change, file was skipped")
	}

	var vecCount int
	if err := db2.DB().QueryRow("SELECT COUNT(*) FROM vec_embeddings").Scan(&vecCount); err != nil {
		t.Fatalf("count vec_embeddings: %v", err)
	}
	if vecCount == 0 {
		t.Fatal("expected vec embeddings after rebuild")
	}
}
