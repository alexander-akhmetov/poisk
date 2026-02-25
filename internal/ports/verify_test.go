package ports_test

import (
	"github.com/alexander-akhmetov/poisk/internal/embed"
	"github.com/alexander-akhmetov/poisk/internal/ports"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

// Compile-time interface checks — these ensure that concrete types satisfy
// their respective port interfaces without needing adapter wrappers.

var _ ports.Embedder = (*embed.Client)(nil)
var _ ports.ChunkStore = (*store.Store)(nil)
