// Package check compares the entries of a .bib file with the dblp index.
package check

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/store"
)

// Status is the outcome of checking one entry.
type Status int

const (
	OK       Status = iota // a dblp record agrees with the entry
	Differs                // a dblp record has the entry's title but differs elsewhere
	NotFound               // no dblp record has the entry's key, DOI or title
)

func (s Status) String() string {
	return [...]string{"ok", "differs", "not found"}[s]
}

// Result is the outcome of checking one entry.
type Result struct {
	Entry  bibtex.Entry
	Status Status
	Record dump.Record // the dblp record compared with, unless NotFound
	Diffs  []string    // what differs, for Differs
	Note   string      // why nothing was found, for NotFound
}

// Entry looks the entry up in the index and compares it with the record
// found. It looks up, in order, a key of the form DBLP:<key>, the DOI, and
// the title.
func Entry(st *store.Store, e bibtex.Entry) (Result, error) {
	res := Result{Entry: e, Status: NotFound}
	candidates, how, err := lookup(st, e)
	if err != nil {
		return res, err
	}
	if len(candidates) == 0 {
		res.Note = how
		return res, nil
	}
	best, bestDiffs := candidates[0], compare(e, candidates[0])
	for _, c := range candidates[1:] {
		d := compare(e, c)
		if len(d) < len(bestDiffs) || (len(d) == len(bestDiffs) && isCoRR(best) && !isCoRR(c)) {
			best, bestDiffs = c, d
		}
	}
	res.Record, res.Diffs = best, bestDiffs
	if len(bestDiffs) == 0 {
		res.Status = OK
	} else {
		res.Status = Differs
	}
	return res, nil
}

func isCoRR(r dump.Record) bool { return strings.HasPrefix(r.Key, "journals/corr/") }

// lookup returns the candidate records for an entry, or a note on why there
// is none.
func lookup(st *store.Store, e bibtex.Entry) ([]dump.Record, string, error) {
	if key, ok := strings.CutPrefix(e.Key, "DBLP:"); ok {
		r, found, err := st.Get(key)
		if err != nil || found {
			return []dump.Record{r}, "", err
		}
		return nil, "no dblp record has key " + key, nil
	}
	if doi := entryDOI(e); doi != "" {
		recs, err := st.ByDOI(doi)
		if err != nil && err != store.ErrNoDOIIndex {
			return nil, "", err
		}
		if len(recs) > 0 {
			return recs, "", nil
		}
	}
	title := Normalize(e.Fields["title"])
	if title == "" {
		return nil, "the entry has no title", nil
	}
	// Every word is matched exactly, so the query narrows the candidates to
	// titles with all of the entry's words.
	var terms []string
	for _, w := range strings.Fields(title) {
		terms = append(terms, `"`+w+`"`)
	}
	results, err := st.SearchQuery("title:("+strings.Join(terms, " ")+")", "", 50, 0)
	if err != nil {
		return nil, "", err
	}
	var recs []dump.Record
	for _, r := range results {
		if r.Type == "www" || Normalize(r.Title) != title {
			continue
		}
		rec, found, err := st.Get(r.Key)
		if err != nil {
			return nil, "", err
		}
		if found {
			recs = append(recs, rec)
		}
	}
	if len(recs) == 0 {
		return nil, "no dblp record has this title", nil
	}
	return recs, "", nil
}

// compare lists how the entry differs from the record in title, year,
// authors and DOI. A record without a DOI is not taken to differ in it.
func compare(e bibtex.Entry, r dump.Record) []string {
	var diffs []string
	if doi := entryDOI(e); doi != "" {
		if dois := r.DOIs(); len(dois) > 0 && !slices.Contains(dois, dump.NormalizeDOI(doi)) {
			diffs = append(diffs, fmt.Sprintf("doi %s, dblp %s", doi, strings.Join(dois, ", ")))
		}
	}
	if t := e.Fields["title"]; t != "" && Normalize(t) != Normalize(r.Title) {
		diffs = append(diffs, fmt.Sprintf("title %q, dblp %q", strings.TrimSpace(t), r.Title))
	}
	if y := strings.TrimSpace(e.Fields["year"]); y != "" && r.Year != "" && y != r.Year {
		diffs = append(diffs, fmt.Sprintf("year %s, dblp %s", y, r.Year))
	}
	if a := e.Fields["author"]; a != "" {
		if d := compareAuthors(a, r.CleanAuthors()); d != "" {
			diffs = append(diffs, d)
		}
	}
	return diffs
}

