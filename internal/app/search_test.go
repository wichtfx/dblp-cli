package app

import (
	"os"
	"path/filepath"
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

func TestAppendBibtexKeepsEarlierEntries(t *testing.T) {
	file := filepath.Join(t.TempDir(), "refs.bib")
	if err := appendBibtex("@article{DBLP:a,\n}\n", file, 1); err != nil {
		t.Fatal(err)
	}
	if err := appendBibtex("@article{DBLP:b,\n}\n", file, 1); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := "@article{DBLP:a,\n}\n\n@article{DBLP:b,\n}\n"; string(got) != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
}
