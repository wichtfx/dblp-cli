package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/check"
	"github.com/wichtfx/dblp-cli/internal/dump"
)

func TestReport(t *testing.T) {
	results := []check.Result{
		{Entry: bibtex.Entry{Key: "dijkstra68"}, Status: check.OK, Record: dump.Record{Key: "journals/cacm/Dijkstra68a"}},
		{Entry: bibtex.Entry{Key: "fake"}, Status: check.NotFound, Note: "no dblp record has this title"},
	}
	var out bytes.Buffer
	err := report(&out, results)
	if err == nil || !strings.Contains(err.Error(), "1 entries") {
		t.Errorf("report error = %v", err)
	}
	want := "ok         dijkstra68  journals/cacm/Dijkstra68a\n" +
		"not found  fake        no dblp record has this title\n" +
		"2 entries: 1 ok, 0 differ, 1 not found\n"
	if out.String() != want {
		t.Errorf("report printed\n%s\nwant\n%s", out.String(), want)
	}
	if err := report(&out, results[:1]); err != nil {
		t.Errorf("report of a clean file = %v", err)
	}
}
