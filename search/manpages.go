package search

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// ManPages implements the Provider interface using Arch Linux man pages.
type ManPages struct {
	client *http.Client
}

// NewManPages creates a new man pages search provider.
func NewManPages() *ManPages {
	return &ManPages{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (m *ManPages) Name() string {
	return "Man Pages"
}

// Search performs a man pages search.
func (m *ManPages) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	searchURL := fmt.Sprintf(
		"https://man.archlinux.org/search?q=%s&go=Go",
		url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

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

	results, err := m.parseResults(string(body))
	if err != nil {
		return nil, err
	}

	return &Results{
		Query:      query,
		Provider:   m.Name(),
		Results:    results,
		TotalFound: len(results),
		SearchedAt: searchedAt,
	}, nil
}

// parseResults extracts search results from man pages HTML.
func (m *ManPages) parseResults(htmlContent string) ([]Result, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	var results []Result

	// Man pages uses table rows for results
	var findResults func(*html.Node)
	var inResultsTable bool

	findResults = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Detect the results table
			if n.Data == "table" && hasClass(n, "results") {
				inResultsTable = true
			}

			// Parse table rows in results
			if n.Data == "tr" && inResultsTable {
				result := m.extractResult(n)
				if result.Title != "" && result.URL != "" {
					results = append(results, result)
				}
			}

			// Also look for list-based results
			if n.Data == "li" && hasClassContaining(n, "result") {
				result := m.extractResult(n)
				if result.Title != "" && result.URL != "" {
					results = append(results, result)
				}
			}

			// Look for dl/dt/dd structure
			if n.Data == "dt" {
				result := m.extractResult(n)
				if result.Title != "" && result.URL != "" {
					// Try to get description from next dd sibling
					if dd := findNextSibling(n, "dd"); dd != nil {
						result.Snippet = cleanText(getTextContent(dd))
					}
					results = append(results, result)
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findResults(c)
		}

		if n.Type == html.ElementNode && n.Data == "table" && hasClass(n, "results") {
			inResultsTable = false
		}
	}

	findResults(doc)

	// If no structured results found, try generic link extraction
	if len(results) == 0 {
		results = m.extractGenericResults(doc)
	}

	return results, nil
}

// extractResult extracts a single result from a table row or list item.
func (m *ManPages) extractResult(n *html.Node) Result {
	var result Result

	var extract func(*html.Node)
	extract = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Look for man page links
			if node.Data == "a" && result.URL == "" {
				href := getAttr(node, "href")
				text := cleanText(getTextContent(node))
				if href != "" && text != "" {
					if !strings.HasPrefix(href, "http") {
						href = "https://man.archlinux.org" + href
					}
					// Filter for actual man page links
					if strings.Contains(href, "/man/") {
						result.URL = href
						result.Title = text
					}
				}
			}

			// Look for description in td or span
			if (node.Data == "td" || node.Data == "span") && result.Snippet == "" {
				text := cleanText(getTextContent(node))
				if len(text) > 10 && len(text) < 300 && text != result.Title {
					result.Snippet = text
				}
			}
		}

		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}

	extract(n)

	if result.URL != "" {
		result.Domain = "man.archlinux.org"
	}

	return result
}

// extractGenericResults tries to find man page links anywhere in the document.
func (m *ManPages) extractGenericResults(doc *html.Node) []Result {
	var results []Result
	seen := make(map[string]bool)

	var findLinks func(*html.Node)
	findLinks = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := getAttr(n, "href")
			text := cleanText(getTextContent(n))

			if href != "" && text != "" && strings.Contains(href, "/man/") {
				if !strings.HasPrefix(href, "http") {
					href = "https://man.archlinux.org" + href
				}
				if !seen[href] {
					seen[href] = true
					results = append(results, Result{
						Title:  text,
						URL:    href,
						Domain: "man.archlinux.org",
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findLinks(c)
		}
	}

	findLinks(doc)
	return results
}

// findNextSibling finds the next sibling element with the given tag name.
func findNextSibling(n *html.Node, tag string) *html.Node {
	for sib := n.NextSibling; sib != nil; sib = sib.NextSibling {
		if sib.Type == html.ElementNode && sib.Data == tag {
			return sib
		}
	}
	return nil
}
