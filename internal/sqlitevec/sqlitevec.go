// Package sqlitevec statically links the sqlite-vec extension.
//
// The amalgamation is vendored here rather than pulled from
// github.com/asg017/sqlite-vec-go-bindings, whose newest tag still ships a
// January 2025 pre-release: poisk needs v0.1.7, which is the first version
// where deleting vectors frees the chunk they lived in.
//
// To upgrade, replace sqlite-vec.c and sqlite-vec.h with the amalgamation from
// the target release and run TestVec0DeleteFreesVectorChunks.
package sqlitevec

// #cgo CFLAGS: -DSQLITE_CORE
// #cgo linux LDFLAGS: -lm
// #include "sqlite-vec.h"
import "C"

// Auto registers sqlite-vec with every SQLite connection opened afterwards,
// through sqlite3_auto_extension.
func Auto() {
	C.sqlite3_auto_extension((*[0]byte)(C.sqlite3_vec_init))
}
