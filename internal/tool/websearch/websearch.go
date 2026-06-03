package websearch

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sho0pi/god/internal/tool"
)

// Tool searches the web via the ddg-search CLI (github.com/Djarvur/ddg-search).
// Install: go install github.com/Djarvur/ddg-search/cmd/ddg-search@latest
type Tool struct{}

func New() *Tool { return &Tool{} }

func (t *Tool) Name() string { return "web_search" }

func (t *Tool) Description() string {
	return "Search the web using DuckDuckGo. No API key required. " +
		"Returns titles, URLs, and snippets for the top results."
}

func (t *Tool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"query": {
				Type:        "string",
				Description: "Search query, e.g. 'best Go web frameworks 2025'",
			},
			"max_results": {
				Type:        "number",
				Description: "Max results to return (default: 5, max: 10)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *Tool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	maxResults := 5
	if n, ok := args["max_results"].(float64); ok && n > 0 {
		maxResults = min(int(n), 10)
	}

	out, err := exec.CommandContext(ctx,
		"ddg-search",
		"--max-results", strconv.Itoa(maxResults),
		query,
	).Output()
	if err != nil {
		return "", fmt.Errorf("ddg-search: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return fmt.Sprintf("No results found for %q.", query), nil
	}
	return result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
