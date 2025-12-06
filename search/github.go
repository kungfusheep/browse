package search

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// GitHub implements the Provider interface using GitHub's API.
type GitHub struct {
	client *http.Client
}

// NewGitHub creates a new GitHub search provider.
func NewGitHub() *GitHub {
	return &GitHub{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (g *GitHub) Name() string {
	return "GitHub"
}

// githubResponse represents the GitHub search API response.
type githubResponse struct {
	TotalCount int          `json:"total_count"`
	Items      []githubRepo `json:"items"`
}

type githubRepo struct {
	FullName        string `json:"full_name"`
	HTMLURL         string `json:"html_url"`
	Description     string `json:"description"`
	StargazersCount int    `json:"stargazers_count"`
	ForksCount      int    `json:"forks_count"`
	Language        string `json:"language"`
}

// Search performs a GitHub repository search via API.
func (g *GitHub) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	searchURL := fmt.Sprintf(
		"https://api.github.com/search/repositories?q=%s&per_page=25",
		url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Browse-Terminal-Browser")

	resp, err := g.client.Do(req)
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

	var ghResp githubResponse
	if err := json.Unmarshal(body, &ghResp); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	results := make([]Result, 0, len(ghResp.Items))
	for _, repo := range ghResp.Items {
		// Build snippet with metadata
		snippet := repo.Description
		if snippet == "" {
			snippet = fmt.Sprintf("★ %d | %d forks", repo.StargazersCount, repo.ForksCount)
		} else {
			snippet = fmt.Sprintf("%s (★ %d)", snippet, repo.StargazersCount)
		}
		if repo.Language != "" {
			snippet += " | " + repo.Language
		}

		results = append(results, Result{
			Title:   repo.FullName,
			URL:     repo.HTMLURL,
			Snippet: snippet,
			Domain:  "github.com",
		})
	}

	return &Results{
		Query:      query,
		Provider:   g.Name(),
		Results:    results,
		TotalFound: ghResp.TotalCount,
		SearchedAt: searchedAt,
	}, nil
}
