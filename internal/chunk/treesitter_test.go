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
	var syms []string
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
