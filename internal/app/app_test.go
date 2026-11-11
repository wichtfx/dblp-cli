package app

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/meta"
	"github.com/wichtfx/dblp-cli/internal/paths"
)

func TestIndexOfAnotherDumpClearsDumpDate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := (meta.Meta{SourceURL: "https://dblp.org/xml/dblp.xml.gz", DumpDate: time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)}).Save(); err != nil {
		t.Fatal(err)
	}
	xmlPath := filepath.Join(t.TempDir(), "other.xml")
	data := `<?xml version="1.0" encoding="UTF-8"?>
<dblp><article key="journals/test/One"><author>Ada Author</author><title>Only Record</title><journal>Test</journal><year>2025</year></article></dblp>`
	if err := os.WriteFile(xmlPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Index(xmlPath); err != nil {
		t.Fatal(err)
	}
	m, _, err := meta.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !m.DumpDate.IsZero() || m.SourceURL != "" || m.Records != 1 {
		t.Fatalf("meta after indexing another dump: %#v", m)
	}
}

// serveDump serves body as the dump with the given date, from every source.
func serveDump(t *testing.T, body []byte, date time.Time) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "dblp.xml.gz", date, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	old := dump.Sources
	dump.Sources = []string{srv.URL + "/dblp.xml.gz"}
	t.Cleanup(func() { dump.Sources = old })
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestUpdateKeepsOldDumpDateWhenIndexingFails(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	old := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := (meta.Meta{DumpDate: old, SourceURL: "https://dblp.org/xml/dblp.xml.gz"}).Save(); err != nil {
		t.Fatal(err)
	}
	newer := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	serveDump(t, gzipped(t, `<dblp><article key="a"><title>Cut`), newer)
	if err := Update(false); err == nil {
		t.Fatal("Update of a truncated dump succeeded")
	}
	m, _, err := meta.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !m.DumpDate.Equal(old) || !m.DownloadedDumpDate.Equal(newer) {
		t.Fatalf("meta after failed indexing: %#v", m)
	}

	serveDump(t, gzipped(t, `<dblp><article key="a"><title>Whole</title></article></dblp>`), newer)
	if err := Update(true); err != nil {
		t.Fatal(err)
	}
	if m, _, err = meta.Load(); err != nil {
		t.Fatal(err)
	}
	if !m.DumpDate.Equal(newer) || m.SourceURL != dump.Sources[0] || m.Records != 1 {
		t.Fatalf("meta after successful update: %#v", m)
	}
}

func TestMetaOmitsUnsetDownloadDate(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := (meta.Meta{Records: 1}).Save(); err != nil {
		t.Fatal(err)
	}
	p, err := paths.Meta()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("downloaded_dump_date")) {
		t.Fatalf("meta.json has an unset download date: %s", b)
	}
}
