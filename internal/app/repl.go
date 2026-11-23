package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/lineedit"
	"github.com/wichtfx/dblp-cli/internal/store"
)

const pageSize = 15

// activeQuery is whatever the current result page is showing: either a
// free-text search or a facet drill-down (an author or a venue, plus an
// optional year). Keeping it lets n/p page through facet results too.
type activeQuery struct {
	text   string // free-text query (uses Store.Search)
	author string // exact raw DBLP author identity (uses Store.SearchAuthor)
	venue  string // exact venue (uses Store.SearchVenue)
	year   string // optional year filter for facet queries
	label  string // human description shown to the user
}

func (q activeQuery) run(ctx context.Context, st *store.Store, offset int) ([]store.Result, error) {
	switch {
	case q.text != "":
		return st.Search(ctx, q.text, pageSize, offset)
	case q.author != "":
		return st.SearchAuthor(ctx, q.author, q.year, pageSize, offset)
	case q.venue != "":
		return st.SearchVenue(ctx, q.venue, q.year, pageSize, offset)
	}
	return nil, nil
}

// lineReader reads one line at the prompt; lineedit.Reader is one.
type lineReader interface {
	ReadLine(prompt string) (string, bool, error)
}

// session is the state of the interactive search between commands.
type session struct {
	st      *store.Store
	active  activeQuery
	current []store.Result // the visible page
	offset  int
	sel     *dump.Record // currently selected record (for faceting)
	bibFile string       // the file the last `w` wrote to
}

// Interactive launches the search REPL. The user types queries to search,
// selects a result by number to inspect its facets, then drills down by an
// author or venue (optionally narrowed to a year), and exports with b/c/w.
func Interactive() error {
	st, err := openIndex()
	if err != nil {
		return err
	}
	defer st.Close()
	return repl(st, lineedit.New(os.Stdin, os.Stderr))
}

// repl runs the interactive search. Ctrl-C cancels the running command and
// leaves the page as it was; at the prompt it discards the typed line. A
// Ctrl-C right after another one quits.
func repl(st *store.Store, in lineReader) error {
	s := &session{st: st}
	var (
		mu      sync.Mutex
		cancel  context.CancelFunc // of the running command, nil at the prompt
		sigs    = make(chan os.Signal, 1)
		stopped = make(chan struct{})
	)
	signal.Notify(sigs, os.Interrupt)
	defer func() { signal.Stop(sigs); close(stopped) }()
	go func() {
		for {
			select {
			case <-sigs:
			case <-stopped:
				return
			}
			mu.Lock()
			c := cancel
			mu.Unlock()
			if c == nil {
				// SIGINT at the prompt arrives only when the prompt is not
				// read from a terminal, which takes Ctrl-C as a key.
				os.Exit(130)
			}
			c()
		}
	}()

	interrupted := false // the previous input was a Ctrl-C
	printREPLHelp()
	for {
		fmt.Fprintln(os.Stderr)
		line, ok, err := in.ReadLine(cyan("dblp> "))
		if errors.Is(err, lineedit.ErrInterrupt) {
			if interrupted {
				return nil
			}
			interrupted = true
			fmt.Fprintln(os.Stderr, "Press Ctrl-C again to quit.")
			continue
		}
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		interrupted = false

		ctx, c := context.WithCancel(context.Background())
		mu.Lock()
		cancel = c
		mu.Unlock()
		saved := *s
		quit := s.do(ctx, strings.TrimSpace(line))
		mu.Lock()
		cancel = nil
		mu.Unlock()
		if ctx.Err() != nil {
			*s = saved
			interrupted = true
			fmt.Fprintln(os.Stderr, "Cancelled. Press Ctrl-C again to quit.")
		}
		c()
		if quit {
			return nil
		}
	}
}

