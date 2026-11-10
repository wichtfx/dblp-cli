package store

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wichtfx/dblp-cli/internal/dump"
)

func TestSearchAuthorUsesDBLPIdentity(t *testing.T) {
	st, err := Open(t.TempDir() + "/dblp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	records := []dump.Record{
		{Type: "www", Key: "homepages/w/WeiWang1", Title: "Home Page", Authors: []string{"Wei Wang 0001"}},
		{Type: "article", Key: "journals/a/Wang24", Title: "First Identity", Authors: []string{"Wei Wang 0001"}, Year: "2024"},
		{Type: "article", Key: "journals/b/Wang23", Title: "Second Identity", Authors: []string{"Wei Wang 0002"}, Year: "2023"},
	}
	for _, record := range records {
		if err := b.Add(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}

	got, err := st.SearchAuthor("Wei Wang 0001", "", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "journals/a/Wang24" {
		t.Fatalf("SearchAuthor returned %#v", got)
	}
	got, err = st.SearchAuthor("Wei Wang 0001", "", 20, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("past final page returned %#v", got)
	}
}

func TestBuilderFlushesAuthorBatches(t *testing.T) {
	st, err := Open(t.TempDir() + "/dblp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < authorBatchSize+1; i++ {
		author := fmt.Sprintf("Author %04d", i)
		if err := b.Add(dump.Record{
			Type: "article", Key: fmt.Sprintf("journals/test/%04d", i),
			Title: "Batch Test", Authors: []string{author},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := st.SearchAuthor("Author 1000", "", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "journals/test/1000" {
		t.Fatalf("SearchAuthor returned %#v", got)
	}
}

func TestSearchVenueIsExact(t *testing.T) {
	st, err := Open(t.TempDir() + "/dblp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []dump.Record{
		{Type: "inproceedings", Key: "conf/ccs/Main24", Title: "Main Track", Booktitle: "CCS", Year: "2024"},
		{Type: "inproceedings", Key: "conf/ccs/Workshop24", Title: "Workshop Paper", Booktitle: "CCS Workshops", Year: "2024"},
	} {
		if err := b.Add(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := st.SearchVenue("CCS", "", 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "conf/ccs/Main24" {
		t.Fatalf("SearchVenue returned %#v", got)
	}
}

func TestBuildMatchMatchesShortTermsExactly(t *testing.T) {
	tests := map[string]string{
		"dijkstra go to statement": `"dijkstra"* "go" "to" "statement"*`,
		"co":                       `"co"`,
		"über":                     `"über"*`,
		"ü":                        `"ü"`,
		"title:transformer":        "title:transformer",
	}
	for in, want := range tests {
		if got := buildMatch(in); got != want {
			t.Errorf("buildMatch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuilderKeepsFirstOfDuplicateKeys(t *testing.T) {
	st, err := Open(t.TempDir() + "/dblp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []dump.Record{
		{Type: "article", Key: "journals/x/Dup", Title: "First Copy", Authors: []string{"Ada Author"}},
		{Type: "article", Key: "journals/x/Other", Title: "Other Paper", Authors: []string{"Ada Author"}},
		{Type: "article", Key: "journals/x/Dup", Title: "Second Copy", Authors: []string{"Ben Author"}},
	} {
		if err := b.Add(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	if b.Count() != 2 {
		t.Fatalf("Count = %d, want 2", b.Count())
	}
	r, ok, err := st.Get("journals/x/Dup")
	if err != nil || !ok || r.Title != "First Copy" {
		t.Fatalf("Get = %#v, %v, %v", r, ok, err)
	}
	if got, err := st.SearchAuthor("Ben Author", "", 10, 0); err != nil || len(got) != 0 {
		t.Fatalf("author of the removed copy still linked: %#v, %v", got, err)
	}
	if got, err := st.SearchAuthor("Ada Author", "", 10, 0); err != nil || len(got) != 2 {
		t.Fatalf("SearchAuthor(Ada Author) = %#v, %v", got, err)
	}
	if _, err := st.db.Exec(`INSERT INTO pubs(key) VALUES('journals/x/Dup')`); err == nil {
		t.Fatal("pubs accepts a duplicate key after the build")
	}
}

func TestGetRestoresFieldsLeftOutOfStoredJSON(t *testing.T) {
	st, err := Open(t.TempDir() + "/dblp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	want := dump.Record{Type: "article", Key: "journals/cacm/Dijkstra68a", MDate: "2020-01-01",
		Authors: []string{"Edsger W. Dijkstra"}, Title: "Go To Statement Considered Harmful.",
		Year: "1968", Journal: "Commun. ACM", Volume: "11"}
	if err := b.Add(want); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	var blob string
	if err := st.db.QueryRow(`SELECT data FROM pubs`).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"type"`, `"key"`, `"title"`, `"year"`} {
		if strings.Contains(blob, field) {
			t.Errorf("stored JSON has %s: %s", field, blob)
		}
	}
	got, ok, err := st.Get(want.Key)
	if err != nil || !ok {
		t.Fatalf("Get: %v, %v", ok, err)
	}
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("Get = %#v, want %#v", got, want)
	}
}
