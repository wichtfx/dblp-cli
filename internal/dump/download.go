package dump

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const SourceURL = "https://dblp.org/xml/dblp.xml.gz"

// Sources contains the canonical dump URL followed by the official mirrors.
var Sources = []string{
	SourceURL,
	"https://dblp.dagstuhl.de/xml/dblp.xml.gz",
	"https://dblp.uni-trier.de/xml/dblp.xml.gz",
}

var httpClient = &http.Client{
	Transport: func() *http.Transport {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = 30 * time.Second
		return transport
	}(),
	// The dump is about 1 GiB, so the overall limit must accommodate slow
	// connections. ResponseHeaderTimeout above handles dead mirrors quickly.
	Timeout: 6 * time.Hour,
}

type progressWriter struct {
	total      int64
	written    int64
	lastReport time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.written += int64(len(b))
	if time.Since(p.lastReport) >= 250*time.Millisecond {
		p.report(false)
		p.lastReport = time.Now()
	}
	return len(b), nil
}

func (p *progressWriter) report(done bool) {
	const width = 30
	if p.total <= 0 {
		fmt.Fprintf(os.Stderr, "\r  [%-30s] %6.1f MiB", strings.Repeat("=", int(p.written/(32<<20))%width), mib(p.written))
		return
	}
	ratio := min(float64(p.written)/float64(p.total), 1)
	filled := int(ratio * width)
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", width-filled)
	if !done && filled > 0 && filled < width {
		bar = bar[:filled-1] + ">" + bar[filled:]
	}
	fmt.Fprintf(os.Stderr, "\r  [%s] %6.1f / %.1f MiB  %3.0f%%", bar, mib(p.written), mib(p.total), ratio*100)
}

func mib(n int64) float64 { return float64(n) / (1024 * 1024) }

// RemoteDate checks the canonical host and then the official mirrors.
func RemoteDate() (time.Time, error) {
	var errs []error
	for _, source := range Sources {
		req, err := http.NewRequest(http.MethodHead, source, nil)
		if err != nil {
			return time.Time{}, err
		}
		req.Header.Set("User-Agent", "dblp-cli/1")
		resp, err := httpClient.Do(req)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", source, err))
			continue
		}
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errs = append(errs, fmt.Errorf("%s: %s", source, resp.Status))
			continue
		}
		date, err := time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified"))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: invalid Last-Modified header", source))
			continue
		}
		return date, nil
	}
	return time.Time{}, errors.Join(errs...)
}

// Download tries the canonical host and each official mirror in turn. The
// returned URL identifies the source that succeeded.
func Download(dst string) (time.Time, string, error) {
	var errs []error
	for i, source := range Sources {
		if i > 0 {
			fmt.Fprintf(os.Stderr, "Trying mirror %s ...\n", source)
		}
		date, err := downloadFrom(source, dst)
		if err == nil {
			return date, source, nil
		}
		fmt.Fprintf(os.Stderr, "  %s failed: %v\n", source, err)
		errs = append(errs, fmt.Errorf("%s: %w", source, err))
	}
	return time.Time{}, "", fmt.Errorf("all dblp download servers failed: %w", errors.Join(errs...))
}

// downloadFrom downloads source to dst through dst.part. An interrupted
// download leaves the .part file, dated with the dump's Last-Modified time,
// and the next attempt resumes it with a range request. If-Range makes the
// server send the whole dump instead when it has changed since.
func downloadFrom(source, dst string) (time.Time, error) {
	tmp := dst + ".part"
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("User-Agent", "dblp-cli/1")
	var have int64
	if info, err := os.Stat(tmp); err == nil && info.Size() > 0 {
		have = info.Size()
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
		req.Header.Set("If-Range", info.ModTime().UTC().Format(http.TimeFormat))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()

	var out *os.File
	switch resp.StatusCode {
	case http.StatusPartialContent:
		fmt.Fprintf(os.Stderr, "  resuming at %.1f MiB\n", mib(have))
		out, err = os.OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0o644)
	case http.StatusOK:
		have = 0
		out, err = os.Create(tmp)
	default:
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			os.Remove(tmp)
		}
		return time.Time{}, fmt.Errorf("server returned %s", resp.Status)
	}
	if err != nil {
		return time.Time{}, err
	}
	date, dateErr := time.Parse(http.TimeFormat, resp.Header.Get("Last-Modified"))

	total := resp.ContentLength
	if total > 0 {
		total += have
	}
	progress := &progressWriter{total: total, written: have}
	_, copyErr := io.Copy(out, io.TeeReader(resp.Body, progress))
	progress.report(copyErr == nil)
	fmt.Fprintln(os.Stderr)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		// Keep the part for the next attempt when its version is known.
		if dateErr != nil || os.Chtimes(tmp, date, date) != nil {
			os.Remove(tmp)
		}
		if copyErr != nil {
			return time.Time{}, copyErr
		}
		return time.Time{}, closeErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		return time.Time{}, err
	}
	if dateErr != nil {
		date = time.Time{}
	}
	return date, nil
}