// do runs one command line and reports whether it asks to quit.
func (s *session) do(ctx context.Context, line string) bool {
	if line == "" {
		return false
	}
	st := s.st
	cmd, rest := split(line)
	switch cmd {
	case "q", "quit", "exit":
		return true
	case "h", "help", "?":
		printREPLHelp()

	case "n", "next":
		s.offset += pageSize
		s.sel = nil
		s.current = execQuery(ctx, st, s.active, s.offset)
	case "p", "prev":
		if s.offset >= pageSize {
			s.offset -= pageSize
		}
		s.sel = nil
		s.current = execQuery(ctx, st, s.active, s.offset)

	case "au", "author": // drill down by an author of the selection
		if q, ok := facetAuthor(s.sel, rest); ok {
			s.active, s.offset, s.sel = q, 0, nil
			s.current = execQuery(ctx, st, s.active, s.offset)
		}
	case "ve", "venue": // drill down by the selection's venue
		if q, ok := facetVenue(s.sel, rest); ok {
			s.active, s.offset, s.sel = q, 0, nil
			s.current = execQuery(ctx, st, s.active, s.offset)
		}

	case "b", "bib":
		doExport(ctx, st, s.current, rest, "", false, s.offset)
	case "c", "copy":
		doExport(ctx, st, s.current, rest, "", true, s.offset)
	case "w", "write":
		if file, ok := doWrite(ctx, st, s.current, rest, s.bibFile, s.sel, s.offset); ok {
			s.bibFile = file
		}
	case "o", "open":
		doOpen(ctx, st, s.current, rest, s.sel, s.offset)

	default:
		// Once a publication is selected, its numbered authors become the
		// active menu. Otherwise a number selects a result from the page.
		if s.sel != nil {
			if n, err := strconv.Atoi(line); err == nil && n > 0 {
				if q, ok := facetAuthor(s.sel, line); ok {
					s.active, s.offset, s.sel = q, 0, nil
					s.current = execQuery(ctx, st, s.active, s.offset)
				}
				return false
			}
		}
		if i, ok := asResultIndex(line, len(s.current), s.offset); ok {
			r, found, err := st.Get(ctx, s.current[i].Key)
			if err != nil {
				printError(ctx, err)
				return false
			}
			if !found {
				return false
			}
			if r.IsHomepage() && len(r.Authors) > 0 {
				name := r.CleanAuthors()[0]
				s.active = activeQuery{author: r.Authors[0], label: "author: " + name}
				s.offset, s.sel = 0, nil
				s.current = execQuery(ctx, st, s.active, s.offset)
				return false
			}
			s.sel = &r
			printFacets(s.offset+i+1, r)
			return false
		}
		s.active = activeQuery{text: line, label: line}
		s.offset, s.sel = 0, nil
		s.current = execQuery(ctx, st, s.active, s.offset)
	}
	return false
}

// printError prints err, unless it comes from a cancelled command, which the
// REPL reports itself.
func printError(ctx context.Context, err error) {
	if ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, red("error:"), err)
	}
}

// execQuery runs the active query at the given offset and prints the page.
func execQuery(ctx context.Context, st *store.Store, active activeQuery, offset int) []store.Result {
	res, err := active.run(ctx, st, offset)
	if err != nil {
		printError(ctx, err)
		return nil
	}
	if len(res) == 0 {
		if offset == 0 {
			fmt.Fprintln(os.Stderr, "No matches.")
		} else {
			fmt.Fprintln(os.Stderr, "No more results.")
		}
		return nil
	}
	if active.label != "" {
		fmt.Fprintln(os.Stderr, "— "+active.label+" —")
	}
	printResults(res, offset)
	return res
}

// printFacets shows the selected record's authors, venue and year as the
// values you can drill down on.
func printFacets(num int, r dump.Record) {
	fmt.Fprintf(os.Stderr, "%s %s\n", cyan(fmt.Sprintf("selected %d:", num)), bold(r.Title))
	if authors := r.CleanAuthors(); len(authors) > 0 {
		parts := make([]string, len(authors))
		for i, a := range authors {
			parts[i] = cyan(strconv.Itoa(i+1)) + " " + a
		}
		fmt.Fprintf(os.Stderr, "  authors: %s\n", strings.Join(parts, "   "))
	}
	if v := r.Venue(); v != "" {
		fmt.Fprintf(os.Stderr, "  venue:   %s\n", green(v))
	}
	if r.Year != "" {
		fmt.Fprintf(os.Stderr, "  year:    %s\n", yellow(r.Year))
	}
	fmt.Fprintln(os.Stderr, "  drill down: au <k> [year|y] · ve [year|y]   (y = this paper's year)")
	fmt.Fprintln(os.Stderr, "  o: open in the browser · w [file]: append its BibTeX to a file")
}

