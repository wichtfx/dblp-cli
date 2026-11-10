package dump

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func mirrorClient(modified time.Time) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "primary.invalid" {
			return response(req, http.StatusServiceUnavailable, "try later", time.Time{}), nil
		}
		body := ""
		if req.Method == http.MethodGet {
			body = "dblp dump"
		}
		return response(req, http.StatusOK, body, modified), nil
	})}
}

func response(req *http.Request, status int, body string, modified time.Time) *http.Response {
	header := make(http.Header)
	if !modified.IsZero() {
		header.Set("Last-Modified", modified.Format(http.TimeFormat))
	}
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

func useTestSources(t *testing.T, modified time.Time) {
	t.Helper()
	oldSources, oldClient := Sources, httpClient
	Sources = []string{"https://primary.invalid/dblp.xml.gz", "https://mirror.invalid/dblp.xml.gz"}
	httpClient = mirrorClient(modified)
	t.Cleanup(func() {
		Sources, httpClient = oldSources, oldClient
	})
}

func TestDownloadFallsBackToMirror(t *testing.T) {
	modified := time.Date(2026, time.July, 17, 2, 0, 0, 0, time.UTC)
	useTestSources(t, modified)

	dst := filepath.Join(t.TempDir(), "dblp.xml.gz")
	date, source, err := Download(dst)
	if err != nil {
		t.Fatal(err)
	}
	if source != Sources[1] {
		t.Fatalf("source = %q, want mirror %q", source, Sources[1])
	}
	if !date.Equal(modified) {
		t.Fatalf("date = %v, want %v", date, modified)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "dblp dump" {
		t.Fatalf("downloaded %q", data)
	}
}

func TestRemoteDateFallsBackToMirror(t *testing.T) {
	modified := time.Date(2026, time.July, 17, 2, 0, 0, 0, time.UTC)
	useTestSources(t, modified)

	got, err := RemoteDate()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(modified) {
		t.Fatalf("date = %v, want %v", got, modified)
	}
}

// serveDump serves body from every source and records the Range headers.
func serveDump(t *testing.T, body []byte, modified time.Time, abortAfter int) *[]string {
	t.Helper()
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		if abortAfter > 0 {
			w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Write(body[:abortAfter])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		http.ServeContent(w, r, "dblp.xml.gz", modified, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	oldSources, oldClient := Sources, httpClient
	Sources, httpClient = []string{srv.URL + "/dblp.xml.gz"}, srv.Client()
	t.Cleanup(func() { Sources, httpClient = oldSources, oldClient })
	return &ranges
}

func TestDownloadResumesInterruptedDownload(t *testing.T) {
	body := []byte(strings.Repeat("dblp dump ", 1000))
	modified := time.Date(2026, time.July, 17, 2, 0, 0, 0, time.UTC)
	dst := filepath.Join(t.TempDir(), "dblp.xml.gz")

	serveDump(t, body, modified, 4000)
	if _, _, err := Download(dst); err == nil {
		t.Fatal("interrupted download succeeded")
	}
	info, err := os.Stat(dst + ".part")
	if err != nil || info.Size() != 4000 || !info.ModTime().Equal(modified) {
		t.Fatalf("part after interruption: %v, %v", info, err)
	}

	ranges := serveDump(t, body, modified, 0)
	if _, _, err := Download(dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("resumed download differs: %v", err)
	}
	if len(*ranges) != 1 || (*ranges)[0] != "bytes=4000-" {
		t.Fatalf("Range headers = %q", *ranges)
	}
}

func TestDownloadRestartsWhenDumpChanged(t *testing.T) {
	body := []byte(strings.Repeat("new dump ", 1000))
	dst := filepath.Join(t.TempDir(), "dblp.xml.gz")
	stale := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	if err := os.WriteFile(dst+".part", []byte("old dump bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dst+".part", stale, stale); err != nil {
		t.Fatal(err)
	}
	serveDump(t, body, time.Date(2026, time.July, 17, 2, 0, 0, 0, time.UTC), 0)
	if _, _, err := Download(dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("download after a changed dump differs: %v", err)
	}
}
