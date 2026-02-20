package store

import (
	"math"
	"testing"
)

func TestFloat32sBlobRoundtrip(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
	}{
		{"basic", []float32{1.0, -2.5, 0.0}},
		{"negative", []float32{-1.0, -0.5}},
		{"empty", []float32{}},
		{"single", []float32{3.14}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := Float32sToBlob(tt.vec)
			if len(blob) != len(tt.vec)*4 {
				t.Fatalf("blob length = %d, want %d", len(blob), len(tt.vec)*4)
			}
			got := BlobToFloat32s(blob)
			if len(got) != len(tt.vec) {
				t.Fatalf("got length = %d, want %d", len(got), len(tt.vec))
			}
			for i := range tt.vec {
				if math.Abs(float64(got[i]-tt.vec[i])) > 0.001 {
					t.Errorf("index %d: got %f, want %f", i, got[i], tt.vec[i])
				}
			}
		})
	}
}