// facetAuthor builds a query for papers by the k-th author of the selection,
// optionally restricted to a year ("y" means the selected paper's year).
func facetAuthor(sel *dump.Record, rest string) (activeQuery, bool) {
	if sel == nil {
		fmt.Fprintln(os.Stderr, "Select a result first (type its number), then `au <k>`.")
		return activeQuery{}, false
	}
	authors := sel.CleanAuthors()
	f := strings.Fields(rest)
	k, yearAt := 1, 0
	if len(f) > 0 {
		// A four-digit number beyond the author count is a year: `au 2020`
		// means the first author in 2020.
		if n, err := strconv.Atoi(f[0]); err == nil && !(len(f[0]) == 4 && n > len(authors)) {
			k, yearAt = n, 1
		}
	}
	if k < 1 || k > len(authors) {
		fmt.Fprintf(os.Stderr, "No author %s (this paper has %d).\n", firstOr(f, "?"), len(authors))
		return activeQuery{}, false
	}
	name := authors[k-1]
	year := resolveYear(f, yearAt, sel.Year)
	label := "author: " + name
	if year != "" {
		label += " · " + year
	}
	return activeQuery{author: sel.Authors[k-1], year: year, label: label}, true
}

// facetVenue builds a query for the selection's venue, optionally by year.
func facetVenue(sel *dump.Record, rest string) (activeQuery, bool) {
	if sel == nil {
		fmt.Fprintln(os.Stderr, "Select a result first (type its number), then `ve`.")
		return activeQuery{}, false
	}
	venue := sel.Venue()
	if venue == "" {
		fmt.Fprintln(os.Stderr, "Selected paper has no venue.")
		return activeQuery{}, false
	}
	year := resolveYear(strings.Fields(rest), 0, sel.Year)
	label := "venue: " + venue
	if year != "" {
		label += " · " + year
	}
	return activeQuery{venue: venue, year: year, label: label}, true
}

// resolveYear reads the year token at fields[i]: empty -> no filter, "y" ->
// the selected paper's year, otherwise the literal token.
func resolveYear(fields []string, i int, selYear string) string {
	if i >= len(fields) {
		return ""
	}
	if t := fields[i]; t == "y" || t == "Y" {
		return selYear
	}
	return fields[i]
}

// doExport resolves the selected result numbers and renders their bibtex. A
// file is appended to, so that repeated `w` commands collect entries in it.
func doExport(ctx context.Context, st *store.Store, current []store.Result, sel, file string, toClip bool, offset int) {
	recs, err := selectRecords(ctx, st, current, sel, offset)
	if err != nil {
		printError(ctx, err)
		return
	}
	if len(recs) == 0 {
		fmt.Fprintln(os.Stderr, "Nothing selected. Example: b 1 3 5  (or `b all`)")
		return
	}
	text := bibtex.RenderMany(recs)
	if file != "" {
		err = appendBibtex(text, file, len(recs))
	} else {
		err = emit(text, "", toClip, len(recs))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, red("error:"), err)
	}
}

// defaultBibFile is the file `w` writes to before any file is named.
const defaultBibFile = "dblp-cli.bib"

// doWrite appends BibTeX to a file for `w [file] [numbers]`. Without a file
// it writes to lastFile, the file of the previous `w`, or to defaultBibFile;
// without numbers it writes the selected publication. It returns the file it
// wrote to.
func doWrite(ctx context.Context, st *store.Store, current []store.Result, rest, lastFile string, sel *dump.Record, offset int) (string, bool) {
	file, nums := split(rest)
	if isSelection(file) {
		file, nums = "", rest
	}
	if file == "" {
		file = lastFile
	}
	if file == "" {
		file = defaultBibFile
	}
	recs, err := chosenRecords(ctx, st, current, nums, sel, offset)
	if err != nil {
		printError(ctx, err)
		return "", false
	}
	if len(recs) == 0 {
		fmt.Fprintln(os.Stderr, "Nothing selected. Select a result, or give numbers: w refs.bib 1 3")
		return "", false
	}
	if err := appendBibtex(bibtex.RenderMany(recs), file, len(recs)); err != nil {
		fmt.Fprintln(os.Stderr, red("error:"), err)
		return "", false
	}
	return file, true
}

