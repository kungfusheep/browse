package search

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// MDN implements the Provider interface using MDN Web Docs API.
type MDN struct {
	client *http.Client
}

// NewMDN creates a new MDN Web Docs search provider.
func NewMDN() *MDN {
	return &MDN{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (m *MDN) Name() string {
	return "MDN Web Docs"
}

// mdnResponse represents the MDN API response.
type mdnResponse struct {
	Documents []mdnDocument `json:"documents"`
}

type mdnDocument struct {
	MdnURL  string `json:"mdn_url"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// Search performs an MDN Web Docs search via API.
func (m *MDN) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	searchURL := fmt.Sprintf(
		"https://developer.mozilla.org/api/v1/search?q=%s&locale=en-US",
		url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
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

	var mdnResp mdnResponse
	if err := json.Unmarshal(body, &mdnResp); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	results := make([]Result, 0, len(mdnResp.Documents))
	for _, doc := range mdnResp.Documents {
		results = append(results, Result{
			Title:   doc.Title,
			URL:     "https://developer.mozilla.org" + doc.MdnURL,
			Snippet: doc.Summary,
			Domain:  "developer.mozilla.org",
		})
	}

	return &Results{
		Query:      query,
		Provider:   m.Name(),
		Results:    results,
		TotalFound: len(results),
		SearchedAt: searchedAt,
	}, nil
}
