package app

import (
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

	res, err := st.Search(query, limit, 0)
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
		r, ok, err := st.Get(k)
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

// printResults renders a numbered list to stderr (so it can coexist with
// piped stdout). base is added to the displayed index for paging.
func printResults(res []store.Result, base int) {
	for i, r := range res {
		fmt.Fprintf(os.Stderr, "%s %s\n",
			cyan(fmt.Sprintf("%3d.", base+i+1)), bold(r.Title))
		meta := r.Authors
		if r.Venue != "" {
			meta += " — " + green(r.Venue)
		}
		if r.Year != "" {
			meta += " " + yellow("("+r.Year+")")
		}
		fmt.Fprintf(os.Stderr, "     %s\n", truncateMeta(meta, r, 110))
		fmt.Fprintf(os.Stderr, "     %s · %s\n", magenta(r.Type), r.Key)
	}
}

// truncateMeta truncates the authors/venue/year line to roughly n visible
// runes. Color codes added above don't count toward the limit, so we measure
// the plain text and only truncate when the authors list itself is long.
func truncateMeta(colored string, r store.Result, n int) string {
	plain := r.Authors
	if len(r.Venue) > 0 {
		plain += " — " + r.Venue
	}
	if len([]rune(plain)) <= n {
		return colored
	}
	return truncate(plain, n) // fall back to plain when over-long
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