// doOpen opens the given results in the browser, or the selected publication
// when no numbers are given.
func doOpen(ctx context.Context, st *store.Store, current []store.Result, nums string, sel *dump.Record, offset int) {
	recs, err := chosenRecords(ctx, st, current, nums, sel, offset)
	if err != nil {
		printError(ctx, err)
		return
	}
	if len(recs) == 0 {
		fmt.Fprintln(os.Stderr, "Nothing selected. Select a result, or give numbers: o 1 3")
		return
	}
	for _, r := range recs {
		if ctx.Err() != nil {
			return
		}
		link := r.Link()
		fmt.Fprintln(os.Stderr, "Opening", link)
		if err := openURL(link); err != nil {
			fmt.Fprintln(os.Stderr, red("error:"), err)
		}
	}
}

// chosenRecords returns the records of the result numbers nums, or the
// selected publication when nums is empty.
func chosenRecords(ctx context.Context, st *store.Store, current []store.Result, nums string, sel *dump.Record, offset int) ([]dump.Record, error) {
	switch {
	case strings.TrimSpace(nums) != "":
		return selectRecords(ctx, st, current, nums, offset)
	case sel != nil:
		return []dump.Record{*sel}, nil
	}
	return nil, nil
}

// selectRecords maps a selection string ("all" or numbers) to full records.
func selectRecords(ctx context.Context, st *store.Store, current []store.Result, sel string, offset int) ([]dump.Record, error) {
	var recs []dump.Record
	for _, i := range parseSelection(sel, len(current), offset) {
		r, ok, err := st.Get(ctx, current[i].Key)
		if err != nil {
			return nil, err
		}
		if ok {
			recs = append(recs, r)
		}
	}
	return recs, nil
}

// isSelection reports whether s is the first word of a selection, a result
// number or "all", and not a file name.
func isSelection(s string) bool {
	if s == "all" || s == "*" {
		return true
	}
	_, err := strconv.Atoi(strings.TrimRight(s, ","))
	return err == nil
}

// parseSelection turns "all" or "1 3 5" (1-based) into 0-based indexes,
// ignoring out-of-range values.
func parseSelection(sel string, n, offset int) []int {
	sel = strings.TrimSpace(sel)
	if sel == "all" || sel == "*" {
		idxs := make([]int, n)
		for i := range idxs {
			idxs[i] = i
		}
		return idxs
	}
	var idxs []int
	for _, f := range strings.Fields(sel) {
		num, err := strconv.Atoi(strings.TrimRight(f, ","))
		if err != nil || num <= offset || num > offset+n {
			continue
		}
		idxs = append(idxs, num-offset-1)
	}
	return idxs
}

// asResultIndex maps the absolute number printed beside a paged result back
// to its position in the current page. Page two, for example, displays 16–30
// rather than starting over at one.
func asResultIndex(s string, n, offset int) (int, bool) {
	num, err := strconv.Atoi(s)
	if err != nil || num <= offset || num > offset+n {
		return 0, false
	}
	return num - offset - 1, true
}

func split(s string) (head, rest string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

func firstOr(f []string, def string) string {
	if len(f) > 0 {
		return f[0]
	}
	return def
}

func printREPLHelp() {
	h := strings.TrimSpace(`
Interactive search. Commands:
  <text>            search publications (title, authors, venue, key)
  <number>          select a result; then browse one of its numbered authors
  au [k] [year|y]   re-search by the selection's k-th author, by default the first (y = its year)
  ve [year|y]       re-search by the selection's venue
  n / p             next / previous page
  ↑ / ↓             browse command history
  ← / →             previous / next publication page
  b <nums|all>      print BibTeX for results        (e.g. b 1 3 5)
  c <nums|all>      copy results to the clipboard
  w [file] [nums]   append results, or the selection, to a file (default: the last one, else dblp-cli.bib)
  o [nums]          open results, or the selection, in the browser
  Ctrl-C            cancel a running search; twice in a row quits
  h / q             help / quit`)
	fmt.Fprintln(os.Stderr, h)
}
