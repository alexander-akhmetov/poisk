package chunk

import (
	"context"
	"fmt"
	"strings"

	"github.com/alexander-akhmetov/poisk/internal/treesitter/commonlisp"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

const maxChunkBytes = DefaultMaxInputBytes

type langSpec struct {
	language       *sitter.Language
	topTypes       map[string]bool
	containerTypes map[string]bool // recurse into these (classes, impl blocks)
	innerTypes     map[string]bool // extract these from containers as individual chunks
	name           string
	extensions     []string
}

var languages = []langSpec{
	{
		language:   golang.GetLanguage(),
		name:       "go",
		extensions: []string{".go"},
		topTypes: map[string]bool{
			"function_declaration": true,
			"method_declaration":   true,
			"type_declaration":     true,
			"const_declaration":    true,
			"var_declaration":      true,
		},
	},
	{
		language:   python.GetLanguage(),
		name:       "python",
		extensions: []string{".py"},
		topTypes: map[string]bool{
			"function_definition":  true,
			"decorated_definition": true,
			"assignment":           true,
			"expression_statement": true,
		},
		containerTypes: map[string]bool{
			"class_definition": true,
		},
		innerTypes: map[string]bool{
			"function_definition":  true,
			"decorated_definition": true,
		},
	},
	{
		language:   rust.GetLanguage(),
		name:       "rust",
		extensions: []string{".rs"},
		topTypes: map[string]bool{
			"function_item": true,
			"struct_item":   true,
			"enum_item":     true,
			"const_item":    true,
			"static_item":   true,
			"type_item":     true,
			"mod_item":      true,
		},
		containerTypes: map[string]bool{
			"impl_item":  true,
			"trait_item": true,
		},
		innerTypes: map[string]bool{
			"function_item": true,
			"const_item":    true,
			"type_item":     true,
		},
	},
	{
		language:   javascript.GetLanguage(),
		name:       "javascript",
		extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		topTypes: map[string]bool{
			"function_declaration":           true,
			"generator_function_declaration": true,
			"lexical_declaration":            true,
			"variable_declaration":           true,
			"export_statement":               true,
		},
		containerTypes: map[string]bool{
			"class_declaration": true,
		},
		innerTypes: map[string]bool{
			"method_definition": true,
		},
	},
	{
		language:   typescript.GetLanguage(),
		name:       "typescript",
		extensions: []string{".ts"},
		topTypes: map[string]bool{
			"function_declaration":           true,
			"generator_function_declaration": true,
			"lexical_declaration":            true,
			"variable_declaration":           true,
			"export_statement":               true,
			"interface_declaration":          true,
			"type_alias_declaration":         true,
			"enum_declaration":               true,
		},
		containerTypes: map[string]bool{
			"class_declaration": true,
		},
		innerTypes: map[string]bool{
			"method_definition": true,
		},
	},
	{
		language:   tsx.GetLanguage(),
		name:       "typescript",
		extensions: []string{".tsx"},
		topTypes: map[string]bool{
			"function_declaration":           true,
			"generator_function_declaration": true,
			"lexical_declaration":            true,
			"variable_declaration":           true,
			"export_statement":               true,
			"interface_declaration":          true,
			"type_alias_declaration":         true,
			"enum_declaration":               true,
		},
		containerTypes: map[string]bool{
			"class_declaration": true,
		},
		innerTypes: map[string]bool{
			"method_definition": true,
		},
	},
	{
		language:   commonlisp.GetLanguage(),
		name:       "commonlisp",
		extensions: []string{".lisp", ".cl", ".asd", ".lsp"},
		topTypes: map[string]bool{
			"list_lit": true,
		},
	},
}

var extToLang map[string]*langSpec

func init() {
	extToLang = make(map[string]*langSpec)
	for i := range languages {
		for _, ext := range languages[i].extensions {
			extToLang[ext] = &languages[i]
		}
	}
}

// SupportedLanguages returns the list of language names tree-sitter supports.
func SupportedLanguages() []string {
	names := make([]string, len(languages))
	for i, l := range languages {
		names[i] = l.name
	}
	return names
}

// SupportedExtensions returns file extensions for the given languages.
func SupportedExtensions(langs []string) []string {
	langSet := make(map[string]bool, len(langs))
	for _, l := range langs {
		langSet[strings.ToLower(l)] = true
	}
	var exts []string
	for _, spec := range languages {
		if langSet[spec.name] {
			for _, ext := range spec.extensions {
				exts = append(exts, strings.TrimPrefix(ext, "."))
			}
		}
	}
	return exts
}

// LangForExt returns the language name for a file extension, or empty string.
func LangForExt(ext string) string {
	if spec, ok := extToLang[ext]; ok {
		return spec.name
	}
	return ""
}

func chunkTreeSitter(ext, content string) ([]Chunk, error) {
	spec, ok := extToLang[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported extension: %s", ext)
	}

	parser := sitter.NewParser()
	parser.SetLanguage(spec.language)

	src := []byte(content)
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, fmt.Errorf("tree-sitter parse: %w", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	var chunks []Chunk

	for i := range int(root.ChildCount()) {
		child := root.Child(i)
		if child == nil {
			continue
		}

		nodeType := child.Type()

		// Handle decorated definitions that wrap containers (e.g. Python @decorator on a class)
		if spec.name == "python" && nodeType == "decorated_definition" {
			if isDecoratedContainer(child, spec) {
				subChunks := extractContainerChunks(child, content, src, spec)
				chunks = append(chunks, subChunks...)
				continue
			}
		}

		// Container types → recurse into methods/inner definitions
		if spec.containerTypes[nodeType] {
			subChunks := extractContainerChunks(child, content, src, spec)
			chunks = append(chunks, subChunks...)
			continue
		}

		if !spec.topTypes[nodeType] {
			continue
		}
		if spec.name == "python" && nodeType == "expression_statement" && !hasChildType(child, "assignment") {
			continue
		}

		text := content[child.StartByte():child.EndByte()]
		startLine := int(child.StartPoint().Row) + 1
		endLine := int(child.EndPoint().Row) + 1
		symbol := extractSymbol(child, src, spec.name)

		if len(text) > maxChunkBytes {
			subChunks := splitOversizedNode(child, content, src, spec.name)
			chunks = append(chunks, subChunks...)
		} else if len(strings.TrimSpace(text)) >= minChars {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: startLine,
				EndLine:   endLine,
				Language:  spec.name,
				Kind:      nodeType,
				Symbol:    symbol,
			})
		}
	}

	return chunks, nil
}

