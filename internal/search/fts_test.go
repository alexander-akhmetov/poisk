package search

import "testing"

func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", `"hello" AND "world"`},
		{"CFR-1234!", `"CFR" AND "1234"`},
		{"my_var_name", `"my_var_name"`},
		{"", ""},
		{"!@#$%", ""},
		{"single", `"single"`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := buildFTSQuery(tt.input)
			if got != tt.want {
				t.Errorf("buildFTSQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
