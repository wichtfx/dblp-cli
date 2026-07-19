package bibtex

import (
	"strings"
	"testing"

	"github.com/wichtfx/dblp-cli/internal/dump"
)

func TestRender(t *testing.T) {
	r := dump.Record{
		Type:    "inproceedings",
		Key:     "conf/focs/Wang0001",
		Authors: []string{"Wei Wang 0001", "A & B"},
		Title:   "Cost is 50% of $X.",
		Year:    "2019",
		EE:      []string{"https://doi.org/10.1109/FOCS.2019.00001"},
	}
	// Collapse runs of spaces so assertions don't depend on column alignment.
	out := strings.Join(strings.Fields(Render(r)), " ")

	checks := []string{
		"@inproceedings{DBLP:conf/focs/Wang0001,", // type + dblp-style key
		"author = {Wei Wang and A \\& B}",         // homonym stripped, & escaped
		"title = {Cost is 50\\% of \\$X.}",        // % and $ escaped
		"doi = {10.1109/FOCS.2019.00001}",         // doi extracted from ee
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("rendered entry missing %q\n---\n%s", c, out)
		}
	}
}

func TestRenderManyDedup(t *testing.T) {
	a := dump.Record{Type: "article", Key: "x/1", Title: "A"}
	out := RenderMany([]dump.Record{a, a})
	if n := strings.Count(out, "@article"); n != 1 {
		t.Errorf("duplicate key not de-duplicated: got %d entries", n)
	}
}