func isDecoratedContainer(node *sitter.Node, spec *langSpec) bool {
	for i := range int(node.ChildCount()) {
		child := node.Child(i)
		if child != nil && spec.containerTypes[child.Type()] {
			return true
		}
	}
	return false
}

func extractContainerChunks(container *sitter.Node, content string, src []byte, spec *langSpec) []Chunk {
	// Find the actual container node (may be wrapped in decorated_definition)
	containerNode := container
	if container.Type() == "decorated_definition" {
		for i := range int(container.ChildCount()) {
			child := container.Child(i)
			if child != nil && spec.containerTypes[child.Type()] {
				containerNode = child
				break
			}
		}
	}

	containerSymbol := extractSymbol(containerNode, src, spec.name)

	// Find the body node
	body := containerNode.ChildByFieldName("body")
	if body == nil {
		// Rust impl/trait: try "declaration_list" child
		for i := range int(containerNode.ChildCount()) {
			child := containerNode.Child(i)
			if child != nil && child.Type() == "declaration_list" {
				body = child
				break
			}
		}
	}
	if body == nil {
		body = containerNode
	}

	var chunks []Chunk
	var preamble strings.Builder
	preambleStart := int(container.StartPoint().Row) + 1

	// Include decorator lines in preamble (Python decorated classes)
	if container.Type() == "decorated_definition" && containerNode != container {
		preambleText := content[container.StartByte():containerNode.StartByte()]
		preamble.WriteString(preambleText)
	}

	// Include container signature (everything before the body)
	if body != containerNode {
		sigText := content[containerNode.StartByte():body.StartByte()]
		preamble.WriteString(sigText)
	}

	for i := range int(body.ChildCount()) {
		child := body.Child(i)
		if child == nil {
			continue
		}

		if spec.innerTypes[child.Type()] {
			// Flush preamble before first inner chunk
			if preamble.Len() > 0 {
				text := strings.TrimSpace(preamble.String())
				if len(text) >= minChars {
					chunks = append(chunks, Chunk{
						Text:      text,
						StartLine: preambleStart,
						EndLine:   int(child.StartPoint().Row),
						Language:  spec.name,
						Kind:      containerNode.Type(),
						Symbol:    containerSymbol,
					})
				}
				preamble.Reset()
			}

			innerText := content[child.StartByte():child.EndByte()]
			innerSymbol := extractSymbol(child, src, spec.name)
			qualifiedSymbol := containerSymbol + "." + innerSymbol

			if len(innerText) > maxChunkBytes {
				subChunks := splitOversizedNode(child, content, src, spec.name)
				for j := range subChunks {
					subChunks[j].Symbol = qualifiedSymbol
				}
				chunks = append(chunks, subChunks...)
			} else if len(strings.TrimSpace(innerText)) >= minChars {
				chunks = append(chunks, Chunk{
					Text:      innerText,
					StartLine: int(child.StartPoint().Row) + 1,
					EndLine:   int(child.EndPoint().Row) + 1,
					Language:  spec.name,
					Kind:      child.Type(),
					Symbol:    qualifiedSymbol,
				})
			}
		} else {
			// Accumulate non-inner children into preamble
			childText := content[child.StartByte():child.EndByte()]
			if len(strings.TrimSpace(childText)) > 0 {
				preamble.WriteString(childText)
				// Add gap to next sibling
				if i < int(body.ChildCount())-1 {
					next := body.Child(i + 1)
					if next != nil {
						gap := content[child.EndByte():next.StartByte()]
						preamble.WriteString(gap)
					}
				}
			}
		}
	}

	// Flush remaining preamble (e.g. class with only fields, no methods)
	if preamble.Len() > 0 {
		text := strings.TrimSpace(preamble.String())
		if len(text) >= minChars {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: preambleStart,
				EndLine:   int(container.EndPoint().Row) + 1,
				Language:  spec.name,
				Kind:      containerNode.Type(),
				Symbol:    containerSymbol,
			})
		}
	}

	// If no chunks were extracted (e.g. empty class), emit the whole container
	if len(chunks) == 0 {
		text := content[container.StartByte():container.EndByte()]
		if len(strings.TrimSpace(text)) >= minChars {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: int(container.StartPoint().Row) + 1,
				EndLine:   int(container.EndPoint().Row) + 1,
				Language:  spec.name,
				Kind:      containerNode.Type(),
				Symbol:    containerSymbol,
			})
		}
	}

	return chunks
}

