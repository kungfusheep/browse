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

// ArchWiki implements the Provider interface using Arch Wiki search.
type ArchWiki struct {
	client *http.Client
}

// NewArchWiki creates a new Arch Wiki search provider.
func NewArchWiki() *ArchWiki {
	return &ArchWiki{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (a *ArchWiki) Name() string {
	return "Arch Wiki"
}

// Search performs an Arch Wiki search.
func (a *ArchWiki) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	searchURL := fmt.Sprintf(
		"https://wiki.archlinux.org/index.php?search=%s&title=Special:Search&profile=default&fulltext=1",
		url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

	resp, err := a.client.Do(req)
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

	results, err := a.parseResults(string(body))
	if err != nil {
		return nil, err
	}

	return &Results{
		Query:      query,
		Provider:   a.Name(),
		Results:    results,
		TotalFound: len(results),
		SearchedAt: searchedAt,
	}, nil
}

// parseResults extracts search results from Arch Wiki HTML.
func (a *ArchWiki) parseResults(htmlContent string) ([]Result, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	var results []Result

	// Arch Wiki uses mw-search-result class for results
	var findResults func(*html.Node)
	findResults = func(n *html.Node) {
		if n.Type == html.ElementNode {
			// Look for search result list items
			if n.Data == "li" && hasClass(n, "mw-search-result") {
				result := a.extractResult(n)
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
	return results, nil
}

// extractResult extracts a single result from a search result li.
func (a *ArchWiki) extractResult(n *html.Node) Result {
	var result Result

	var extract func(*html.Node)
	extract = func(node *html.Node) {
		if node.Type == html.ElementNode {
			// Title link in mw-search-result-heading
			if node.Data == "div" && hasClass(node, "mw-search-result-heading") {
				if link := findFirstAnchor(node); link != nil {
					result.Title = cleanText(getTextContent(link))
					for _, attr := range link.Attr {
						if attr.Key == "href" {
							href := attr.Val
							if strings.HasPrefix(href, "/") {
								href = "https://wiki.archlinux.org" + href
							}
							result.URL = href
							break
						}
					}
				}
			}

			// Snippet in searchresult class
			if node.Data == "div" && hasClass(node, "searchresult") {
				result.Snippet = cleanText(getTextContent(node))
			}
		}

		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}

	extract(n)

	if result.URL != "" {
		result.Domain = "wiki.archlinux.org"
	}

	return result
}

// findFirstAnchor finds the first <a> element in a subtree.
func findFirstAnchor(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "a" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstAnchor(c); found != nil {
			return found
		}
	}
	return nil
}
