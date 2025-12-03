package search

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// DuckDuckGo implements the Provider interface using DuckDuckGo search.
type DuckDuckGo struct {
	client *http.Client
}

// NewDuckDuckGo creates a new DuckDuckGo search provider.
func NewDuckDuckGo() *DuckDuckGo {
	return &DuckDuckGo{
		client: &http.Client{},
	}
}

// Name returns the provider name.
func (d *DuckDuckGo) Name() string {
	return "DuckDuckGo"
}

// Search performs a DuckDuckGo search and returns parsed results.
func (d *DuckDuckGo) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	// Use DuckDuckGo HTML search
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers to look like a browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

	resp, err := d.client.Do(req)
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

	results, err := d.parseResults(string(body))
	if err != nil {
		return nil, err
	}

	return &Results{
		Query:      query,
		Provider:   d.Name(),
		Results:    results,
		TotalFound: len(results),
		SearchedAt: searchedAt,
	}, nil
}

// parseResults extracts search results from DuckDuckGo HTML.
func (d *DuckDuckGo) parseResults(htmlContent string) ([]Result, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	var results []Result

	// Find all result divs
	var findResults func(*html.Node)
	findResults = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			if hasClass(n, "result") || hasClass(n, "results_links") {
				result := extractResult(n)
				if result.URL != "" && result.Title != "" {
					results = append(results, result)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findResults(c)
		}
	}

	findResults(doc)

	// Deduplicate results by URL
	seen := make(map[string]bool)
	var unique []Result
	for _, r := range results {
		if !seen[r.URL] {
			seen[r.URL] = true
			unique = append(unique, r)
		}
	}

	return unique, nil
}

// extractResult extracts a single result from a result div.
func extractResult(n *html.Node) Result {
	var result Result

	var extract func(*html.Node)
	extract = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Look for the result link
			if node.Data == "a" && hasClass(node, "result__a") {
				result.Title = cleanText(getTextContent(node))
				for _, attr := range node.Attr {
					if attr.Key == "href" {
						result.URL = extractRealURL(attr.Val)
						result.Domain = extractDomain(result.URL)
						break
					}
				}
			}

			// Look for snippet
			if node.Data == "a" && hasClass(node, "result__snippet") {
				result.Snippet = cleanText(getTextContent(node))
			}
		}

		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}

	extract(n)
	return result
}

// extractDomain extracts the domain from a URL.
func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Host
}

// extractRealURL extracts the actual URL from DuckDuckGo's redirect URL.
func extractRealURL(href string) string {
	// DuckDuckGo wraps URLs in a redirect, extract the uddg parameter
	if strings.Contains(href, "uddg=") {
		if u, err := url.Parse(href); err == nil {
			if uddg := u.Query().Get("uddg"); uddg != "" {
				return uddg
			}
		}
	}

	// If it's already a direct URL
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}

	return href
}

// hasClass checks if a node has a specific class.
func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			classes := strings.Fields(attr.Val)
			for _, c := range classes {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

// getTextContent extracts all text content from a node.
func getTextContent(n *html.Node) string {
	var text strings.Builder

	var extract func(*html.Node)
	extract = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}

	extract(n)
	return text.String()
}

// cleanText normalizes whitespace in text.
func cleanText(s string) string {
	// Replace multiple whitespace with single space
	re := regexp.MustCompile(`\s+`)
	s = re.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
