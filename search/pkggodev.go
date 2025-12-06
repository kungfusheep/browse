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

// PkgGoDev implements the Provider interface using pkg.go.dev search.
type PkgGoDev struct {
	client *http.Client
}

// NewPkgGoDev creates a new pkg.go.dev search provider.
func NewPkgGoDev() *PkgGoDev {
	return &PkgGoDev{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (p *PkgGoDev) Name() string {
	return "pkg.go.dev"
}

// Search performs a pkg.go.dev search.
func (p *PkgGoDev) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	searchURL := fmt.Sprintf("https://pkg.go.dev/search?q=%s", url.QueryEscape(query))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

	resp, err := p.client.Do(req)
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

	results, err := p.parseResults(string(body))
	if err != nil {
		return nil, err
	}

	return &Results{
		Query:      query,
		Provider:   p.Name(),
		Results:    results,
		TotalFound: len(results),
		SearchedAt: searchedAt,
	}, nil
}

// parseResults extracts search results from pkg.go.dev HTML.
func (p *PkgGoDev) parseResults(htmlContent string) ([]Result, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	var results []Result

	// pkg.go.dev uses SearchSnippet class for results
	var findResults func(*html.Node)
	findResults = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			if hasClass(n, "SearchSnippet") {
				result := p.extractResult(n)
				if result.Title != "" && result.URL != "" {
					results = append(results, result)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findResults(c)
		}
	}

	findResults(doc)

	// If SearchSnippet parsing didn't work, try finding links with data-gtmc="search result"
	if len(results) == 0 {
		results = p.extractByDataAttr(doc)
	}

	return results, nil
}

// extractByDataAttr finds results using data-gtmc="search result" attribute.
func (p *PkgGoDev) extractByDataAttr(doc *html.Node) []Result {
	var results []Result
	seen := make(map[string]bool)

	var find func(*html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if getAttr(n, "data-gtmc") == "search result" {
				href := getAttr(n, "href")
				if href != "" && !seen[href] {
					seen[href] = true
					title := cleanText(getTextContent(n))
					if title == "" {
						title = strings.TrimPrefix(href, "/")
					}
					if !strings.HasPrefix(href, "http") {
						href = "https://pkg.go.dev" + href
					}
					// Find synopsis from parent SearchSnippet
					snippet := p.findSynopsis(n)
					results = append(results, Result{
						Title:   title,
						URL:     href,
						Snippet: snippet,
						Domain:  "pkg.go.dev",
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}

	find(doc)
	return results
}

// findSynopsis looks for synopsis text near a search result link.
func (p *PkgGoDev) findSynopsis(n *html.Node) string {
	// Walk up to find SearchSnippet parent
	for parent := n.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == html.ElementNode && hasClass(parent, "SearchSnippet") {
			// Find synopsis within this snippet
			var synopsis string
			var findSyn func(*html.Node)
			findSyn = func(node *html.Node) {
				if node.Type == html.ElementNode && node.Data == "p" && hasClass(node, "SearchSnippet-synopsis") {
					synopsis = cleanText(getTextContent(node))
				}
				for c := node.FirstChild; c != nil && synopsis == ""; c = c.NextSibling {
					findSyn(c)
				}
			}
			findSyn(parent)
			return synopsis
		}
	}
	return ""
}

// extractResult extracts a single result from a SearchSnippet div.
func (p *PkgGoDev) extractResult(n *html.Node) Result {
	var result Result

	var extract func(*html.Node)
	extract = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Package name link
			if node.Data == "a" && hasClass(node, "SearchSnippet-header-path") {
				result.Title = cleanText(getTextContent(node))
				for _, attr := range node.Attr {
					if attr.Key == "href" {
						result.URL = "https://pkg.go.dev" + attr.Val
						break
					}
				}
			}

			// Synopsis/description
			if node.Data == "p" && hasClass(node, "SearchSnippet-synopsis") {
				result.Snippet = cleanText(getTextContent(node))
			}
		}

		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}

	extract(n)

	// Set domain
	if result.URL != "" {
		result.Domain = "pkg.go.dev"
	}

	return result
}
