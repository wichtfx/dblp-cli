package app

import (
	"context"
	"fmt"
	"os"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/clip"
	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/store"
)

// Search runs a one-shot query and prints a numbered result list.
func Search(query string, limit int) error {
	st, err := openIndex()
	if err != nil {
		return err
	}
	defer st.Close()

	res, err := st.Search(context.Background(), query, limit, 0)
	if err != nil {
		return err
	}
	if len(res) == 0 {
		fmt.Fprintln(os.Stderr, "No matches.")
		return nil
	}
	printResults(res, 0)
	fmt.Fprintf(os.Stderr, "\nExport: dblp-cli bibtex %s\n", res[0].Key)
	return nil
}

// Bibtex renders the given keys to stdout, or to a file / clipboard.
func Bibtex(keys []string, outFile string, toClip bool) error {
	st, err := openIndex()
	if err != nil {
		return err
	}
	defer st.Close()

	var recs []dump.Record
	for _, k := range keys {
		r, ok, err := st.Get(context.Background(), k)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: key not found: %s\n", k)
			continue
		}
		recs = append(recs, r)
	}
	if len(recs) == 0 {
		return fmt.Errorf("no matching records")
	}
	out := bibtex.RenderMany(recs)
	return emit(out, outFile, toClip, len(recs))
}

// DOI renders the records with the given DOIs to stdout, or to a file /
// clipboard. A DOI that several records carry yields all of them.
func DOI(dois []string, outFile string, toClip bool) error {
	st, err := openIndex()
	if err != nil {
		return err
	}
	defer st.Close()

	var recs []dump.Record
	for _, doi := range dois {
		found, err := st.ByDOI(context.Background(), doi)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			fmt.Fprintf(os.Stderr, "warning: DOI not found: %s\n", doi)
		}
		recs = append(recs, found...)
	}
	if len(recs) == 0 {
		return fmt.Errorf("no matching records")
	}
	return emit(bibtex.RenderMany(recs), outFile, toClip, len(recs))
}

// emit sends rendered bibtex to the chosen destination.
func emit(text, outFile string, toClip bool, n int) error {
	switch {
	case toClip:
		if !clip.Available() {
			return fmt.Errorf("no clipboard tool found (install wl-clipboard, xclip or xsel)")
		}
		if err := clip.Copy(text); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Copied %d entry(ies) to clipboard.\n", n)
	case outFile != "":
		if err := os.WriteFile(outFile, []byte(text), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Wrote %d entry(ies) to %s\n", n, outFile)
	default:
		fmt.Print(text)
	}
	return nil
}

// appendBibtex appends rendered bibtex to file, creating it if needed and
// separating the new entries from existing ones by a blank line.
func appendBibtex(text, file string, n int) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil && info.Size() > 0 {
		text = "\n" + text
	}
	if err == nil {
		_, err = f.WriteString(text)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Appended %d entry(ies) to %s\n", n, file)
	return nil
}

// printResults renders a numbered list to stderr (so it can coexist with
// piped stdout). base is added to the displayed index for paging.
func printResults(res []store.Result, base int) {
	for i, r := range res {
		fmt.Fprintf(os.Stderr, "%s %s\n",
			cyan(fmt.Sprintf("%3d.", base+i+1)), bold(r.Title))
		fmt.Fprintf(os.Stderr, "     %s\n", metaLine(r, 110))
		fmt.Fprintf(os.Stderr, "     %s · %s\n", magenta(r.Type), r.Key)
	}
}

// metaLine renders the authors, venue and year of a result. When the authors
// and the venue exceed n runes, the author list is shortened so that the
// venue and the year are still printed, in colour.
func metaLine(r store.Result, n int) string {
	authors := r.Authors
	venueLen := 0
	if r.Venue != "" {
		venueLen = len([]rune(" — " + r.Venue))
	}
	if len([]rune(authors))+venueLen > n {
		authors = truncate(authors, max(n-venueLen, 10))
	}
	line := authors
	if r.Venue != "" {
		line += " — " + green(r.Venue)
	}
	if r.Year != "" {
		line += " " + yellow("("+r.Year+")")
	}
	return line
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
