package dump

import "testing"

func TestLinkIsAbsolute(t *testing.T) {
	tests := []struct {
		name string
		r    Record
		want string
	}{
		{"electronic edition", Record{Key: "journals/x/A1", EE: []string{"https://doi.org/10.1/x"}, URL: "db/journals/x/x1.html#A1"}, "https://doi.org/10.1/x"},
		{"relative url", Record{Key: "journals/x/A1", URL: "db/journals/x/x1.html#A1"}, "https://dblp.org/rec/journals/x/A1.html"},
		{"absolute url", Record{Key: "homepages/x/A", URL: "https://example.org/~a"}, "https://example.org/~a"},
		{"no key", Record{}, ""},
	}
	for _, tt := range tests {
		if got := tt.r.Link(); got != tt.want {
			t.Errorf("%s: Link() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
