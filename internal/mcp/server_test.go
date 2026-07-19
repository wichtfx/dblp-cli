package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/dblp.db")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.NewBuilder()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Add(dump.Record{
		Type: "article", Key: "journals/cacm/Dijkstra68", Title: "Go To Statement Considered Harmful",
		Authors: []string{"Edsger W. Dijkstra"}, Journal: "Commun. ACM", Year: "1968",
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(dump.Record{
		Type: "article", Key: "journals/cacm/Dijkstra69", Title: "Structured Programming",
		Authors: []string{"Edsger W. Dijkstra"}, Journal: "Commun. ACM", Year: "1969",
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestServeInitializeListAndNotification(t *testing.T) {
	st := testStore(t)
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer
	if err := Serve(context.Background(), st, in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses, want 2: %s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"protocolVersion":"2025-06-18"`) {
		t.Errorf("initialize response: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"search_publications"`) || !strings.Contains(lines[1], `"get_bibtex"`) ||
		!strings.Contains(lines[1], `"browse_author_publications"`) || !strings.Contains(lines[1], `"browse_venue_publications"`) {
		t.Errorf("tools response: %s", lines[1])
	}
}

func TestSearchAndGetBibtexTools(t *testing.T) {
	st := testStore(t)
	search := call(t, st, "search_publications", map[string]any{"query": "dijkstra", "limit": 1})
	if !strings.Contains(search, "Dijkstra") {
		t.Errorf("search response: %s", search)
	}
	if !strings.Contains(search, `"hasMore":true`) {
		t.Errorf("search response has no pagination signal: %s", search)
	}
	bib := call(t, st, "get_bibtex", map[string]any{"keys": []any{"journals/cacm/Dijkstra68"}})
	if !strings.Contains(bib, "@article{DBLP:journals/cacm/Dijkstra68") {
		t.Errorf("bibtex response: %s", bib)
	}
}

func TestBrowseAuthorAndVenueTools(t *testing.T) {
	st := testStore(t)
	author := call(t, st, "browse_author_publications", map[string]any{
		"author": "Edsger W. Dijkstra", "limit": 1,
	})
	if !strings.Contains(author, "Dijkstra69") || !strings.Contains(author, `"hasMore":true`) {
		t.Errorf("author browse response: %s", author)
	}
	venue := call(t, st, "browse_venue_publications", map[string]any{
		"venue": "Commun. ACM", "year": "1968",
	})
	if !strings.Contains(venue, "Dijkstra68") || strings.Contains(venue, "Dijkstra69") {
		t.Errorf("venue browse response: %s", venue)
	}
	if !strings.Contains(venue, `"hasMore":false`) {
		t.Errorf("venue browse response has no final-page signal: %s", venue)
	}
}

func call(t *testing.T, st *store.Store, name string, args map[string]any) string {
	t.Helper()
	params, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	result, err := callTool(st, params)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
