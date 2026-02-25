package ports_test

import (
	"github.com/alexander-akhmetov/poisk/internal/ports"
	"github.com/alexander-akhmetov/poisk/internal/store"
)

// Compile-time interface check — ensures the concrete type satisfies the port
// interface without needing an adapter wrapper.

var _ ports.ChunkStore = (*store.Store)(nil)