func splitOversizedNode(node *sitter.Node, content string, src []byte, lang string) []Chunk {
	var chunks []Chunk
	childCount := int(node.ChildCount())
	if childCount == 0 {
		text := content[node.StartByte():node.EndByte()]
		if len(strings.TrimSpace(text)) >= minChars {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: int(node.StartPoint().Row) + 1,
				EndLine:   int(node.EndPoint().Row) + 1,
				Language:  lang,
				Kind:      node.Type(),
				Symbol:    extractSymbol(node, src, lang),
			})
		}
		return chunks
	}

	var buf strings.Builder
	bufStart := int(node.StartPoint().Row) + 1
	parentSymbol := extractSymbol(node, src, lang)

	for i := range childCount {
		child := node.Child(i)
		if child == nil {
			continue
		}
		childText := content[child.StartByte():child.EndByte()]

		if buf.Len()+len(childText) > maxChunkBytes && buf.Len() > 0 {
			text := strings.TrimSpace(buf.String())
			if len(text) >= minChars {
				endLine := int(child.StartPoint().Row)
				chunks = append(chunks, Chunk{
					Text:      text,
					StartLine: bufStart,
					EndLine:   endLine,
					Language:  lang,
					Kind:      node.Type(),
					Symbol:    parentSymbol,
				})
			}
			buf.Reset()
			bufStart = int(child.StartPoint().Row) + 1
		}

		buf.WriteString(childText)
		// Add whitespace between children
		if i < childCount-1 {
			next := node.Child(i + 1)
			if next != nil {
				gap := content[child.EndByte():next.StartByte()]
				buf.WriteString(gap)
			}
		}
	}

	if buf.Len() > 0 {
		text := strings.TrimSpace(buf.String())
		if len(text) >= minChars {
			chunks = append(chunks, Chunk{
				Text:      text,
				StartLine: bufStart,
				EndLine:   int(node.EndPoint().Row) + 1,
				Language:  lang,
				Kind:      node.Type(),
				Symbol:    parentSymbol,
			})
		}
	}

	return chunks
}

