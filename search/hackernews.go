package search

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// HackerNews implements the Provider interface using Algolia's HN API.
type HackerNews struct {
	client *http.Client
}

// NewHackerNews creates a new Hacker News search provider.
func NewHackerNews() *HackerNews {
	return &HackerNews{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (h *HackerNews) Name() string {
	return "Hacker News"
}

// algoliaResponse represents the Algolia API response structure.
type algoliaResponse struct {
	Hits []algoliaHit `json:"hits"`
}

type algoliaHit struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Author    string `json:"author"`
	Points    int    `json:"points"`
	NumComments int  `json:"num_comments"`
	ObjectID  string `json:"objectID"`
	CreatedAt string `json:"created_at"`
}

// Search performs a Hacker News search via Algolia API.
func (h *HackerNews) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	// Use Algolia's HN search API
	searchURL := fmt.Sprintf(
		"https://hn.algolia.com/api/v1/search?query=%s&tags=story&hitsPerPage=25",
		url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var algResp algoliaResponse
	if err := json.Unmarshal(body, &algResp); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	results := make([]Result, 0, len(algResp.Hits))
	for _, hit := range algResp.Hits {
		// Use the external URL if available, otherwise link to HN comments
		resultURL := hit.URL
		if resultURL == "" {
			resultURL = fmt.Sprintf("https://news.ycombinator.com/item?id=%s", hit.ObjectID)
		}

		// Build snippet with metadata
		snippet := fmt.Sprintf("%d points by %s", hit.Points, hit.Author)
		if hit.NumComments > 0 {
			snippet += fmt.Sprintf(" | %d comments", hit.NumComments)
		}

		domain := extractDomain(resultURL)

		results = append(results, Result{
			Title:   hit.Title,
			URL:     resultURL,
			Snippet: snippet,
			Domain:  domain,
		})
	}

	return &Results{
		Query:      query,
		Provider:   h.Name(),
		Results:    results,
		TotalFound: len(results),
		SearchedAt: searchedAt,
	}, nil
}
