// Package meta tracks information about the local dump: where it came
// from, the date of the dump on the server, and when we last indexed it.
package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/wichtfx/dblp-cli/internal/paths"
)

// StaleAfter is how old a dump may get before we nudge the user to refresh.
const StaleAfter = 30 * 24 * time.Hour

// Meta is the persisted state describing the local data.
type Meta struct {
	SourceURL    string    `json:"source_url"`
	DumpDate     time.Time `json:"dump_date"`     // server Last-Modified of the dump
	DownloadedAt time.Time `json:"downloaded_at"` // when we fetched it
	IndexedAt    time.Time `json:"indexed_at"`    // when we last (re)built the index
	Records      int64     `json:"records"`       // number of indexed records
}

// Load reads the metadata file. A missing file yields a zero-value Meta and
// ok=false, which callers treat as "nothing downloaded yet".
func Load() (m Meta, ok bool, err error) {
	p, err := paths.Meta()
	if err != nil {
		return m, false, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, false, err
	}
	return m, true, nil
}

// Save writes the metadata file atomically.
func (m Meta) Save() error {
	p, err := paths.Meta()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Age returns how long ago the dump was produced on the server.
func (m Meta) Age() time.Duration { return time.Since(m.DumpDate) }

// Stale reports whether the dump is old enough to warrant a refresh.
func (m Meta) Stale() bool { return m.Age() > StaleAfter }

// Banner returns a one-line human summary of the local data state, suitable
// for printing at the top of every command.
func (m Meta) Banner() string {
	if m.DumpDate.IsZero() {
		if m.Records > 0 {
			return fmt.Sprintf("Indexed %s records from a local dump (date unknown) · `dblp update` fetches a dated dump",
				humanCount(m.Records))
		}
		return "No dump downloaded yet — run `dblp update` to fetch the dblp.org dump."
	}
	age := int(m.Age().Hours() / 24)
	s := fmt.Sprintf("Dump: %s (%d day(s) old) · %s records",
		m.DumpDate.Format("2006-01-02"), age, humanCount(m.Records))
	if m.Stale() {
		s += " · ⚠ stale, refresh with `dblp update`"
	}
	return s
}

func humanCount(n int64) string {
	switch {
	case n <= 0:
		return "0"
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	}
}