//nolint:gocyclo
func extractSymbol(node *sitter.Node, src []byte, lang string) string {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return nameNode.Content(src)
	}

	// Rust impl/trait: the type being implemented is in the "type" field
	if lang == "rust" && (node.Type() == "impl_item" || node.Type() == "trait_item") {
		if typeNode := node.ChildByFieldName("type"); typeNode != nil {
			return typeNode.Content(src)
		}
		// trait_item has "name" which we already checked, but fallback to looking for type_identifier
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child != nil && child.Type() == "type_identifier" {
				return child.Content(src)
			}
		}
	}

	if symbol := findNameInDescendants(node, src, 3); symbol != "" {
		return symbol
	}

	// For Go type declarations, look for type_spec child
	if lang == "go" && node.Type() == "type_declaration" {
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child != nil && child.Type() == "type_spec" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					return nameNode.Content(src)
				}
			}
		}
	}

	// For decorated definitions (Python), look inside
	if node.Type() == "decorated_definition" {
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if child.Type() == "function_definition" || child.Type() == "class_definition" {
				if nameNode := child.ChildByFieldName("name"); nameNode != nil {
					return nameNode.Content(src)
				}
			}
		}
	}

	// For export statements, try to find the declaration inside
	if node.Type() == "export_statement" {
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				return nameNode.Content(src)
			}
		}
	}

	if node.Type() == "var_declaration" || node.Type() == "const_declaration" {
		if symbol := findFirstDescendantType(node, src, "identifier", 4); symbol != "" {
			return symbol
		}
	}

	if node.Type() == "assignment" && lang == "python" {
		if symbol := findFirstDescendantType(node, src, "identifier", 3); symbol != "" {
			return symbol
		}
	}
	if node.Type() == "expression_statement" && lang == "python" && hasChildType(node, "assignment") {
		if symbol := findFirstDescendantType(node, src, "identifier", 4); symbol != "" {
			return symbol
		}
	}

	// Common Lisp: all top-level forms are list_lit
	if lang == "commonlisp" && node.Type() == "list_lit" {
		// defun is a nested child: list_lit → defun → defun_header → sym_lit
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if child.Type() == "defun" {
				for j := range int(child.ChildCount()) {
					hdr := child.Child(j)
					if hdr != nil && hdr.Type() == "defun_header" {
						for k := range int(hdr.ChildCount()) {
							sym := hdr.Child(k)
							if sym != nil && sym.Type() == "sym_lit" {
								return sym.Content(src)
							}
						}
					}
				}
				return ""
			}
		}
		// Regular forms: (form-name symbol-name ...)
		// child 0 = "(", child 1 = form name, child 2 = defined symbol
		if int(node.ChildCount()) >= 3 {
			sym := node.Child(2)
			if sym != nil {
				return sym.Content(src)
			}
		}
	}

	return ""
}

func findNameInDescendants(node *sitter.Node, src []byte, maxDepth int) string {
	type queueItem struct {
		node  *sitter.Node
		depth int
	}

	queue := []queueItem{{node: node, depth: 0}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.node == nil || current.depth > maxDepth {
			continue
		}
		if current.depth > 0 {
			if nameNode := current.node.ChildByFieldName("name"); nameNode != nil {
				return strings.TrimSpace(nameNode.Content(src))
			}
		}
		if current.depth == maxDepth {
			continue
		}
		for i := range int(current.node.ChildCount()) {
			queue = append(queue, queueItem{node: current.node.Child(i), depth: current.depth + 1})
		}
	}
	return ""
}

func findFirstDescendantType(node *sitter.Node, src []byte, nodeType string, maxDepth int) string {
	type queueItem struct {
		node  *sitter.Node
		depth int
	}

	queue := []queueItem{{node: node, depth: 0}}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.node == nil || current.depth > maxDepth {
			continue
		}
		if current.depth > 0 && current.node.Type() == nodeType {
			return strings.TrimSpace(current.node.Content(src))
		}
		if current.depth == maxDepth {
			continue
		}
		for i := range int(current.node.ChildCount()) {
			queue = append(queue, queueItem{node: current.node.Child(i), depth: current.depth + 1})
		}
	}
	return ""
}

func hasChildType(node *sitter.Node, nodeType string) bool {
	if node == nil {
		return false
	}
	for i := range int(node.ChildCount()) {
		child := node.Child(i)
		if child != nil && child.Type() == nodeType {
			return true
		}
	}
	return false
}
