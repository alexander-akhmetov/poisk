package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexander-akhmetov/poisk/internal/app"
	"github.com/alexander-akhmetov/poisk/internal/config"
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/index"
	"github.com/alexander-akhmetov/poisk/internal/search"
	"github.com/alexander-akhmetov/poisk/internal/store"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func testEmbedServer(t *testing.T, dims int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		type datum struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		resp := struct {
			Data []datum `json:"data"`
		}{}
		for i := range req.Input {
			vec := make([]float32, dims)
			vec[0] = 1.0
			resp.Data = append(resp.Data, datum{Embedding: vec, Index: i})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

type mcpTestEnv struct {
	client  *gomcp.ClientSession
	indexer *index.Indexer
	db      *store.Store
	cfg     *config.Config
	corpus  string
	cleanup func()
}

func newMCPTestEnv(t *testing.T) *mcpTestEnv {
	t.Helper()
	ctx := context.Background()
	dims := 3
	corpus, _ := filepath.EvalSymlinks(t.TempDir())

	embedSrv := testEmbedServer(t, dims)
	t.Cleanup(embedSrv.Close)

	cfg := config.DefaultConfig()
	cfg.Embedding.BaseURL = embedSrv.URL
	cfg.Embedding.Model = "test-model"
	cfg.Embedding.Dimensions = dims
	cfg.Embedding.BatchSize = 16
	cfg.Index.MaxFileSizeKB = 1024
	cfg.Search.QueryExpansion = false
	cfg.Search.Rerank = false
	cfg.Folders = []config.FolderConfig{
		{Path: corpus, Description: "test corpus"},
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath, dims, cfg.Embedding.Quantization)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	embedClient := embed.NewClient(cfg.Embedding.BaseURL, "", cfg.Embedding.Model, dims, false, false)
	indexer := index.NewIndexer(db, embedClient, &cfg)
	searcher := search.NewSearcher(db, embedClient, &cfg, nil)

	indexSvc := app.NewIndexService(indexer, db, &cfg)
	searchSvc := app.NewSearchService(searcher)
	docSvc := app.NewDocumentService(db, &cfg)
	statusSvc := app.NewStatusService(db, &cfg)

	// Set up MCP server with in-memory transport
	server := gomcp.NewServer(
		&gomcp.Implementation{Name: "poisk-test", Version: "0.1.0"},
		nil,
	)
	registerSearchTool(server, searchSvc)
	registerReindexTool(server, indexSvc)
	registerGetTool(server, docSvc)
	registerMultiGetTool(server, docSvc)
	registerStatusResource(server, statusSvc)

	ct, st := gomcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		db.Close()
		t.Fatalf("server connect: %v", err)
	}

	client := gomcp.NewClient(
		&gomcp.Implementation{Name: "test-client", Version: "0.1.0"},
		nil,
	)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		ss.Close()
		db.Close()
		t.Fatalf("client connect: %v", err)
	}

	return &mcpTestEnv{
		client:  cs,
		indexer: indexer,
		db:      db,
		cfg:     &cfg,
		corpus:  corpus,
		cleanup: func() {
			cs.Close()
			_ = ss.Wait()
			db.Close()
		},
	}
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	mt := time.Now()
	_ = os.Chtimes(path, mt, mt)
	return path
}

func callTool(t *testing.T, env *mcpTestEnv, name string, args any) *gomcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	result, err := env.client.CallTool(ctx, &gomcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

func toolText(t *testing.T, result *gomcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("expected non-empty content in tool result")
	}
	tc, ok := result.Content[0].(*gomcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

func TestSearchTool(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "algo.go", `package algo

// QuickSort sorts a slice in place using the quicksort algorithm.
func QuickSort(data []int) {
    if len(data) <= 1 {
        return
    }
    pivot := data[0]
    lo, hi := 1, len(data)-1
    for lo <= hi {
        if data[lo] <= pivot {
            lo++
        } else {
            data[lo], data[hi] = data[hi], data[lo]
            hi--
        }
    }
    data[0], data[hi] = data[hi], data[0]
    QuickSort(data[:hi])
    QuickSort(data[hi+1:])
}
`)
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	result := callTool(t, env, "search", SearchInput{
		Query: "lex:QuickSort",
		TopK:  5,
	})
	text := toolText(t, result)
	if !strings.Contains(text, "QuickSort") {
		t.Errorf("search result should contain 'QuickSort', got: %s", text)
	}
	if result.IsError {
		t.Errorf("search tool returned error: %s", text)
	}
}

func TestSearchToolNoResults(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "doc.txt", "some basic content for testing the search tool behavior")
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	result := callTool(t, env, "search", SearchInput{
		Query: "lex:xyznonexistentzzz",
		TopK:  5,
	})
	text := toolText(t, result)
	if !strings.Contains(text, "No results found") {
		t.Errorf("expected 'No results found' message, got: %s", text)
	}
}

