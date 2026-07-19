package dump

import (
	"os"
	"path/filepath"
	"testing"
)

// sample mirrors real dblp.xml: ISO-8859-1 declaration, a DOCTYPE referencing
// the private DTD, and non-ASCII expressed as HTML entities.
const sample = `<?xml version="1.0" encoding="ISO-8859-1"?>
<!DOCTYPE dblp SYSTEM "dblp.dtd">
<dblp>
<article key="journals/x/A1">
<author>Wei Wang 0001</author>
<author>J&uuml;rgen Schmidh&uuml;ber</author>
<title>&Uuml;ber die Komplexit&auml;t.</title>
<journal>J. X</journal>
<year>2019</year>
</article>
<www key="homepages/x/W">
<title>Home Page</title>
</www>
</dblp>`

func TestParseEntitiesAndCharset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.xml")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}

	var recs []Record
	if err := Parse(path, func(r Record) error { recs = append(recs, r); return nil }); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}

	a := recs[0]
	if a.Type != "article" || a.Key != "journals/x/A1" {
		t.Errorf("unexpected type/key: %q %q", a.Type, a.Key)
	}
	// HTML entities must decode to UTF-8.
	if a.Title != "Über die Komplexität." {
		t.Errorf("title not decoded: %q", a.Title)
	}
	// Homonym suffix must be stripped on clean access only.
	if got := a.CleanAuthors(); got[0] != "Wei Wang" || got[1] != "Jürgen Schmidhüber" {
		t.Errorf("clean authors = %v", got)
	}
	if a.Authors[0] != "Wei Wang 0001" {
		t.Errorf("raw author should keep suffix: %q", a.Authors[0])
	}
	if recs[1].Type != "www" {
		t.Errorf("second record type = %q, want www", recs[1].Type)
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"Wei Wang 0001":   "Wei Wang",
		"John Smith":      "John Smith",
		"X 12":            "X 12", // only 4-digit suffixes are stripped
		"Year 2019 Study": "Year 2019 Study",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsHomepage(t *testing.T) {
	if !((Record{Type: "www", Key: "homepages/w/WeiWang1"}).IsHomepage()) {
		t.Fatal("author homepage was not recognized")
	}
	if (Record{Type: "www", Key: "tr/misc/Report"}).IsHomepage() {
		t.Fatal("ordinary www record was recognized as an author homepage")
	}
}