// compareAuthors compares the surnames of a BibTeX author list, in order,
// with dblp's authors. "and others" ends the comparison.
func compareAuthors(field string, dblp []string) string {
	names := splitAuthors(field)
	others := len(names) > 0 && Normalize(names[len(names)-1]) == "others"
	if others {
		names = names[:len(names)-1]
	}
	if len(names) > len(dblp) || (!others && len(names) != len(dblp)) {
		return fmt.Sprintf("%d authors, dblp %d", len(names), len(dblp))
	}
	for i, name := range names {
		bib, want := bibSurname(name), dblpSurname(dblp[i])
		if bib != want && !strings.HasSuffix(bib, " "+want) && !strings.HasSuffix(want, " "+bib) {
			return fmt.Sprintf("author %d %q, dblp %q", i+1, strings.TrimSpace(name), dblp[i])
		}
	}
	return ""
}

// entryDOI returns the entry's DOI without the TeX escapes and braces a
// .bib file may write in it, as in 10.1007/978-3-662-54970-4\_5.
func entryDOI(e bibtex.Entry) string {
	return strings.TrimSpace(strings.NewReplacer(`\`, "", "{", "", "}", "").Replace(e.Fields["doi"]))
}

// splitAuthors splits a BibTeX name list on "and" outside braces.
func splitAuthors(field string) []string {
	field = strings.Join(strings.Fields(field), " ")
	var names []string
	depth, start := 0, 0
	for i := 0; i < len(field); i++ {
		switch field[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth == 0 && i+5 <= len(field) && strings.EqualFold(field[i:i+5], " and ") {
			names = append(names, field[start:i])
			start = i + 5
			i += 4
		}
	}
	return append(names, field[start:])
}

// bibSurname returns the normalised surname of a BibTeX name, written as
// "Last, First", "First Last" or "{Organisation}".
func bibSurname(name string) string {
	name = strings.TrimSpace(name)
	if last, _, ok := strings.Cut(name, ","); ok {
		return Normalize(last)
	}
	if strings.HasSuffix(name, "}") {
		if i := strings.LastIndex(name, "{"); i >= 0 {
			return Normalize(name[i:])
		}
	}
	return lastWord(Normalize(name))
}

// dblpSurname returns the normalised surname of a dblp name.
func dblpSurname(name string) string { return lastWord(Normalize(name)) }

var nameSuffix = map[string]bool{"jr": true, "sr": true, "ii": true, "iii": true, "iv": true}

func lastWord(s string) string {
	words := strings.Fields(s)
	for len(words) > 1 && nameSuffix[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}

var (
	texAccent  = regexp.MustCompile(`\\[` + "`" + `'^"~=.uvHckrdbt]\s*\{?\s*\\?([A-Za-z])\}?`)
	texLetter  = regexp.MustCompile(`\\(ss|ae|AE|oe|OE|aa|AA|o|O|l|L|i|j)\b`)
	texCommand = regexp.MustCompile(`\\[A-Za-z]+\*?`)
)

// Normalize reduces a title or a name to lower-case ASCII words: TeX accents
// and commands are removed, letters with diacritics are folded, and every
// other character separates words.
func Normalize(s string) string {
	s = texAccent.ReplaceAllString(s, "$1")
	s = texLetter.ReplaceAllString(s, "$1")
	s = texCommand.ReplaceAllString(s, " ")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(unicode.ToLower(r))
		case fold[r] != "":
			b.WriteString(fold[r])
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '{' || r == '}':
			// Braces only protect case in BibTeX; they separate nothing.
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
