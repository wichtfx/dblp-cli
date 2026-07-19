//go:build linux

package lineedit

import (
	"strings"
	"testing"
)

func TestReadEscapeConsumesWholeSequence(t *testing.T) {
	tests := []struct {
		sequence string
		want     byte
	}{
		{"[A", 'A'},
		{"[B", 'B'},
		{"[3~", '~'},
		{"OH", 'H'},
	}
	for _, tt := range tests {
		r := strings.NewReader(tt.sequence)
		got, err := readEscape(r)
		if err != nil {
			t.Fatalf("readEscape(%q): %v", tt.sequence, err)
		}
		if got != tt.want {
			t.Fatalf("readEscape(%q) = %q, want %q", tt.sequence, got, tt.want)
		}
		if r.Len() != 0 {
			t.Fatalf("readEscape(%q) left %d byte(s)", tt.sequence, r.Len())
		}
	}
}
