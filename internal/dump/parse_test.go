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

func parseString(t *testing.T, xml string) []Record {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dblp.xml")
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	var recs []Record
	if err := Parse(path, func(r Record) error { recs = append(recs, r); return nil }); err != nil {
		t.Fatal(err)
	}
	return recs
}

func TestParseKeepsTextOfInlineMarkup(t *testing.T) {
	recs := parseString(t, `<?xml version="1.0" encoding="ISO-8859-1"?>
<dblp>
<incollection mdate="2017-07-12" key="reference/crypt/Kaliski11w"><author orcid="0000-0001">Burt Kaliski</author><title><i>O</i>-Notation.</title><year>2011</year></incollection>
<article key="journals/x/A"><title>Time 2<sup>&Otilde;(n<sup>1/3</sup>)</sup>.</title><note type="x"/><ee type="oa">https://doi.org/10.1/a</ee></article>
</dblp>`)
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	if recs[0].Title != "O-Notation." || recs[0].Authors[0] != "Burt Kaliski" || recs[0].MDate != "2017-07-12" {
		t.Errorf("first record = %#v", recs[0])
	}
	if recs[1].Title != "Time 2Õ(n1/3)." || recs[1].EE[0] != "https://doi.org/10.1/a" {
		t.Errorf("second record = %#v", recs[1])
	}
}

func TestParseEncodings(t *testing.T) {
	latin := parseString(t, "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?>\n<dblp><article key=\"a\"><title>M\xfcller &amp; &#252;&#xFC; &unknown;</title></article></dblp>")
	if got := latin[0].Title; got != "Müller & üü &unknown;" {
		t.Errorf("Latin-1 title = %q", got)
	}
	utf := parseString(t, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<dblp><article key=\"a\"><title>Müller</title></article></dblp>")
	if got := utf[0].Title; got != "Müller" {
		t.Errorf("UTF-8 title = %q", got)
	}
}

func TestParseSkipsMalformedRecord(t *testing.T) {
	recs := parseString(t, `<dblp><article key="bad"><title>Unclosed</article><article key="good"><title>Fine</title></article></dblp>`)
	if len(recs) != 1 || recs[0].Key != "good" {
		t.Fatalf("records = %#v", recs)
	}
}

func TestParseRejectsTruncatedDump(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dblp.xml")
	if err := os.WriteFile(path, []byte(`<dblp><article key="a"><title>Cut`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Parse(path, func(Record) error { return nil }); err == nil {
		t.Fatal("a truncated dump parsed without error")
	}
}

func TestNormalizeDOI(t *testing.T) {
	for _, in := range []string{
		"10.1145/362929.362947",
		"https://doi.org/10.1145/362929.362947",
		"http://dx.doi.org/10.1145/362929.362947",
		"doi:10.1145/362929.362947",
		" DOI:10.1145/362929.362947 ",
	} {
		if got := NormalizeDOI(in); got != "10.1145/362929.362947" {
			t.Errorf("NormalizeDOI(%q) = %q", in, got)
		}
	}
	if got := NormalizeDOI("https://doi.org/10.1002/(SICI)1097-4571%28199806%2949:8"); got != "10.1002/(sici)1097-4571(199806)49:8" {
		t.Errorf("percent-encoded DOI = %q", got)
	}
	r := Record{EE: []string{"https://arxiv.org/abs/1706.03762", "https://doi.org/10.5555/3295222.3295349"}}
	if got := r.DOIs(); len(got) != 1 || got[0] != "10.5555/3295222.3295349" {
		t.Errorf("DOIs = %q", got)
	}
}
