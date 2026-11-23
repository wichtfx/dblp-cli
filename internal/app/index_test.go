package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wichtfx/dblp-cli/internal/store"
)

func TestImportRecordsPipeline(t *testing.T) {
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "dblp.xml")
	data := `<?xml version="1.0" encoding="UTF-8"?>
<dblp>
<article key="journals/test/One"><author>Ada Author</author><title>First Record</title><journal>Test</journal><year>2025</year></article>
<inproceedings key="conf/test/Two"><author>Ben Author</author><title>Second Record</title><booktitle>TestConf</booktitle><year>2026</year></inproceedings>
</dblp>`
	if err := os.WriteFile(xmlPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "dblp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	builder, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	if err := importRecords(xmlPath, builder, 0); err != nil {
		t.Fatal(err)
	}
	if err := builder.Commit(); err != nil {
		t.Fatal(err)
	}
	if builder.Count() != 2 {
		t.Fatalf("imported %d records, want 2", builder.Count())
	}
	results, err := st.Search(context.Background(), "second record", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Key != "conf/test/Two" {
		t.Fatalf("search returned %#v", results)
	}
}
