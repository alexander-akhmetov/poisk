package chunk

import (
	"strings"
	"testing"
)

func TestTreeSitterGo(t *testing.T) {
	content := `package example

func Hello() string {
	return "hello"
}

type Config struct {
	Name string
}

func (c *Config) GetName() string {
	return c.Name
}
`
	chunks, err := File("example.go", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	// Check first function
	found := false
	for _, c := range chunks {
		if c.Symbol == "Hello" {
			found = true
			if c.Language != "go" {
				t.Errorf("language = %q, want go", c.Language)
			}
			if c.Kind != "function_declaration" {
				t.Errorf("kind = %q, want function_declaration", c.Kind)
			}
			if c.StartLine == 0 || c.EndLine == 0 {
				t.Error("missing line range")
			}
			if c.EndLine < c.StartLine {
				t.Errorf("EndLine %d < StartLine %d", c.EndLine, c.StartLine)
			}
			break
		}
	}
	if !found {
		t.Error("did not find chunk with symbol 'Hello'")
	}

	// Check type declaration
	found = false
	for _, c := range chunks {
		if c.Symbol == "Config" {
			found = true
			if c.Kind != "type_declaration" {
				t.Errorf("type kind = %q, want type_declaration", c.Kind)
			}
			break
		}
	}
	if !found {
		t.Error("did not find chunk with symbol 'Config'")
	}
}

func TestTreeSitterPython(t *testing.T) {
	content := `def greet(name):
    return f"Hello, {name}!"

class Greeter:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return f"Hello, {self.name}!"
`
	chunks, err := File("example.py", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	// Check function
	found := false
	for _, c := range chunks {
		if c.Symbol == "greet" && c.Kind == "function_definition" {
			found = true
			if c.Language != "python" {
				t.Errorf("language = %q, want python", c.Language)
			}
			break
		}
	}
	if !found {
		t.Error("did not find function 'greet'")
	}

	// Check class
	found = false
	for _, c := range chunks {
		if c.Symbol == "Greeter" {
			found = true
			break
		}
	}
	if !found {
		t.Error("did not find class 'Greeter'")
	}
}

func TestTreeSitterRust(t *testing.T) {
	content := `fn main() {
    println!("Hello, world!");
}

struct Config {
    name: String,
}

impl Config {
    fn new(name: String) -> Self {
        Config { name }
    }
}
`
	chunks, err := File("example.rs", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	found := false
	for _, c := range chunks {
		if c.Symbol == "main" {
			found = true
			if c.Language != "rust" {
				t.Errorf("language = %q, want rust", c.Language)
			}
			break
		}
	}
	if !found {
		t.Error("did not find function 'main'")
	}
}

func TestTreeSitterJavaScript(t *testing.T) {
	content := `function greet(name) {
    return "Hello, " + name + "!";
}

class Greeter {
    constructor(name) {
        this.name = name;
    }
}
`
	chunks, err := File("example.js", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	found := false
	for _, c := range chunks {
		if c.Symbol == "greet" {
			found = true
			if c.Language != "javascript" {
				t.Errorf("language = %q, want javascript", c.Language)
			}
			break
		}
	}
	if !found {
		t.Error("did not find function 'greet'")
	}
}

func TestTreeSitterTypeScript(t *testing.T) {
	content := `interface Config {
    name: string;
}

function greet(config: Config): string {
    return "Hello, " + config.name + "!";
}

type Result = string | number;
`
	chunks, err := File("example.ts", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2", len(chunks))
	}

	foundInterface := false
	foundFunc := false
	for _, c := range chunks {
		if c.Symbol == "Config" {
			foundInterface = true
			if c.Language != "typescript" {
				t.Errorf("language = %q, want typescript", c.Language)
			}
		}
		if c.Symbol == "greet" {
			foundFunc = true
		}
	}
	if !foundInterface {
		t.Error("did not find interface 'Config'")
	}
	if !foundFunc {
		t.Error("did not find function 'greet'")
	}
}

func TestTreeSitterTSX(t *testing.T) {
	content := `import React from 'react'

export function App() {
    return <div>Hello</div>
}
`
	chunks, err := File("app.tsx", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("TSX file produced no chunks")
	}
	found := false
	for _, c := range chunks {
		if c.Symbol == "App" {
			found = true
			if c.Language != "typescript" {
				t.Errorf("language = %q, want typescript", c.Language)
			}
			break
		}
	}
	if !found {
		t.Error("did not find function 'App' in TSX")
	}
}

func TestTreeSitterUnsupportedExtension(t *testing.T) {
	// .txt should fall through to source chunker, not tree-sitter
	content := strings.Repeat("some text line\n", 40)
	chunks, err := File("test.txt", content)
	if err != nil {
		t.Fatal(err)
	}
	// Should get window-type chunks from source chunker
	if len(chunks) == 0 {
		t.Fatal("expected chunks from source chunker")
	}
	if chunks[0].Kind != "window" {
		t.Errorf("kind = %q, want window (source chunker fallback)", chunks[0].Kind)
	}
}

func TestSupportedExtensions(t *testing.T) {
	exts := SupportedExtensions([]string{"go", "python"})
	goFound := false
	pyFound := false
	for _, e := range exts {
		if e == "go" {
			goFound = true
		}
		if e == "py" {
			pyFound = true
		}
	}
	if !goFound {
		t.Error("missing .go extension")
	}
	if !pyFound {
		t.Error("missing .py extension")
	}
}

func TestLangForExt(t *testing.T) {
	if got := LangForExt(".go"); got != "go" {
		t.Errorf("LangForExt(.go) = %q, want go", got)
	}
	if got := LangForExt(".py"); got != "python" {
		t.Errorf("LangForExt(.py) = %q, want python", got)
	}
	if got := LangForExt(".unknown"); got != "" {
		t.Errorf("LangForExt(.unknown) = %q, want empty", got)
	}
}
