// Test pages fetches and parses multiple URLs to validate rendering.
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"browse/document"
	"browse/html"
	"browse/render"
)

var testURLs = []string{
	"https://kungfusheep.com/articles",
	"https://kungfusheep.com/articles/service-principles",
	"https://example.com",
	"https://go.dev/doc/effective_go",
	"https://en.wikipedia.org/wiki/Go_(programming_language)",
	"https://lobste.rs",
	"https://text.npr.org",
	"https://lite.cnn.com",
}

func main() {
	if len(os.Args) > 1 {
		// Single URL mode
		testURL(os.Args[1])
		return
	}

	// Test all URLs
	for _, url := range testURLs {
		testURL(url)
		fmt.Println(strings.Repeat("=", 80))
	}
}

func testURL(url string) {
	fmt.Printf("Testing: %s\n", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("  ERROR creating request: %v\n", err)
		return
	}
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("  ERROR fetching: %v\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("  Status: %d\n", resp.StatusCode)

	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Printf("  ERROR parsing: %v\n", err)
		return
	}

	// Count content types
	stats := countNodes(doc.Content)
	fmt.Printf("  Content: %d h1, %d h2, %d h3, %d paragraphs, %d lists, %d blockquotes, %d code blocks\n",
		stats["h1"], stats["h2"], stats["h3"], stats["p"], stats["list"], stats["blockquote"], stats["code"])

	// Count links
	linkCount := countLinks(doc.Content)
	fmt.Printf("  Links: %d\n", linkCount)

	// Show navigation sections
	if len(doc.Navigation) > 0 {
		fmt.Printf("  Navigation sections: %d\n", len(doc.Navigation))
		for i, nav := range doc.Navigation {
			if i >= 3 {
				fmt.Printf("    ... and %d more\n", len(doc.Navigation)-3)
				break
			}
			fmt.Printf("    [%s] %d links\n", nav.Text, len(nav.Children))
		}
	}

	// Render to a test canvas (600 lines to see more links)
	canvas := render.NewCanvas(80, 600)
	renderer := document.NewRenderer(canvas)
	renderer.Render(doc, 0)

	// Show visible links
	links := renderer.Links()
	fmt.Printf("  Visible links (first 5):\n")
	for i, link := range links {
		if i >= 5 {
			fmt.Printf("    ... and %d more\n", len(links)-5)
			break
		}
		href := link.Href
		if len(href) > 50 {
			href = href[:47] + "..."
		}
		fmt.Printf("    [%d,%d] %s\n", link.X, link.Y, href)
	}

	// Calculate content height
	height := renderer.ContentHeight(doc)
	fmt.Printf("  Content height: %d lines\n", height)
}

func countNodes(n *html.Node) map[string]int {
	stats := make(map[string]int)
	countNodesRecursive(n, stats)
	return stats
}

func countNodesRecursive(n *html.Node, stats map[string]int) {
	switch n.Type {
	case html.NodeHeading1:
		stats["h1"]++
	case html.NodeHeading2:
		stats["h2"]++
	case html.NodeHeading3:
		stats["h3"]++
	case html.NodeParagraph:
		stats["p"]++
	case html.NodeList:
		stats["list"]++
	case html.NodeBlockquote:
		stats["blockquote"]++
	case html.NodeCodeBlock:
		stats["code"]++
	}
	for _, child := range n.Children {
		countNodesRecursive(child, stats)
	}
}

func countLinks(n *html.Node) int {
	count := 0
	countLinksRecursive(n, &count)
	return count
}

func countLinksRecursive(n *html.Node, count *int) {
	if n.Type == html.NodeLink {
		(*count)++
	}
	for _, child := range n.Children {
		countLinksRecursive(child, count)
	}
}
