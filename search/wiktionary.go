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

// Wiktionary implements the Provider interface using Wiktionary search.
type Wiktionary struct {
	client *http.Client
}

// NewWiktionary creates a new Wiktionary search provider.
func NewWiktionary() *Wiktionary {
	return &Wiktionary{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Name returns the provider name.
func (w *Wiktionary) Name() string {
	return "Wiktionary"
}

// Search performs a Wiktionary search.
func (w *Wiktionary) Search(query string) (*Results, error) {
	searchedAt := time.Now()

	// Use the search page
	searchURL := fmt.Sprintf(
		"https://en.wiktionary.org/w/index.php?search=%s&title=Special:Search&profile=default&fulltext=1",
		url.QueryEscape(query),
	)

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching results: %w", err)
	}
	defer resp.Body.Close()

	// Check if we were redirected directly to an entry (exact match)
	finalURL := resp.Request.URL.String()
	if !strings.Contains(finalURL, "Special:Search") && strings.Contains(finalURL, "/wiki/") {
		// Direct match - extract title from URL and return single result
		title := strings.TrimPrefix(resp.Request.URL.Path, "/wiki/")
		title = strings.ReplaceAll(title, "_", " ")

		body, _ := io.ReadAll(resp.Body)
		snippet := w.extractDefinition(string(body))

		return &Results{
			Query:    query,
			Provider: w.Name(),
			Results: []Result{{
				Title:   title,
				URL:     finalURL,
				Snippet: snippet,
				Domain:  "en.wiktionary.org",
			}},
			TotalFound: 1,
			SearchedAt: searchedAt,
		}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	results, err := w.parseResults(string(body))
	if err != nil {
		return nil, err
	}

	return &Results{
		Query:      query,
		Provider:   w.Name(),
		Results:    results,
		TotalFound: len(results),
		SearchedAt: searchedAt,
	}, nil
}

// parseResults extracts search results from Wiktionary HTML.
func (w *Wiktionary) parseResults(htmlContent string) ([]Result, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	var results []Result

	// Wiktionary uses mw-search-result class like other MediaWiki sites
	var findResults func(*html.Node)
	findResults = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "li" && hasClass(n, "mw-search-result") {
				result := w.extractResult(n)
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
func (w *Wiktionary) extractResult(n *html.Node) Result {
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
								href = "https://en.wiktionary.org" + href
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
		result.Domain = "en.wiktionary.org"
	}

	return result
}

// extractDefinition extracts the first definition from a Wiktionary entry page.
func (w *Wiktionary) extractDefinition(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}

	var definition string

	// Look for the first definition list (ol > li after a heading)
	var findDef func(*html.Node) bool
	findDef = func(n *html.Node) bool {
		if n.Type == html.ElementNode {
			// Look for ordered lists (definitions are in ol)
			if n.Data == "ol" {
				// Get first li
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "li" {
						text := cleanText(getTextContent(c))
						if len(text) > 10 && len(text) < 500 {
							definition = text
							return true
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if findDef(c) {
				return true
			}
		}
		return false
	}

	findDef(doc)
	return definition
}
