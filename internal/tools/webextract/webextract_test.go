package webextract

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

// testConfig disables the SSRF guard so tests can hit httptest's loopback
// server. Production config keeps BlockPrivate true (asserted separately).
func testConfig() Config {
	return Config{BlockPrivate: false}
}

func call(t *testing.T, tool tools.Tool, urls ...string) tools.Result {
	t.Helper()
	raw, _ := json.Marshal(Args{URLs: urls})
	res, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	return res
}

func htmlServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWebExtract_FetchesAndConverts(t *testing.T) {
	srv := htmlServer(t, `<html><head><title>T</title></head><body><h1>Hi</h1><p>world</p></body></html>`)
	tool := New(testConfig, nil)
	res := call(t, tool, srv.URL)

	if !strings.Contains(res.Content, "# Hi") || !strings.Contains(res.Content, "world") {
		t.Fatalf("content missing converted html:\n%s", res.Content)
	}
	if d, _ := res.Data[srv.URL].(map[string]any); d == nil || d["ok"] != true {
		t.Fatalf("data not marked ok: %v", res.Data)
	}
}

func TestWebExtract_NoURLs(t *testing.T) {
	tool := New(testConfig, nil)
	raw, _ := json.Marshal(Args{URLs: []string{"  "}})
	if _, err := tool.Execute(context.Background(), raw); err == nil {
		t.Fatal("expected error when no usable urls")
	}
}

func TestWebExtract_BadStatusReportedPerURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	res := call(t, New(testConfig, nil), srv.URL)
	if !strings.Contains(res.Content, "error:") || !strings.Contains(res.Content, "404") {
		t.Fatalf("expected per-url 404 error, got:\n%s", res.Content)
	}
}

func TestWebExtract_SSRFGuardBlocksLoopback(t *testing.T) {
	srv := htmlServer(t, `<html><body>secret</body></html>`)
	// BlockPrivate true → loopback httptest server must be refused.
	tool := New(func() Config { return Config{BlockPrivate: true} }, nil)
	res := call(t, tool, srv.URL)
	if strings.Contains(res.Content, "secret") {
		t.Fatalf("SSRF guard failed: leaked content:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "error:") {
		t.Fatalf("expected blocked error, got:\n%s", res.Content)
	}
}

func TestWebExtract_Truncates(t *testing.T) {
	big := "<html><body><p>" + strings.Repeat("word ", 5000) + "</p></body></html>"
	srv := htmlServer(t, big)
	tool := New(func() Config { return Config{BlockPrivate: false, MaxChars: 100} }, nil)
	res := call(t, tool, srv.URL)
	if !strings.Contains(res.Content, "truncated") {
		t.Fatalf("expected truncation marker, got len=%d", len(res.Content))
	}
}

type fakeSummarizer struct {
	called bool
	out    string
	err    error
}

func (f *fakeSummarizer) Summarize(_ context.Context, _, _ string) (string, error) {
	f.called = true
	return f.out, f.err
}

func TestWebExtract_SummarizesLargePages(t *testing.T) {
	big := "<html><body><p>" + strings.Repeat("data ", 3000) + "</p></body></html>"
	srv := htmlServer(t, big)
	fs := &fakeSummarizer{out: "SHORT SUMMARY"}
	cfg := func() Config {
		return Config{BlockPrivate: false, Summarize: true, SummarizeMinChars: 100}
	}
	res := call(t, New(cfg, fs), srv.URL)
	if !fs.called {
		t.Fatal("summarizer not invoked for large page")
	}
	if !strings.Contains(res.Content, "SHORT SUMMARY") {
		t.Fatalf("summary not used:\n%s", res.Content)
	}
}

func TestWebExtract_SkipsSummaryForSmallPages(t *testing.T) {
	srv := htmlServer(t, `<html><body><p>tiny</p></body></html>`)
	fs := &fakeSummarizer{out: "SUMMARY"}
	cfg := func() Config {
		return Config{BlockPrivate: false, Summarize: true, SummarizeMinChars: 5000}
	}
	res := call(t, New(cfg, fs), srv.URL)
	if fs.called {
		t.Fatal("summarizer should be skipped for small page")
	}
	if !strings.Contains(res.Content, "tiny") {
		t.Fatalf("content = %q", res.Content)
	}
}

func TestWebExtract_SummarizerErrorFallsBackToTruncate(t *testing.T) {
	big := "<html><body><p>" + strings.Repeat("z ", 3000) + "</p></body></html>"
	srv := htmlServer(t, big)
	fs := &fakeSummarizer{err: fmt.Errorf("llm down")}
	cfg := func() Config {
		return Config{BlockPrivate: false, Summarize: true, SummarizeMinChars: 100, MaxChars: 200}
	}
	res := call(t, New(cfg, fs), srv.URL)
	if !fs.called {
		t.Fatal("summarizer should have been attempted")
	}
	// Fallback to truncated raw content, not an error.
	if !strings.Contains(res.Content, "z z") {
		t.Fatalf("expected fallback to raw content:\n%s", res.Content)
	}
}

func TestWebExtract_DedupesAndCapsURLs(t *testing.T) {
	srv := htmlServer(t, `<html><body><p>ok</p></body></html>`)
	res := call(t, New(testConfig, nil), srv.URL, srv.URL, srv.URL)
	// Same URL three times → deduped to one section (no separator).
	if strings.Contains(res.Content, "---") {
		t.Fatalf("expected dedupe to single section:\n%s", res.Content)
	}
}
