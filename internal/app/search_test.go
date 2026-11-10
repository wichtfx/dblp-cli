package app

import (
	"strings"
	"testing"

	"github.com/wichtfx/dblp-cli/internal/store"
)

func TestMetaLineKeepsVenueAndYearForLongAuthorLists(t *testing.T) {
	r := store.Result{
		Authors: strings.Repeat("Ada Author, ", 30),
		Venue:   "Commun. ACM",
		Year:    "1968",
	}
	line := metaLine(r, 110)
	if !strings.Contains(line, "Commun. ACM") || !strings.Contains(line, "(1968)") {
		t.Fatalf("metaLine dropped the venue or the year: %q", line)
	}
	if !strings.Contains(line, "…") {
		t.Fatalf("metaLine did not shorten the authors: %q", line)
	}
}
