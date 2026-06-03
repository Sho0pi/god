// Package websearch provides the web_search tool: a DuckDuckGo search via the
// ddg-search CLI (github.com/Djarvur/ddg-search). It returns result metadata
// only (titles, URLs, snippets) — use the web_extract tool to read a page.
package websearch

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sho0pi/god/internal/tools"
)

const (
	defaultResults = 5
	maxResults     = 10
)

// Args are the web_search arguments.
type Args struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results"`
}

// New returns the web_search tool. runner runs the ddg-search command; pass nil
// to use the real CLI. The seam exists so tests don't shell out.
func New(runner Runner) tools.Tool {
	if runner == nil {
		runner = cliRunner
	}
	return tools.NewTypedTool(
		"web_search",
		"Search the web using DuckDuckGo. No API key required. "+
			"Returns titles, URLs, and snippets for the top results. "+
			"To read a result's full page, pass its URL to web_extract.",
		schema(),
		func(ctx context.Context, args Args) (tools.Result, error) {
			return run(ctx, runner, args)
		},
	)
}

// Runner executes the search and returns raw output. Injected for testing.
type Runner func(ctx context.Context, query string, max int) (string, error)

func schema() *tools.Schema {
	return tools.Object(map[string]*tools.Property{
		"query": {
			Type:        "string",
			Description: "Search query, e.g. 'best Go web frameworks 2025'",
		},
		"max_results": {
			Type:        "number",
			Description: fmt.Sprintf("Max results to return (default: %d, max: %d)", defaultResults, maxResults),
		},
	}, "query")
}

func run(ctx context.Context, runner Runner, args Args) (tools.Result, error) {
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return tools.Result{}, fmt.Errorf("query is required")
	}

	n := defaultResults
	if args.MaxResults > 0 {
		n = min(args.MaxResults, maxResults)
	}

	out, err := runner(ctx, query, n)
	if err != nil {
		return tools.Result{}, fmt.Errorf("ddg-search: %w", err)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return tools.Result{Content: fmt.Sprintf("No results found for %q.", query)}, nil
	}
	return tools.Result{
		Content: out,
		Data:    map[string]any{"query": query, "max_results": n},
	}, nil
}

func cliRunner(ctx context.Context, query string, max int) (string, error) {
	out, err := exec.CommandContext(ctx,
		"ddg-search",
		"--max-results", strconv.Itoa(max),
		query,
	).Output()
	return string(out), err
}
