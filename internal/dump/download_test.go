package dump

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
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
