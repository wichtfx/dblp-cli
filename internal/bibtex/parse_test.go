package bibtex

import "testing"

func TestParseFile(t *testing.T) {
	src := `% a comment line with an @ sign
@string{cacm = "Communications of the {ACM}"}
@comment{ignored {nested} text}
@Article{DBLP:journals/cacm/Dijkstra68a,
  author  = {Edsger W. Dijkstra},
  title   = "{Go To} Statement Considered Harmful",
  journal = cacm # { 11},
  year    = 1968,
  note    = {50\% {\"u}ber},
}
@inproceedings(key2, title={A}, author={Ada Lovelace and Others})
`
	entries, err := ParseFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %#v", len(entries), entries)
	}
	e := entries[0]
	want := map[string]string{
		"author":  "Edsger W. Dijkstra",
		"title":   "{Go To} Statement Considered Harmful",
		"journal": "Communications of the {ACM} 11",
		"year":    "1968",
		"note":    `50\% {\"u}ber`,
	}
	if e.Type != "article" || e.Key != "DBLP:journals/cacm/Dijkstra68a" || e.Line != 4 {
		t.Errorf("entry = %q %q line %d", e.Type, e.Key, e.Line)
	}
	for k, v := range want {
		if e.Fields[k] != v {
			t.Errorf("field %s = %q, want %q", k, e.Fields[k], v)
		}
	}
	if entries[1].Key != "key2" || entries[1].Fields["author"] != "Ada Lovelace and Others" {
		t.Errorf("second entry = %#v", entries[1])
	}
}

func TestParseFileReportsUnclosedEntry(t *testing.T) {
	if _, err := ParseFile("@article{a,\n title = {T}"); err == nil {
		t.Fatal("an unclosed entry parsed without error")
	}
}
