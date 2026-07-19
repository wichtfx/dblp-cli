package app

import (
	"testing"

	"github.com/wichtfx/dblp-cli/internal/dump"
)

func TestFacetAuthorKeepsRawIdentity(t *testing.T) {
	record := dump.Record{
		Authors: []string{"Alex Smith 0001", "Alex Smith 0002"},
		Year:    "2024",
	}
	query, ok := facetAuthor(&record, "2")
	if !ok {
		t.Fatal("facetAuthor rejected a valid author number")
	}
	if query.author != "Alex Smith 0002" {
		t.Fatalf("author = %q, want raw DBLP identity", query.author)
	}
	if query.label != "author: Alex Smith" {
		t.Fatalf("label = %q", query.label)
	}
}

func TestResultNumberUsesPageOffset(t *testing.T) {
	tests := []struct {
		input  string
		n      int
		offset int
		want   int
		ok     bool
	}{
		{input: "1", n: 15, offset: 0, want: 0, ok: true},
		{input: "16", n: 15, offset: 15, want: 0, ok: true},
		{input: "30", n: 15, offset: 15, want: 14, ok: true},
		{input: "15", n: 15, offset: 15, ok: false},
		{input: "31", n: 15, offset: 15, ok: false},
	}
	for _, tt := range tests {
		got, ok := asResultIndex(tt.input, tt.n, tt.offset)
		if ok != tt.ok || got != tt.want {
			t.Errorf("asResultIndex(%q, %d, %d) = (%d, %v), want (%d, %v)",
				tt.input, tt.n, tt.offset, got, ok, tt.want, tt.ok)
		}
	}
}

func TestSelectionUsesDisplayedNumbers(t *testing.T) {
	got := parseSelection("16 18 30", 15, 15)
	want := []int{0, 2, 14}
	if len(got) != len(want) {
		t.Fatalf("parseSelection returned %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseSelection returned %v, want %v", got, want)
		}
	}
}
