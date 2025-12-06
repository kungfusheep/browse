package search

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Wikipedia implements the Provider interface using Wikipedia's API.
type Wikipedia struct {
	client *http.Client
}

// NewWikipedia creates a new Wikipedia search provider.
func NewWikipedia() *Wikipedia {
	return &Wikipedia{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns the provider name.
func (w *Wikipedia) Name() string {
	return "Wikipedia"
}

// Search performs a Wikipedia search and returns parsed results.
func (w *Wikipedia) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	// Use Wikipedia's search API
	apiURL := fmt.Sprintf(
		"https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch=%s&srlimit=15&format=json",
		url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Web Browser)")
	req.Header.Set("Accept", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	var apiResp wikipediaResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	results := make([]Result, 0, len(apiResp.Query.Search))
	for _, item := range apiResp.Query.Search {
		results = append(results, Result{
			Title:   item.Title,
			URL:     fmt.Sprintf("https://en.wikipedia.org/wiki/%s", url.PathEscape(item.Title)),
			Snippet: stripHTMLTags(item.Snippet),
			Domain:  "en.wikipedia.org",
		})
	}

	return &Results{
		Query:      query,
		Provider:   w.Name(),
		Results:    results,
		TotalFound: apiResp.Query.SearchInfo.TotalHits,
		SearchedAt: searchedAt,
	}, nil
}

// wikipediaResponse represents the Wikipedia API response.
type wikipediaResponse struct {
	Query struct {
		SearchInfo struct {
			TotalHits int `json:"totalhits"`
		} `json:"searchinfo"`
		Search []struct {
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
		} `json:"search"`
	} `json:"query"`
}

// stripHTMLTags removes HTML tags from a string (Wikipedia snippets contain <span> tags).
func stripHTMLTags(s string) string {
	var result []rune
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result = append(result, r)
		}
	}
	return string(result)
}
