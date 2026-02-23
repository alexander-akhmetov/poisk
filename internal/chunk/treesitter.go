package chunk

import (
	"context"
	"fmt"
	"strings"

	"github.com/akhmetov/poisk/internal/treesitter/commonlisp"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	typescript "github.com/smacker/go-tree-sitter/typescript/typescript"
)

const maxChunkBytes = 8000

type langSpec struct {
	language   *sitter.Language
	topTypes   map[string]bool
	name       string
	extensions []string
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
		},
	},
	{
		language:   python.GetLanguage(),
		name:       "python",
		extensions: []string{".py"},
		topTypes: map[string]bool{
			"function_definition":  true,
			"class_definition":     true,
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
			"impl_item":     true,
			"trait_item":    true,
		},
	},
	{
		language:   javascript.GetLanguage(),
		name:       "javascript",
		extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		topTypes: map[string]bool{
			"function_declaration": true,
			"class_declaration":    true,
			"lexical_declaration":  true,
			"export_statement":     true,
		},
	},
	{
		language:   typescript.GetLanguage(),
		name:       "typescript",
		extensions: []string{".ts"},
		topTypes: map[string]bool{
			"function_declaration":   true,
			"class_declaration":      true,
			"lexical_declaration":    true,
			"export_statement":       true,
			"interface_declaration":  true,
			"type_alias_declaration": true,
		},
	},
	{
		language:   tsx.GetLanguage(),
		name:       "typescript",
		extensions: []string{".tsx"},
		topTypes: map[string]bool{
			"function_declaration":   true,
			"class_declaration":      true,
			"lexical_declaration":    true,
			"export_statement":       true,
			"interface_declaration":  true,
			"type_alias_declaration": true,
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

	for i := 0; i < int(root.ChildCount()); i++ {
		child := root.Child(i)
		if child == nil {
			continue
		}

		nodeType := child.Type()
		if !spec.topTypes[nodeType] {
			continue
		}

		text := content[child.StartByte():child.EndByte()]
		startLine := int(child.StartPoint().Row) + 1
		endLine := int(child.EndPoint().Row) + 1
		symbol := extractSymbol(child, src, spec.name)

		if len(text) > maxChunkBytes {
			// Split oversized nodes by children
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

	for i := 0; i < childCount; i++ {
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

func extractSymbol(node *sitter.Node, src []byte, lang string) string {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return nameNode.Content(src)
	}

	// For Go type declarations, look for type_spec child
	if lang == "go" && node.Type() == "type_declaration" {
		for i := 0; i < int(node.ChildCount()); i++ {
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
		for i := 0; i < int(node.ChildCount()); i++ {
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
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if nameNode := child.ChildByFieldName("name"); nameNode != nil {
				return nameNode.Content(src)
			}
		}
	}

	// Common Lisp: all top-level forms are list_lit
	if lang == "commonlisp" && node.Type() == "list_lit" {
		// defun is a nested child: list_lit → defun → defun_header → sym_lit
		for i := 0; i < int(node.ChildCount()); i++ {
			child := node.Child(i)
			if child == nil {
				continue
			}
			if child.Type() == "defun" {
				for j := 0; j < int(child.ChildCount()); j++ {
					hdr := child.Child(j)
					if hdr != nil && hdr.Type() == "defun_header" {
						for k := 0; k < int(hdr.ChildCount()); k++ {
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
