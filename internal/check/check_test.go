package check

import (
	"testing"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/store"
)

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		`{Go To} Statement Considered Harmful.`: "go to statement considered harmful",
		`J{\"u}rgen Schmidh\"{u}ber`:            "jurgen schmidhuber",
		`Jürgen Schmidhüber`:                    "jurgen schmidhuber",
		`Ko{\c{c}}, {\'{\i}}{\ss}`:              "koc iss",
		`\emph{Zero}-Knowledge Proofs`:          "zero knowledge proofs",
		`Paweł Łukasz`:                          "pawel lukasz",
	}
	for in, want := range tests {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/dblp.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []dump.Record{
		{Type: "article", Key: "journals/cacm/Dijkstra68a", Title: "Go To Statement Considered Harmful.",
			Authors: []string{"Edsger W. Dijkstra"}, Year: "1968", EE: []string{"https://doi.org/10.1145/362929.362947"}},
		{Type: "inproceedings", Key: "conf/nips/VaswaniSPUJGKP17", Title: "Attention is All you Need.",
			Authors: []string{"Ashish Vaswani", "Noam Shazeer", "Niki Parmar"}, Year: "2017"},
		{Type: "article", Key: "journals/corr/VaswaniSPUJGKP17", Title: "Attention Is All You Need.",
			Authors: []string{"Ashish Vaswani", "Noam Shazeer", "Niki Parmar"}, Year: "2017"},
		{Type: "article", Key: "journals/x/Schmidhuber15", Title: "Deep Learning in Neural Networks: An Overview.",
			Authors: []string{"Jürgen Schmidhuber"}, Year: "2015"},
	} {
		if err := b.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestEntry(t *testing.T) {
	st := testStore(t)
	tests := []struct {
		name   string
		entry  bibtex.Entry
		status Status
		key    string
	}{
		{"dblp key", bibtex.Entry{Key: "DBLP:journals/cacm/Dijkstra68a", Fields: map[string]string{
			"title": "{Go To} Statement Considered Harmful", "author": "Dijkstra, Edsger W.", "year": "1968"}},
			OK, "journals/cacm/Dijkstra68a"},
		{"doi", bibtex.Entry{Key: "dijkstra", Fields: map[string]string{
			"doi": "10.1145/362929.362947", "year": "1969"}},
			Differs, "journals/cacm/Dijkstra68a"},
		{"escaped doi", bibtex.Entry{Key: "DBLP:journals/cacm/Dijkstra68a", Fields: map[string]string{
			"doi": `{10.1145/362929.362947}`, "author": `Edsger W. Dijkstra`}},
			OK, "journals/cacm/Dijkstra68a"},
		{"escaped doi lookup", bibtex.Entry{Key: "dijkstra3", Fields: map[string]string{
			"doi": `10.1145/362929\_362947`}},
			NotFound, ""},
		{"doi again", bibtex.Entry{Key: "dijkstra", Fields: map[string]string{
			"doi": "10.1145/362929.362947", "year": "1969"}},
			Differs, "journals/cacm/Dijkstra68a"},
		{"title prefers the published version", bibtex.Entry{Key: "vaswani", Fields: map[string]string{
			"title": "Attention is all you need", "author": "Vaswani, Ashish and others", "year": "2017"}},
			OK, "conf/nips/VaswaniSPUJGKP17"},
		{"accents", bibtex.Entry{Key: "schmidhuber", Fields: map[string]string{
			"title": "Deep learning in neural networks: {A}n overview", "author": `J{\"u}rgen Schmidhuber`, "year": "2015"}},
			OK, "journals/x/Schmidhuber15"},
		{"names across lines", bibtex.Entry{Key: "DBLP:conf/nips/VaswaniSPUJGKP17", Fields: map[string]string{
			"author": "Ashish Vaswani and\n    Noam Shazeer and\n\tNiki Parmar"}},
			OK, "conf/nips/VaswaniSPUJGKP17"},
		{"other version by title", bibtex.Entry{Key: "dijkstra2", Fields: map[string]string{
			"title": "Go To Statement Considered Harmful", "doi": "10.9999/reprint"}},
			Differs, "journals/cacm/Dijkstra68a"},
		{"wrong author", bibtex.Entry{Key: "vaswani2", Fields: map[string]string{
			"title": "Attention is all you need", "author": "Ashish Vaswani and Ian Goodfellow and Niki Parmar"}},
			Differs, "conf/nips/VaswaniSPUJGKP17"},
		{"invented", bibtex.Entry{Key: "fake", Fields: map[string]string{
			"title": "A Paper That Does Not Exist", "year": "2020"}},
			NotFound, ""},
		{"unknown dblp key", bibtex.Entry{Key: "DBLP:conf/x/None", Fields: map[string]string{}}, NotFound, ""},
	}
	for _, tt := range tests {
		res, err := Entry(st, tt.entry)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if res.Status != tt.status || res.Record.Key != tt.key {
			t.Errorf("%s: status %s, record %q, diffs %q; want %s, %q", tt.name, res.Status, res.Record.Key, res.Diffs, tt.status, tt.key)
		}
	}
}
