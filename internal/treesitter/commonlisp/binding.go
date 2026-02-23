package commonlisp

//#include "tree_sitter/parser.h"
//TSLanguage *tree_sitter_commonlisp(void);
import "C"
import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_commonlisp())
	return sitter.NewLanguage(ptr)
}
