package chunk

import (
	"fmt"
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

func TestTreeSitterGoValueDeclarations(t *testing.T) {
	content := `package example

const DefaultName = "poisk"
var defaultPort = 8080
`
	chunks, err := File("example.go", content)
	if err != nil {
		t.Fatal(err)
	}

	foundConst := false
	foundVar := false
	for _, c := range chunks {
		if c.Symbol == "DefaultName" {
			foundConst = true
			if c.Kind != "const_declaration" {
				t.Errorf("const kind = %q, want const_declaration", c.Kind)
			}
		}
		if c.Symbol == "defaultPort" {
			foundVar = true
			if c.Kind != "var_declaration" {
				t.Errorf("var kind = %q, want var_declaration", c.Kind)
			}
		}
	}
	if !foundConst {
		t.Error("did not find const declaration symbol 'DefaultName'")
	}
	if !foundVar {
		t.Error("did not find var declaration symbol 'defaultPort'")
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

	// Class methods should now be individual chunks with qualified symbols
	foundInit := false
	foundGreetMethod := false
	for _, c := range chunks {
		if c.Symbol == "Greeter.__init__" {
			foundInit = true
		}
		if c.Symbol == "Greeter.greet" {
			foundGreetMethod = true
		}
	}
	if !foundInit {
		t.Errorf("did not find 'Greeter.__init__', got: %v", chunkSymbols(chunks))
	}
	if !foundGreetMethod {
		t.Errorf("did not find 'Greeter.greet', got: %v", chunkSymbols(chunks))
	}
}

func TestTreeSitterPythonAssignment(t *testing.T) {
	content := `DEFAULT_TIMEOUT = 30

def greet():
    return DEFAULT_TIMEOUT
`
	chunks, err := File("example.py", content)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, c := range chunks {
		if c.Symbol == "DEFAULT_TIMEOUT" {
			found = true
			if c.Kind != "expression_statement" {
				t.Errorf("kind = %q, want expression_statement", c.Kind)
			}
			break
		}
	}
	if !found {
		t.Error("did not find assignment symbol 'DEFAULT_TIMEOUT'")
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

func TestTreeSitterRustConstAndModule(t *testing.T) {
	content := `const MAX_RETRIES: usize = 3;

mod parser {
    pub fn run() {}
}
`
	chunks, err := File("example.rs", content)
	if err != nil {
		t.Fatal(err)
	}

	foundConst := false
	foundMod := false
	for _, c := range chunks {
		if c.Symbol == "MAX_RETRIES" {
			foundConst = true
			if c.Kind != "const_item" {
				t.Errorf("const kind = %q, want const_item", c.Kind)
			}
		}
		if c.Symbol == "parser" {
			foundMod = true
			if c.Kind != "mod_item" {
				t.Errorf("mod kind = %q, want mod_item", c.Kind)
			}
		}
	}
	if !foundConst {
		t.Error("did not find rust const symbol 'MAX_RETRIES'")
	}
	if !foundMod {
		t.Error("did not find rust module symbol 'parser'")
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

func TestTreeSitterJavaScriptVariableDeclaration(t *testing.T) {
	content := `var fallbackValue = 42;
const cacheKey = "main";
`
	chunks, err := File("example.js", content)
	if err != nil {
		t.Fatal(err)
	}

	foundVar := false
	foundConst := false
	for _, c := range chunks {
		if c.Symbol == "fallbackValue" {
			foundVar = true
			if c.Kind != "variable_declaration" {
				t.Errorf("var kind = %q, want variable_declaration", c.Kind)
			}
		}
		if c.Symbol == "cacheKey" {
			foundConst = true
			if c.Kind != "lexical_declaration" {
				t.Errorf("const kind = %q, want lexical_declaration", c.Kind)
			}
		}
	}
	if !foundVar {
		t.Error("did not find javascript var symbol 'fallbackValue'")
	}
	if !foundConst {
		t.Error("did not find javascript const symbol 'cacheKey'")
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

func TestTreeSitterTypeScriptEnumDeclaration(t *testing.T) {
	content := `enum Mode {
  Fast,
  Slow
}
`
	chunks, err := File("example.ts", content)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, c := range chunks {
		if c.Symbol == "Mode" {
			found = true
			if c.Kind != "enum_declaration" {
				t.Errorf("kind = %q, want enum_declaration", c.Kind)
			}
			break
		}
	}
	if !found {
		t.Error("did not find enum declaration symbol 'Mode'")
	}
}

func TestPythonClassMethods(t *testing.T) {
	content := `class Greeter:
    def __init__(self, name):
        self.name = name

    def greet(self):
        return f"Hello, {self.name}!"
`
	chunks, err := File("example.py", content)
	if err != nil {
		t.Fatal(err)
	}

	foundInit := false
	foundGreet := false
	for _, c := range chunks {
		if c.Symbol == "Greeter.__init__" {
			foundInit = true
			if c.Kind != "function_definition" {
				t.Errorf("__init__ kind = %q, want function_definition", c.Kind)
			}
			if c.Language != "python" {
				t.Errorf("language = %q, want python", c.Language)
			}
		}
		if c.Symbol == "Greeter.greet" {
			foundGreet = true
			if c.Kind != "function_definition" {
				t.Errorf("greet kind = %q, want function_definition", c.Kind)
			}
		}
	}
	if !foundInit {
		t.Errorf("did not find chunk with symbol 'Greeter.__init__', got chunks: %v", chunkSymbols(chunks))
	}
	if !foundGreet {
		t.Errorf("did not find chunk with symbol 'Greeter.greet', got chunks: %v", chunkSymbols(chunks))
	}
}

func TestPythonDecoratedClassMethods(t *testing.T) {
	content := `@dataclass
class Config:
    name: str = "default"

    def validate(self):
        return len(self.name) > 0
`
	chunks, err := File("example.py", content)
	if err != nil {
		t.Fatal(err)
	}

	foundValidate := false
	for _, c := range chunks {
		if c.Symbol == "Config.validate" {
			foundValidate = true
			break
		}
	}
	if !foundValidate {
		t.Errorf("did not find 'Config.validate' in decorated class, got: %v", chunkSymbols(chunks))
	}
}

func TestRustImplMethods(t *testing.T) {
	content := `struct Config {
    name: String,
}

impl Config {
    fn new(name: String) -> Self {
        Config { name }
    }

    fn validate(&self) -> bool {
        !self.name.is_empty()
    }
}
`
	chunks, err := File("example.rs", content)
	if err != nil {
		t.Fatal(err)
	}

	foundNew := false
	foundValidate := false
	for _, c := range chunks {
		if c.Symbol == "Config.new" {
			foundNew = true
			if c.Kind != "function_item" {
				t.Errorf("new kind = %q, want function_item", c.Kind)
			}
		}
		if c.Symbol == "Config.validate" {
			foundValidate = true
		}
	}
	if !foundNew {
		t.Errorf("did not find 'Config.new', got: %v", chunkSymbols(chunks))
	}
	if !foundValidate {
		t.Errorf("did not find 'Config.validate', got: %v", chunkSymbols(chunks))
	}

	// struct should still be a separate chunk
	foundStruct := false
	for _, c := range chunks {
		if c.Symbol == "Config" && c.Kind == "struct_item" {
			foundStruct = true
			break
		}
	}
	if !foundStruct {
		t.Errorf("did not find struct 'Config', got: %v", chunkSymbols(chunks))
	}
}

func TestJSClassMethods(t *testing.T) {
	content := `class Greeter {
    constructor(name) {
        this.name = name;
    }

    greet() {
        return "Hello, " + this.name + "!";
    }
}
`
	chunks, err := File("example.js", content)
	if err != nil {
		t.Fatal(err)
	}

	foundConstructor := false
	foundGreet := false
	for _, c := range chunks {
		if c.Symbol == "Greeter.constructor" {
			foundConstructor = true
			if c.Kind != "method_definition" {
				t.Errorf("constructor kind = %q, want method_definition", c.Kind)
			}
		}
		if c.Symbol == "Greeter.greet" {
			foundGreet = true
		}
	}
	if !foundConstructor {
		t.Errorf("did not find 'Greeter.constructor', got: %v", chunkSymbols(chunks))
	}
	if !foundGreet {
		t.Errorf("did not find 'Greeter.greet', got: %v", chunkSymbols(chunks))
	}
}

func chunkSymbols(chunks []Chunk) []string {
	syms := make([]string, 0, len(chunks))
	for _, c := range chunks {
		syms = append(syms, c.Symbol+"("+c.Kind+")")
	}
	return syms
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

func TestTreeSitterCommonLisp(t *testing.T) {
	content := `(defun greet (name)
  "Greet someone."
  (format t "Hello, ~a!" name))

(defclass person ()
  ((name :initarg :name :accessor person-name)
   (age  :initarg :age  :accessor person-age)))

(defvar *greeting* "Hello")

(in-package :my-app)
`
	chunks, err := File("example.lisp", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 3 {
		t.Fatalf("got %d chunks, want >= 3", len(chunks))
	}

	type want struct {
		symbol string
		lang   string
	}
	wants := []want{
		{"greet", "commonlisp"},
		{"person", "commonlisp"},
		{"*greeting*", "commonlisp"},
	}

	for _, w := range wants {
		found := false
		for _, c := range chunks {
			if c.Symbol == w.symbol {
				found = true
				if c.Language != w.lang {
					t.Errorf("symbol %q: language = %q, want %q", w.symbol, c.Language, w.lang)
				}
				if c.Kind != "list_lit" {
					t.Errorf("symbol %q: kind = %q, want list_lit", w.symbol, c.Kind)
				}
				if c.StartLine == 0 || c.EndLine == 0 {
					t.Errorf("symbol %q: missing line range", w.symbol)
				}
				break
			}
		}
		if !found {
			t.Errorf("did not find chunk with symbol %q", w.symbol)
		}
	}

	// Verify in-package is also captured (symbol = ":my-app")
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Text, "in-package") {
			found = true
			break
		}
	}
	if !found {
		t.Error("did not find in-package form")
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

func TestOversizedNode(t *testing.T) {
	// Build a Go const block exceeding maxChunkBytes (8000).
	// Each const_spec is a separate child node, so splitOversizedNode
	// can split at child boundaries.
	var sb strings.Builder
	sb.WriteString("package example\n\nconst (\n")
	for i := range 200 {
		// ~50 bytes per spec × 200 = ~10000 bytes
		sb.WriteString("\tC")
		sb.WriteString(strings.Repeat("x", 5))
		sb.WriteString(fmt.Sprintf("%03d", i))
		sb.WriteString(" = \"")
		sb.WriteString(strings.Repeat("a", 30))
		sb.WriteString("\"\n")
	}
	sb.WriteString(")\n")

	content := sb.String()
	constStart := strings.Index(content, "const")
	constBlock := content[constStart:]
	if len(constBlock) <= maxChunkBytes {
		t.Fatalf("test fixture too small: %d bytes, need > %d", len(constBlock), maxChunkBytes)
	}

	chunks, err := File("big.go", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2 (oversized node should be split)", len(chunks))
	}

	for _, c := range chunks {
		if c.Language != "go" {
			t.Errorf("language = %q, want go", c.Language)
		}
		if c.Kind != "const_declaration" {
			t.Errorf("kind = %q, want const_declaration", c.Kind)
		}
		if c.StartLine == 0 || c.EndLine == 0 {
			t.Error("missing line range")
		}
	}
}

func TestExtractSymbolVariants(t *testing.T) {
	cases := []struct {
		name       string
		file       string
		content    string
		wantSymbol string
		wantKind   string
		wantLang   string
	}{
		{
			name: "go_type_decl",
			file: "example.go",
			content: `package example

type MyStruct struct {
	Field1 string
	Field2 int
}
`,
			wantSymbol: "MyStruct",
			wantKind:   "type_declaration",
			wantLang:   "go",
		},
		{
			name: "go_const_block",
			file: "example.go",
			content: `package example

const (
	Alpha = "alpha"
	Beta  = "beta"
	Gamma = "gamma"
)
`,
			wantSymbol: "Alpha",
			wantKind:   "const_declaration",
			wantLang:   "go",
		},
		{
			name: "python_decorated_func",
			file: "example.py",
			content: `@staticmethod
def compute(x):
    return x * 2
`,
			wantSymbol: "compute",
			wantKind:   "decorated_definition",
			wantLang:   "python",
		},
		{
			name: "js_arrow_const",
			file: "example.js",
			content: `const greet = (name) => {
    return "Hello, " + name;
};
`,
			wantSymbol: "greet",
			wantKind:   "lexical_declaration",
			wantLang:   "javascript",
		},
		{
			name: "rust_impl_type",
			file: "example.rs",
			content: `struct Server {
    port: u16,
}

impl Server {
    fn start(&self) {
        println!("starting on {}", self.port);
    }
}
`,
			wantSymbol: "Server",
			wantKind:   "struct_item",
			wantLang:   "rust",
		},
		{
			name: "ts_export_function",
			file: "example.ts",
			content: `export function handleRequest(req: Request): Response {
    return new Response("ok");
}
`,
			wantSymbol: "handleRequest",
			wantKind:   "export_statement",
			wantLang:   "typescript",
		},
		{
			name: "rust_trait",
			file: "example.rs",
			content: `trait Drawable {
    fn draw(&self);
    fn area(&self) -> f64;
}
`,
			wantSymbol: "Drawable",
			wantKind:   "trait_item",
			wantLang:   "rust",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks, err := File(tc.file, tc.content)
			if err != nil {
				t.Fatal(err)
			}

			found := false
			for _, c := range chunks {
				if c.Symbol == tc.wantSymbol {
					found = true
					if c.Kind != tc.wantKind {
						t.Errorf("kind = %q, want %q", c.Kind, tc.wantKind)
					}
					if c.Language != tc.wantLang {
						t.Errorf("language = %q, want %q", c.Language, tc.wantLang)
					}
					break
				}
			}
			if !found {
				t.Errorf("did not find symbol %q, got: %v", tc.wantSymbol, chunkSymbols(chunks))
			}
		})
	}
}

func TestSplitLargeMarkdownSection(t *testing.T) {
	// Build markdown with nested headings (to produce a heading path >= 20 chars)
	// followed by >8000 bytes of continuous text. The text has no blank-line
	// paragraph breaks, so it accumulates into a single flush() call that
	// exceeds maxSectionChars and triggers splitLargeSection.
	//
	// splitLargeSection splits on "\n\n". The only "\n\n" comes from the
	// heading path being prepended: "headingPath\n\ncontent". A sufficiently
	// long heading path (>= 20 chars) ensures the first split part is emitted
	// as a chunk, producing at least 2 chunks total.
	var sb strings.Builder
	sb.WriteString("# Architecture Guide\n\n")
	sb.WriteString("## Storage Layer Details\n\n")
	// 150 lines × ~75 chars = ~11250 chars of continuous text
	for range 150 {
		sb.WriteString(strings.Repeat("word ", 15))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	content := sb.String()
	chunks, err := File("big.md", content)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want >= 2 (large section should be split), got: %v",
			len(chunks), chunkSummary(chunks))
	}
	for _, c := range chunks {
		if c.Language != "markdown" {
			t.Errorf("language = %q, want markdown", c.Language)
		}
		if c.Kind != "paragraph" {
			t.Errorf("kind = %q, want paragraph (split section)", c.Kind)
		}
	}
}

func chunkSummary(chunks []Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, fmt.Sprintf("{kind=%s len=%d symbol=%q}", c.Kind, len(c.Text), c.Symbol))
	}
	return out
}
