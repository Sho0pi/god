// Package webextract provides the web_extract tool: it fetches one or more web
// pages, converts them to compact markdown, and (optionally) summarizes large
// pages with an LLM to keep token usage down. Ported from the Nous Research
// hermes-agent web_extract tool, adapted to god's config-driven, provider-
// neutral tool ecosystem.
//
// Security: every URL is untrusted. An SSRF guard (ssrf.go) blocks non-public
// addresses, embedded credentials, and non-http(s) schemes so a prompt-injected
// URL cannot reach internal services or cloud metadata.
package webextract

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sho0pi/god/internal/tools"
)

const (
	defaultMaxChars          = 8000
	defaultSummarizeMinChars = 5000
	defaultTimeout           = 15 * time.Second
	defaultUserAgent         = "god-webextract/1.0 (+https://github.com/sho0pi/god)"
	maxURLs                  = 5
	maxBodyBytes             = 5 << 20 // 5 MiB cap on a single page download
)

// Config controls web_extract behaviour. It maps to a tools.web_extract block in
// god.yaml; pass the live values per call so edits hot-reload.
type Config struct {
	MaxChars          int           // truncate each page to this many runes (0 → default)
	Summarize         bool          // summarize large pages via the Summarizer
	SummarizeMinChars int           // pages shorter than this skip the LLM (0 → default)
	Timeout           time.Duration // per-request timeout (0 → default)
	UserAgent         string        // outbound User-Agent (empty → default)
	BlockPrivate      bool          // SSRF guard on/off; SHOULD be true in production
}

func (c Config) withDefaults() Config {
	if c.MaxChars <= 0 {
		c.MaxChars = defaultMaxChars
	}
	if c.SummarizeMinChars <= 0 {
		c.SummarizeMinChars = defaultSummarizeMinChars
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.UserAgent == "" {
		c.UserAgent = defaultUserAgent
	}
	return c
}

// Summarizer shrinks page content with an LLM. The web_extract tool is
// decoupled from any specific LLM client through this interface; pass nil to
// disable summarization regardless of Config.Summarize.
type Summarizer interface {
	Summarize(ctx context.Context, content, instruction string) (string, error)
}

// Args are the web_extract arguments.
type Args struct {
	URLs []string `json:"urls"`
}

// New returns the web_extract tool. cfgFn supplies live config per call (so
// god.yaml edits hot-reload); pass a constant-returning func for a fixed config.
// summarizer may be nil to disable LLM summarization.
func New(cfgFn func() Config, summarizer Summarizer) tools.Tool {
	if cfgFn == nil {
		cfgFn = func() Config { return Config{BlockPrivate: true} }
	}
	e := &extractor{cfgFn: cfgFn, summarizer: summarizer}
	return tools.NewTypedTool(
		"web_extract",
		"Fetch the full content of one or more web pages by URL and return it as "+
			"markdown. Use after web_search when a snippet is not enough. Pass up to "+
			"5 URLs.",
		schema(),
		e.execute,
	)
}

func schema() *tools.Schema {
	return tools.Object(map[string]*tools.Property{
		"urls": {
			Type:        "array",
			Description: "Web page URLs to fetch (http/https), up to 5.",
			Items:       &tools.Property{Type: "string"},
		},
	}, "urls")
}

type extractor struct {
	cfgFn      func() Config
	summarizer Summarizer
}

func (e *extractor) execute(ctx context.Context, args Args) (tools.Result, error) {
	cfg := e.cfgFn().withDefaults()

	urls := dedupeNonEmpty(args.URLs)
	if len(urls) == 0 {
		return tools.Result{}, fmt.Errorf("at least one url is required")
	}
	if len(urls) > maxURLs {
		urls = urls[:maxURLs]
	}

	client := newClient(cfg)

	var out strings.Builder
	data := make(map[string]any, len(urls))
	for i, raw := range urls {
		if i > 0 {
			out.WriteString("\n\n---\n\n")
		}
		content, err := e.fetchOne(ctx, client, cfg, raw)
		if err != nil {
			fmt.Fprintf(&out, "## %s\n\nerror: %v", raw, err)
			data[raw] = map[string]any{"ok": false, "error": err.Error()}
			continue
		}
		out.WriteString(content)
		data[raw] = map[string]any{"ok": true, "chars": len(content)}
	}

	return tools.Result{Content: out.String(), Data: data}, nil
}

func (e *extractor) fetchOne(ctx context.Context, client *http.Client, cfg Config, raw string) (string, error) {
	u, err := validateURL(raw)
	if err != nil {
		return "", err
	}
	if cfg.BlockPrivate && blockedLiteralIP(u) {
		return "", fmt.Errorf("blocked non-public address %s", u.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Accept", "text/html,text/plain,*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	ctype := resp.Header.Get("Content-Type")
	title, body := renderBody(ctype, string(bodyBytes))

	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("no extractable text content")
	}

	body = e.shrink(ctx, cfg, u.String(), body)

	header := "## " + u.String()
	if title != "" {
		header = "## " + title + "\n" + u.String()
	}
	return header + "\n\n" + body, nil
}

// shrink summarizes the body when configured and large enough; otherwise it
// truncates to MaxChars. Summarization failures fall back to truncation so a
// flaky LLM never breaks extraction.
func (e *extractor) shrink(ctx context.Context, cfg Config, url, body string) string {
	if e.summarizer != nil && cfg.Summarize && len([]rune(body)) >= cfg.SummarizeMinChars {
		instruction := "Summarize the following web page into concise markdown, " +
			"preserving key facts, figures, names and quotes. Drop navigation and boilerplate. Source: " + url
		if s, err := e.summarizer.Summarize(ctx, body, instruction); err == nil {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return truncateRunes(body, cfg.MaxChars)
}

// renderBody converts a response body to (title, text) based on content type.
func renderBody(contentType, body string) (title, text string) {
	if strings.Contains(strings.ToLower(contentType), "text/html") || looksLikeHTML(body) {
		return htmlToMarkdown(body)
	}
	// Plain text (or unknown): strip base64 image noise and return as-is.
	return "", stripBase64Images(body)
}

func looksLikeHTML(s string) bool {
	head := strings.ToLower(s)
	if len(head) > 512 {
		head = head[:512]
	}
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html") || strings.Contains(head, "<body")
}

func newClient(cfg Config) *http.Client {
	dialer := &net.Dialer{Timeout: cfg.Timeout, KeepAlive: 30 * time.Second}
	if cfg.BlockPrivate {
		dialer.Control = safeControl
	}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   cfg.Timeout,
		ResponseHeaderTimeout: cfg.Timeout,
		MaxIdleConns:          10,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   cfg.Timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			// Re-validate every redirect target: scheme and credentials. The
			// dialer Control hook still guards the actual connect IP.
			u, err := validateURL(req.URL.String())
			if err != nil {
				return fmt.Errorf("blocked redirect to %s: %w", req.URL, err)
			}
			if cfg.BlockPrivate && blockedLiteralIP(u) {
				return fmt.Errorf("blocked redirect to non-public address %s", u.Hostname())
			}
			return nil
		},
	}
}

func dedupeNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "\n\n[... truncated]"
}
