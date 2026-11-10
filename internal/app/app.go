// Package app wires the pieces together into command handlers.
package app

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wichtfx/dblp-cli/internal/dump"
	"github.com/wichtfx/dblp-cli/internal/mcp"
	"github.com/wichtfx/dblp-cli/internal/meta"
	"github.com/wichtfx/dblp-cli/internal/paths"
	"github.com/wichtfx/dblp-cli/internal/store"
)

// PrintBanner shows the dump freshness line on stderr so it never pollutes
// piped stdout (e.g. bibtex output).
func PrintBanner() {
	m, _, err := meta.Load()
	if err != nil {
		return
	}
	fmt.Fprintln(os.Stderr, m.Banner())
}

// MCP serves the local index as a read-only Model Context Protocol server
// over stdin/stdout, suitable for launching from an LLM client.
func MCP() error {
	st, err := openIndex()
	if err != nil {
		return err
	}
	defer st.Close()
	return mcp.Serve(context.Background(), st, os.Stdin, os.Stdout)
}

// Update downloads the dump (unless already current) and rebuilds the index.
func Update(force bool) error {
	dst, err := paths.Dump()
	if err != nil {
		return err
	}
	m, _, _ := meta.Load()

	if !force {
		if remote, err := dump.RemoteDate(); err == nil && !remote.IsZero() {
			if remote.Equal(m.DumpDate) && fileExists(dst) {
				fmt.Fprintf(os.Stderr, "Already up to date (dump %s). Use --force to re-download.\n",
					remote.Format("2006-01-02"))
				return Index("")
			}
			fmt.Fprintf(os.Stderr, "New dump available: %s (local: %s)\n",
				remote.Format("2006-01-02"), dateOrNone(m.DumpDate))
		}
	}

	fmt.Fprintf(os.Stderr, "Downloading %s ...\n", dump.SourceURL)
	dumpDate, sourceURL, err := dump.Download(dst)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	m.SourceURL = sourceURL
	m.DumpDate = dumpDate
	m.DownloadedAt = time.Now()
	if err := m.Save(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Downloaded dump dated %s.\n", dumpDate.Format("2006-01-02"))
	return Index("")
}

// Index builds the search index from the dump. An empty path uses the
// downloaded dump in the data directory.
func Index(path string) error {
	if path == "" {
		var err error
		if path, err = paths.Dump(); err != nil {
			return err
		}
	}
	if !fileExists(path) {
		return fmt.Errorf("dump not found at %s — run `dblp-cli update` first", path)
	}

	dbPath, err := paths.DB()
	if err != nil {
		return err
	}
	// Build into a temp database and swap it in atomically on success. This
	// keeps any existing index fully searchable during a rebuild and avoids
	// leaving a half-built index if indexing is interrupted.
	tmpPath := dbPath + ".building"
	os.Remove(tmpPath)
	st, err := store.Open(tmpPath)
	if err != nil {
		return err
	}

	b, err := st.NewBuilder()
	if err != nil {
		st.Close()
		return err
	}

	fmt.Fprintln(os.Stderr, "Indexing dump (this takes a few minutes)...")
	start := time.Now()
	indexMeta, _, _ := meta.Load()
	err = importRecords(path, b, indexMeta.Records)
	if err != nil {
		st.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("parse: %w", err)
	}
	n := b.Count()
	printIndexProgress(n, n, true)
	fmt.Fprintln(os.Stderr, "Building full-text search index...")
	if err := b.Commit(); err != nil {
		st.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := st.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	// Atomically replace the live index with the freshly built one.
	if err := os.Rename(tmpPath, dbPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	fmt.Fprintf(os.Stderr, "  %s records indexed in %s.\n", commas(n), time.Since(start).Round(time.Second))

	indexMeta.IndexedAt = time.Now()
	indexMeta.Records = n
	return indexMeta.Save()
}

type preparedResult struct {
	record store.PreparedRecord
	err    error
}

// importRecords overlaps the streaming XML parser, CPU-side record
// preparation, and the serial SQLite writer. Channel sizes bound memory use.
func importRecords(path string, b *store.Builder, expected int64) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workers := min(runtime.GOMAXPROCS(0), 8)
	records := make(chan dump.Record, workers*2)
	prepared := make(chan preparedResult, workers*2)
	parseDone := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for record := range records {
				p, err := store.PrepareRecord(record)
				select {
				case prepared <- preparedResult{record: p, err: err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(prepared)
	}()
	go func() {
		err := dump.Parse(path, func(record dump.Record) error {
			select {
			case records <- record:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(records)
		parseDone <- err
	}()

	var importErr error
	for result := range prepared {
		if result.err != nil && importErr == nil {
			importErr = result.err
			cancel()
			continue
		}
		if importErr != nil {
			continue
		}
		if err := b.AddPrepared(result.record); err != nil {
			importErr = err
			cancel()
			continue
		}
		if n := b.Count(); n%100_000 == 0 {
			printIndexProgress(n, expected, false)
		}
	}
	parseErr := <-parseDone
	if importErr != nil {
		return importErr
	}
	return parseErr
}

func printIndexProgress(current, expected int64, done bool) {
	const width = 30
	if expected <= 0 {
		filled := int(current/100_000) % width
		bar := strings.Repeat("=", filled) + ">" + strings.Repeat(" ", width-filled-1)
		fmt.Fprintf(os.Stderr, "\r  [%s] %s records", bar, commas(current))
		return
	}
	ratio := min(float64(current)/float64(expected), 1)
	filled := int(ratio * width)
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
	if !done && filled > 0 && filled < width {
		bar = bar[:filled-1] + ">" + bar[filled:]
	}
	fmt.Fprintf(os.Stderr, "\r  [%s] %s records  %3.0f%%", bar, commas(current), ratio*100)
	if done {
		fmt.Fprintln(os.Stderr)
	}
}

// Status prints a detailed report and checks the server for a newer dump.
func Status() error {
	m, ok, err := meta.Load()
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("No data yet. Run `dblp-cli update` to download and index the dblp dump.")
		return nil
	}
	fmt.Printf("Source:      %s\n", orNone(m.SourceURL))
	if m.DumpDate.IsZero() {
		fmt.Printf("Dump date:   unknown (indexed from a local dump)\n")
	} else {
		fmt.Printf("Dump date:   %s (%d days old)\n", m.DumpDate.Format("2006-01-02"), int(m.Age().Hours()/24))
	}
	fmt.Printf("Downloaded:  %s\n", dateOrNone(m.DownloadedAt))
	fmt.Printf("Indexed:     %s\n", dateOrNone(m.IndexedAt))
	fmt.Printf("Records:     %s\n", commas(m.Records))

	fmt.Fprintln(os.Stderr, "Checking dblp.org for a newer dump...")
	if remote, err := dump.RemoteDate(); err == nil && !remote.IsZero() {
		switch {
		case remote.After(m.DumpDate):
			fmt.Printf("Update:      newer dump available (%s) — run `dblp-cli update`\n", remote.Format("2006-01-02"))
		default:
			fmt.Println("Update:      you have the latest dump")
		}
	} else {
		fmt.Println("Update:      could not reach dblp.org")
	}
	return nil
}

// openIndex opens the index, erroring helpfully if it isn't built yet.
func openIndex() (*store.Store, error) {
	dbPath, err := paths.DB()
	if err != nil {
		return nil, err
	}
	if !fileExists(dbPath) {
		return nil, fmt.Errorf("no index yet — run `dblp-cli update` first")
	}
	return store.Open(dbPath)
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func orNone(s string) string {
	if s == "" {
		return "(local dump, not from dblp.org)"
	}
	return s
}

func dateOrNone(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02 15:04")
}

// commas formats an integer with thousands separators.
func commas(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
