package places

import (
	"context"
	"fmt"
	"strings"

	goplaces "github.com/steipete/goplaces"

	"github.com/sho0pi/god/internal/tool"
)

type SearchTool struct {
	client *goplaces.Client
}

func NewSearchTool(apiKey string) *SearchTool {
	return &SearchTool{
		client: goplaces.NewClient(goplaces.Options{APIKey: apiKey}),
	}
}

func (t *SearchTool) Name() string { return "search_places" }

func (t *SearchTool) Description() string {
	return "Search for places (restaurants, shops, landmarks, etc.) by text query. " +
		"Optionally bias results toward a geographic location."
}

func (t *SearchTool) Schema() *tool.Schema {
	return &tool.Schema{
		Properties: map[string]*tool.Property{
			"query": {
				Type:        "string",
				Description: "Search query, e.g. 'Italian restaurant', 'coffee shop', 'pharmacy near me'",
			},
			"latitude": {
				Type:        "number",
				Description: "Latitude to bias results toward (requires longitude)",
			},
			"longitude": {
				Type:        "number",
				Description: "Longitude to bias results toward (requires latitude)",
			},
			"radius_m": {
				Type:        "number",
				Description: "Search radius in meters when lat/lng are provided (default: 1000)",
			},
			"limit": {
				Type:        "number",
				Description: "Maximum number of results to return (default: 5, max: 20)",
			},
		},
		Required: []string{"query"},
	}
}

func (t *SearchTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	req := goplaces.SearchRequest{
		Query: query,
		Limit: 5,
	}

	if lat, latOK := args["latitude"].(float64); latOK {
		if lng, lngOK := args["longitude"].(float64); lngOK {
			radius := 1000.0
			if r, ok := args["radius_m"].(float64); ok && r > 0 {
				radius = r
			}
			req.LocationBias = &goplaces.LocationBias{
				Lat: lat, Lng: lng, RadiusM: radius,
			}
		}
	}

	if limit, ok := args["limit"].(float64); ok && limit > 0 {
		req.Limit = min(int(limit), 20)
	}

	resp, err := t.client.Search(ctx, req)
	if err != nil {
		return "", fmt.Errorf("places search: %w", err)
	}

	if len(resp.Results) == 0 {
		return "No places found for that query.", nil
	}

	var sb strings.Builder
	for i, p := range resp.Results {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, p.Name)
		if p.Address != "" {
			fmt.Fprintf(&sb, "   Address: %s\n", p.Address)
		}
		if p.Rating != nil {
			fmt.Fprintf(&sb, "   Rating: %.1f", *p.Rating)
			if p.UserRatingCount != nil {
				fmt.Fprintf(&sb, " (%d reviews)", *p.UserRatingCount)
			}
			sb.WriteString("\n")
		}
		if p.OpenNow != nil {
			if *p.OpenNow {
				sb.WriteString("   Open now\n")
			} else {
				sb.WriteString("   Closed\n")
			}
		}
		if p.Location != nil {
			fmt.Fprintf(&sb, "   Location: %.6f, %.6f\n", p.Location.Lat, p.Location.Lng)
		}
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n"), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
