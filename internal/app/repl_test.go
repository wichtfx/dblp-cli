package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/lineedit"
	"github.com/wichtfx/dblp-cli/internal/store"
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

func TestFacetAuthorReadsYearWithoutAuthorNumber(t *testing.T) {
	record := dump.Record{Authors: []string{"Ada Author", "Ben Author"}, Year: "2024"}
	tests := []struct {
		rest, author, year string
	}{
		{"2020", "Ada Author", "2020"},
		{"y", "Ada Author", "2024"},
		{"2 2020", "Ben Author", "2020"},
		{"", "Ada Author", ""},
	}
	for _, tt := range tests {
		q, ok := facetAuthor(&record, tt.rest)
		if !ok || q.author != tt.author || q.year != tt.year {
			t.Fatalf("facetAuthor(%q) = %#v, %v; want author %q, year %q", tt.rest, q, ok, tt.author, tt.year)
		}
	}
	if _, ok := facetAuthor(&record, "3"); ok {
		t.Fatal("facetAuthor accepted author 3 of 2")
	}
}

// testStore indexes two records, the first with a DOI link, and returns the
// store and a search page holding both.
func testStore(t *testing.T) (*store.Store, []store.Result) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "dblp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []dump.Record{
		{Type: "article", Key: "journals/test/One", Authors: []string{"Ada Author"}, Title: "First Record.", Journal: "Test", Year: "2025", EE: []string{"https://doi.org/10.1/one"}},
		{Type: "article", Key: "journals/test/Two", Authors: []string{"Ben Author"}, Title: "Second Record.", Journal: "Test", Year: "2026", URL: "db/journals/test/test1.html#Two"},
	} {
		if err := b.Add(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	res, err := st.Search(context.Background(), "record", pageSize, 0)
	if err != nil || len(res) != 2 {
		t.Fatalf("Search = %v, %v; want 2 results", res, err)
	}
	return st, res
}

func TestOpenUsesSelectionOrNumbers(t *testing.T) {
	st, res := testStore(t)
	var opened []string
	saved := openURL
	openURL = func(url string) error { opened = append(opened, url); return nil }
	t.Cleanup(func() { openURL = saved })

	sel, _, _ := st.Get(context.Background(), "journals/test/One")
	doOpen(context.Background(), st, res, "", &sel, 0)
	doOpen(context.Background(), st, res, "all", nil, 0)
	doOpen(context.Background(), st, res, "", nil, 0)

	want := []string{"https://doi.org/10.1/one"}
	for _, r := range res {
		rec, _, _ := st.Get(context.Background(), r.Key)
		want = append(want, rec.Link())
	}
	if strings.Join(opened, " ") != strings.Join(want, " ") {
		t.Fatalf("opened %v, want %v", opened, want)
	}
	if !slices.Contains(opened, "https://dblp.org/rec/journals/test/Two.html") {
		t.Fatalf("opened %v, want the dblp page of a record without a link", opened)
	}
}

func TestWriteAppendsSelectionToLastFile(t *testing.T) {
	st, res := testStore(t)
	file := filepath.Join(t.TempDir(), "refs.bib")
	one, _, _ := st.Get(context.Background(), "journals/test/One")
	two, _, _ := st.Get(context.Background(), "journals/test/Two")

	if _, ok := doWrite(context.Background(), st, res, "", "", &one, 0); ok {
		t.Fatal("w without a file and without a previous file succeeded")
	}
	got, ok := doWrite(context.Background(), st, res, file, "", &one, 0)
	if !ok || got != file {
		t.Fatalf("w %s = %q, %v", file, got, ok)
	}
	if got, ok = doWrite(context.Background(), st, res, "", file, &two, 0); !ok || got != file {
		t.Fatalf("w = %q, %v; want the previous file", got, ok)
	}
	if _, ok = doWrite(context.Background(), st, res, "", file, nil, 0); ok {
		t.Fatal("w without a selection and without numbers succeeded")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := bibtex.Render(one) + "\n" + bibtex.Render(two); string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}
}

// scriptedReader returns its lines in order, a nil entry as a Ctrl-C, and
// then the end of input.
type scriptedReader struct {
	lines []*string
	reads int
}

func (r *scriptedReader) ReadLine(string) (string, bool, error) {
	if r.reads == len(r.lines) {
		return "", false, nil
	}
	l := r.lines[r.reads]
	r.reads++
	if l == nil {
		return "", true, lineedit.ErrInterrupt
	}
	return *l, true, nil
}

func script(lines ...any) *scriptedReader {
	r := &scriptedReader{}
	for _, l := range lines {
		if s, ok := l.(string); ok {
			r.lines = append(r.lines, &s)
		} else {
			r.lines = append(r.lines, nil)
		}
	}
	return r
}

func TestSecondCtrlCInARowQuits(t *testing.T) {
	st, _ := testStore(t)
	ctrlC := struct{}{}
	in := script(ctrlC, ctrlC, "h")
	if err := repl(st, in); err != nil || in.reads != 2 {
		t.Fatalf("repl = %v after %d reads, want quit after 2", err, in.reads)
	}
	in = script(ctrlC, "h", ctrlC, "h")
	if err := repl(st, in); err != nil || in.reads != 4 {
		t.Fatalf("repl = %v after %d reads, want all 4", err, in.reads)
	}
}