func TestReindexTool(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "file.txt", "content long enough to produce an indexed chunk for reindex testing")

	result := callTool(t, env, "reindex", ReindexInput{})
	text := toolText(t, result)
	if !strings.Contains(text, "files=") {
		t.Errorf("reindex output should contain stats, got: %s", text)
	}
	if !strings.Contains(text, "chunks=") {
		t.Errorf("reindex output should contain chunk count, got: %s", text)
	}
}

func TestReindexToolSpecificFolder(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "file.txt", "content long enough to produce an indexed chunk for specific folder test")

	result := callTool(t, env, "reindex", ReindexInput{
		Folder: env.corpus,
	})
	text := toolText(t, result)
	if !strings.Contains(text, "files=") {
		t.Errorf("reindex output should contain stats, got: %s", text)
	}
}

func TestReindexToolInvalidFolder(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	result := callTool(t, env, "reindex", ReindexInput{Folder: "/nonexistent/folder"})
	if !result.IsError {
		t.Fatal("expected error result for invalid folder")
	}
	text := toolText(t, result)
	if !strings.Contains(text, "not in configured folders") {
		t.Errorf("expected 'not in configured folders' error, got: %s", text)
	}
}

func TestReindexToolForce(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "file.txt", "content long enough to produce an indexed chunk for force reindex test")

	// Index first
	callTool(t, env, "reindex", ReindexInput{})

	count1, _ := env.db.Count(env.corpus)
	if count1 == 0 {
		t.Fatal("expected chunks after first reindex")
	}

	// Force reindex
	result := callTool(t, env, "reindex", ReindexInput{
		Folder: env.corpus,
		Force:  true,
	})
	text := toolText(t, result)
	if !strings.Contains(text, "files=") {
		t.Errorf("force reindex should produce stats, got: %s", text)
	}
}

func TestGetTool(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	filePath := writeFixture(t, env.corpus, "sample.go", `package sample

func Hello() string {
    return "hello world from get tool test fixture"
}

func Goodbye() string {
    return "goodbye from get tool test fixture"
}
`)
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	result := callTool(t, env, "get", GetInput{
		FilePath: filePath,
	})
	text := toolText(t, result)
	if !strings.Contains(text, "sample.go") {
		t.Errorf("get result should reference file path, got: %s", text)
	}
}

func TestGetToolLineRange(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	filePath := writeFixture(t, env.corpus, "ranged.go", `package ranged

// FirstFunction is the first function in the file.
func FirstFunction() string {
    return "first function content for line range testing"
}

// SecondFunction is the second function in the file.
func SecondFunction() string {
    return "second function content for line range testing"
}
`)
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	// Get with line range filter — should only return chunks overlapping the range
	result := callTool(t, env, "get", GetInput{
		FilePath:  filePath,
		StartLine: 1,
		EndLine:   6,
	})
	text := toolText(t, result)
	if text == "" {
		t.Error("expected non-empty content for line range get")
	}
}

func TestGetToolNotFound(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	result := callTool(t, env, "get", GetInput{FilePath: "/definitely/not/a/configured/path.go"})
	if !result.IsError {
		t.Fatal("expected error result for file not under configured folder")
	}
	text := toolText(t, result)
	if !strings.Contains(text, "not under any configured folder") {
		t.Errorf("expected 'not under any configured folder' error, got: %s", text)
	}
}

