// Debug tool to analyze HTML structure
package main

import (
	"fmt"
	"net/http"
	"os"

	"golang.org/x/net/html"
)

func main() {
	url := "https://google.com"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	doc, err := html.Parse(resp.Body)
	if err != nil {
		fmt.Println("Parse error:", err)
		return
	}

	// Find body
	body := findElement(doc, "body")
	if body == nil {
		fmt.Println("No body found!")
		return
	}

	fmt.Println("Body found, analyzing children...")
	analyzeNode(body, 0, 3) // max depth 3
}

func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func analyzeNode(n *html.Node, depth, maxDepth int) {
	if depth > maxDepth {
		return
	}

	indent := ""
	for i := 0; i < depth; i++ {
		indent += "  "
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.ElementNode:
			attrs := ""
			for _, a := range c.Attr {
				if a.Key == "id" || a.Key == "class" {
					attrs += fmt.Sprintf(" %s=%q", a.Key, a.Val)
				}
			}
			fmt.Printf("%s<%s%s>\n", indent, c.Data, attrs)
			analyzeNode(c, depth+1, maxDepth)
		case html.TextNode:
			text := c.Data
			if len(text) > 50 {
				text = text[:50] + "..."
			}
			text = fmt.Sprintf("%q", text)
			if len(text) > 3 { // skip whitespace-only
				// fmt.Printf("%sTEXT: %s\n", indent, text)
			}
		}
	}
}
