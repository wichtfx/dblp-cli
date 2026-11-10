// Package bibtex renders dblp records as BibTeX entries.
package bibtex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wichtfx/dblp-cli/internal/dump"
)

// entryType maps a dblp element name to its BibTeX entry type.
var entryType = map[string]string{
	"article":       "article",
	"inproceedings": "inproceedings",
	"proceedings":   "proceedings",
	"book":          "book",
	"incollection":  "incollection",
	"phdthesis":     "phdthesis",
	"mastersthesis": "mastersthesis",
	"www":           "misc",
}

// Render returns a BibTeX entry for a single record. The citation key mirrors
// dblp's own convention, e.g. "DBLP:journals/cacm/Dijkstra68".
func Render(r dump.Record) string {
	typ := entryType[r.Type]
	if typ == "" {
		typ = "misc"
	}

	// Collect fields in a stable, conventional order.
	type kv struct{ k, v string }
	var fields []kv
	add := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			fields = append(fields, kv{k, escape(v)})
		}
	}

	if a := r.CleanAuthors(); len(a) > 0 {
		add("author", strings.Join(a, " and "))
	}
	if e := r.CleanEditors(); len(e) > 0 {
		add("editor", strings.Join(e, " and "))
	}
	add("title", r.Title)
	add("journal", r.Journal)
	add("booktitle", r.Booktitle)
	add("series", r.Series)
	add("volume", r.Volume)
	add("number", r.Number)
	add("pages", r.Pages)
	add("publisher", r.Publisher)
	add("school", r.School)
	add("isbn", r.ISBN)
	add("year", r.Year)
	add("month", r.Month)
	if doi := doiOf(r); doi != "" {
		add("doi", doi)
	}
	add("url", r.Link())
	add("biburl", "https://dblp.org/rec/"+r.Key+".html")

	var b strings.Builder
	fmt.Fprintf(&b, "@%s{DBLP:%s,\n", typ, r.Key)
	width := 0
	for _, f := range fields {
		if len(f.k) > width {
			width = len(f.k)
		}
	}
	for _, f := range fields {
		fmt.Fprintf(&b, "  %-*s = {%s},\n", width, f.k, f.v)
	}
	b.WriteString("}\n")
	return b.String()
}

// RenderMany concatenates entries for several records, de-duplicating by key.
func RenderMany(recs []dump.Record) string {
	seen := make(map[string]bool, len(recs))
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		if seen[r.Key] {
			continue
		}
		seen[r.Key] = true
		out = append(out, Render(r))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return strings.Join(out, "\n")
}

// doiOf extracts a DOI from any doi.org link in the record's ee list.
func doiOf(r dump.Record) string {
	for _, ee := range r.EE {
		if i := strings.Index(ee, "doi.org/"); i >= 0 {
			return ee[i+len("doi.org/"):]
		}
	}
	return ""
}

// escape protects the handful of characters that are special in BibTeX
// values. dblp text is already UTF-8 and modern engines (biber) accept it,
// so we keep escaping minimal and non-destructive. Some dblp titles contain
// TeX mathematics, such as $\mathbb {F}_{p^{2m}}$: when the dollar signs pair
// up, the text between them is kept as written, and only the text outside
// is escaped.
func escape(s string) string {
	parts := strings.Split(s, "$")
	if len(parts) < 3 || len(parts)%2 == 0 {
		return escaper.Replace(s)
	}
	var b strings.Builder
	for i, part := range parts {
		if i%2 == 0 {
			b.WriteString(escaper.Replace(part))
			continue
		}
		b.WriteString("$" + part + "$")
	}
	return b.String()
}

var escaper = strings.NewReplacer(
	"{", "\\{",
	"}", "\\}",
	"#", "\\#",
	"%", "\\%",
	"&", "\\&",
	"$", "\\$",
)
