package app

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wichtfx/dblp-cli/internal/bibtex"
	"github.com/wichtfx/dblp-cli/internal/check"
)

// Check compares every entry of a .bib file with the index and prints one
// line per entry. It returns an error when an entry differs or is not found,
// so that the command exits with a non-zero status.
func Check(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	entries, err := bibtex.ParseFile(string(src))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	st, err := openIndex()
	if err != nil {
		return err
	}
	defer st.Close()

	var results []check.Result
	for _, e := range entries {
		res, err := check.Entry(st, e)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Key, err)
		}
		results = append(results, res)
	}
	return report(os.Stdout, results)
}

// report prints the results and a summary, and returns an error when an
// entry needs attention.
func report(w io.Writer, results []check.Result) error {
	width := 0
	for _, r := range results {
		width = max(width, len(r.Entry.Key))
	}
	var counts [3]int
	for _, r := range results {
		counts[r.Status]++
		detail := r.Note
		if r.Status != check.NotFound {
			detail = r.Record.Key
			if len(r.Diffs) > 0 {
				detail += ": " + strings.Join(r.Diffs, "; ")
			}
		}
		fmt.Fprintf(w, "%-9s  %-*s  %s\n", r.Status, width, r.Entry.Key, detail)
	}
	fmt.Fprintf(w, "%d entries: %d ok, %d differ, %d not found\n", len(results), counts[check.OK], counts[check.Differs], counts[check.NotFound])
	if bad := counts[check.Differs] + counts[check.NotFound]; bad > 0 {
		return fmt.Errorf("%d entries need attention", bad)
	}
	return nil
}
