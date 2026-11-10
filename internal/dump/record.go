// Package dump downloads and parses the dblp.org XML dump.
package dump

import (
	"regexp"
	"strings"
)

// RecordTypes are the top-level publication elements in dblp.xml that we
// index. The element name doubles as the record's logical type.
var RecordTypes = map[string]bool{
	"article":       true,
	"inproceedings": true,
	"proceedings":   true,
	"book":          true,
	"incollection":  true,
	"phdthesis":     true,
	"mastersthesis": true,
	"www":           true,
}

// Record is one bibliographic entry. Fields map directly onto dblp's XML
// children; multi-valued elements (authors, editors, ee) become slices.
type Record struct {
	Type      string   `json:"type,omitempty"`
	Key       string   `xml:"key,attr" json:"key,omitempty"`
	MDate     string   `xml:"mdate,attr" json:"mdate,omitempty"`
	Authors   []string `xml:"author" json:"authors,omitempty"`
	Editors   []string `xml:"editor" json:"editors,omitempty"`
	Title     string   `xml:"title" json:"title,omitempty"`
	Year      string   `xml:"year" json:"year,omitempty"`
	Month     string   `xml:"month" json:"month,omitempty"`
	Journal   string   `xml:"journal" json:"journal,omitempty"`
	Booktitle string   `xml:"booktitle" json:"booktitle,omitempty"`
	Pages     string   `xml:"pages" json:"pages,omitempty"`
	Volume    string   `xml:"volume" json:"volume,omitempty"`
	Number    string   `xml:"number" json:"number,omitempty"`
	Publisher string   `xml:"publisher" json:"publisher,omitempty"`
	School    string   `xml:"school" json:"school,omitempty"`
	Series    string   `xml:"series" json:"series,omitempty"`
	ISBN      string   `xml:"isbn" json:"isbn,omitempty"`
	EE        []string `xml:"ee" json:"ee,omitempty"`
	URL       string   `xml:"url" json:"url,omitempty"`
}

// homonym matches dblp's 4-digit name-disambiguation suffix, e.g.
// "Wei Wang 0001". We strip it for clean display and bibtex output.
var homonym = regexp.MustCompile(`\s+\d{4}$`)

// CleanName removes dblp's homonym suffix from an author/editor name.
func CleanName(s string) string {
	return homonym.ReplaceAllString(strings.TrimSpace(s), "")
}

// CleanAuthors returns the record's authors with homonym suffixes stripped.
func (r Record) CleanAuthors() []string { return cleanAll(r.Authors) }

// CleanEditors returns the record's editors with homonym suffixes stripped.
func (r Record) CleanEditors() []string { return cleanAll(r.Editors) }

func cleanAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, n := range in {
		out = append(out, CleanName(n))
	}
	return out
}

// Venue returns the publication venue (journal or conference/book title).
func (r Record) Venue() string {
	if r.Journal != "" {
		return r.Journal
	}
	return r.Booktitle
}

// Link returns the best external URL: a DOI/publisher link if present,
// otherwise the dblp page URL.
func (r Record) Link() string {
	if len(r.EE) > 0 {
		return r.EE[0]
	}
	return r.URL
}

// IsHomepage reports whether the record represents a DBLP author homepage.
func (r Record) IsHomepage() bool {
	return r.Type == "www" && strings.HasPrefix(r.Key, "homepages/")
}