func TestMultiGetTool(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "a.go", `package a

func FuncA() string { return "function A content for multi-get testing" }
`)
	writeFixture(t, env.corpus, "b.go", `package b

func FuncB() string { return "function B content for multi-get testing" }
`)
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	aPath := filepath.Join(env.corpus, "a.go")
	bPath := filepath.Join(env.corpus, "b.go")

	result := callTool(t, env, "multi_get", MultiGetInput{
		Paths: aPath + "," + bPath,
	})
	text := toolText(t, result)

	if !strings.Contains(text, "=== "+aPath+" ===") {
		t.Errorf("multi_get should contain a.go header, got: %s", text)
	}
	if !strings.Contains(text, "=== "+bPath+" ===") {
		t.Errorf("multi_get should contain b.go header, got: %s", text)
	}
}

func TestMultiGetToolGlob(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "x.go", `package x

func FuncX() string { return "function X content for glob multi-get testing" }
`)
	writeFixture(t, env.corpus, "y.go", `package y

func FuncY() string { return "function Y content for glob multi-get testing" }
`)
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	result := callTool(t, env, "multi_get", MultiGetInput{
		Paths: "*.go",
	})
	text := toolText(t, result)

	if !strings.Contains(text, "===") {
		t.Errorf("multi_get with glob should return file headers, got: %s", text)
	}
}

func TestMultiGetToolNoMatch(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	result := callTool(t, env, "multi_get", MultiGetInput{
		Paths: "nonexistent*.xyz",
	})
	text := toolText(t, result)
	if !strings.Contains(text, "No matching files") {
		t.Errorf("expected 'No matching files' for nonexistent glob, got: %s", text)
	}
}

func TestMultiGetToolMaxBytes(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "large.txt", strings.Repeat("x", 1000)+" large content for max bytes testing of multi-get tool")
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	largePath := filepath.Join(env.corpus, "large.txt")
	// Set maxBytes big enough for the header (=== /path ===) but too small for content
	maxBytes := len("=== ") + len(largePath) + len(" ===\n") + 10
	result := callTool(t, env, "multi_get", MultiGetInput{
		Paths:    largePath,
		MaxBytes: maxBytes,
	})
	text := toolText(t, result)
	if !strings.Contains(text, "truncated") {
		t.Errorf("expected truncation notice for small max_bytes, got: %s", text)
	}
}

func TestIndexStatusResource(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	writeFixture(t, env.corpus, "doc.txt", "documentation content long enough to be indexed as a chunk for status test")
	if _, err := env.indexer.IndexAll(context.Background()); err != nil {
		t.Fatalf("index: %v", err)
	}

	ctx := context.Background()
	res, err := env.client.ReadResource(ctx, &gomcp.ReadResourceParams{
		URI: "poisk://index-status",
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) == 0 {
		t.Fatal("expected non-empty resource contents")
	}

	var status map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &status); err != nil {
		t.Fatalf("parse status JSON: %v", err)
	}

	// Verify structure
	for _, key := range []string{"vec_available", "fts_available", "folders"} {
		if _, ok := status[key]; !ok {
			t.Errorf("missing key %q in index-status", key)
		}
	}

	folders, ok := status["folders"].([]any)
	if !ok || len(folders) == 0 {
		t.Fatal("expected at least one folder in index-status")
	}
	folder := folders[0].(map[string]any)
	for _, key := range []string{"path", "description", "files", "chunks"} {
		if _, ok := folder[key]; !ok {
			t.Errorf("missing key %q in folder status", key)
		}
	}

	if folder["files"].(float64) < 1 {
		t.Errorf("expected at least 1 file after indexing, got %v", folder["files"])
	}
	if folder["chunks"].(float64) < 1 {
		t.Errorf("expected at least 1 chunk after indexing, got %v", folder["chunks"])
	}
}

func TestListTools(t *testing.T) {
	env := newMCPTestEnv(t)
	defer env.cleanup()

	ctx := context.Background()
	res, err := env.client.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	expectedTools := map[string]bool{
		"search":    false,
		"reindex":   false,
		"get":       false,
		"multi_get": false,
	}
	for _, tool := range res.Tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
	}
	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q not found in ListTools", name)
		}
	}
}
