package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wichtfx/dblp-cli/internal/meta"
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
