package html

import (
	"strings"
	"testing"

	"browse/rules"
)

func TestParse(t *testing.T) {
	input := `<!DOCTYPE html>
<html>
<body>
<article>
	<h1>Test Title</h1>
	<p>This is a paragraph with <strong>bold</strong> and <em>italic</em> text.</p>
	<h2>Section</h2>
	<ul>
		<li>Item one</li>
		<li>Item two</li>
	</ul>
	<blockquote><p>A quote</p></blockquote>
</article>
</body>
</html>`

	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	content := doc.Content
	if content.Type != NodeDocument {
		t.Errorf("expected NodeDocument, got %v", content.Type)
	}

	if len(content.Children) < 4 {
		t.Fatalf("expected at least 4 children, got %d", len(content.Children))
	}

	// Check h1
	if content.Children[0].Type != NodeHeading1 {
		t.Errorf("expected NodeHeading1, got %v", content.Children[0].Type)
	}
	if content.Children[0].Text != "Test Title" {
		t.Errorf("expected 'Test Title', got %q", content.Children[0].Text)
	}

	// Check paragraph
	if content.Children[1].Type != NodeParagraph {
		t.Errorf("expected NodeParagraph, got %v", content.Children[1].Type)
	}

	// Check h2
	if content.Children[2].Type != NodeHeading2 {
		t.Errorf("expected NodeHeading2, got %v", content.Children[2].Type)
	}

	// Check list
	if content.Children[3].Type != NodeList {
		t.Errorf("expected NodeList, got %v", content.Children[3].Type)
	}
	if len(content.Children[3].Children) != 2 {
		t.Errorf("expected 2 list items, got %d", len(content.Children[3].Children))
	}
}

func TestPlainText(t *testing.T) {
	input := `<article><p>Hello <strong>world</strong>!</p></article>`
	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	content := doc.Content
	if len(content.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(content.Children))
	}

	text := content.Children[0].PlainText()
	if text != "Hello world!" {
		t.Errorf("PlainText() = %q, expected 'Hello world!'", text)
	}
}

func TestFromTemplateArrowList(t *testing.T) {
	// Test that arrow list items are properly parsed
	result := &rules.ApplyV2Result{
		Content: `BBC NEWS
══════════════════════════════════════════════════

  → First item ~~2 hrs ago~~
  → Second item ~~3 hrs ago~~
  → Third item ~~13 hrs ago~~`,
	}

	doc := FromTemplateResult(result, "bbc.com")
	if doc == nil {
		t.Fatal("FromTemplateResult returned nil")
	}

	// Should have: paragraph (BBC NEWS), hr, list
	children := doc.Content.Children
	if len(children) < 2 {
		t.Fatalf("expected at least 2 children, got %d", len(children))
	}

	// Find the list
	var list *Node
	for _, child := range children {
		if child.Type == NodeList {
			list = child
			break
		}
	}

	if list == nil {
		t.Fatal("expected a NodeList for arrow items")
	}

	if len(list.Children) != 3 {
		t.Errorf("expected 3 list items, got %d", len(list.Children))
	}
}

func TestFromTemplateWithLinks(t *testing.T) {
	// Test that links from ApplyV2Result are matched to list items
	result := &rules.ApplyV2Result{
		Content: `NEWS
══════════════════════════════════════════════════

  → Breaking story about politics
  → Tech company announces new product`,
		Links: []rules.ExtractedLink{
			{Text: "Breaking story about politics", Href: "/news/politics/123"},
			{Text: "Tech company announces new product", Href: "/news/tech/456"},
		},
	}

	doc := FromTemplateResult(result, "example.com")
	if doc == nil {
		t.Fatal("FromTemplateResult returned nil")
	}

	// Find the list
	var list *Node
	for _, child := range doc.Content.Children {
		if child.Type == NodeList {
			list = child
			break
		}
	}

	if list == nil {
		t.Fatal("expected a NodeList")
	}

	if len(list.Children) != 2 {
		t.Fatalf("expected 2 list items, got %d", len(list.Children))
	}

	// Check that list items have NodeLink children with hrefs
	// The renderer looks for NodeLink children, not Href on the list item itself
	findLinkHref := func(item *Node) string {
		for _, child := range item.Children {
			if child.Type == NodeLink {
				return child.Href
			}
		}
		return ""
	}

	// Also check the structure is what the renderer expects
	findLinkText := func(item *Node) string {
		for _, child := range item.Children {
			if child.Type == NodeLink {
				return child.PlainText()
			}
		}
		return ""
	}

	// First item
	if href := findLinkHref(list.Children[0]); href != "/news/politics/123" {
		t.Errorf("expected href '/news/politics/123', got %q", href)
	}
	if text := findLinkText(list.Children[0]); !strings.Contains(text, "Breaking story") {
		t.Errorf("expected link text containing 'Breaking story', got %q", text)
	}

	// Second item
	if href := findLinkHref(list.Children[1]); href != "/news/tech/456" {
		t.Errorf("expected href '/news/tech/456', got %q", href)
	}
	if text := findLinkText(list.Children[1]); !strings.Contains(text, "Tech company") {
		t.Errorf("expected link text containing 'Tech company', got %q", text)
	}
}


func TestNavigationExtraction(t *testing.T) {
	input := `<!DOCTYPE html>
<html>
<body>
<header><a href="/home">Home</a><a href="/about">About</a></header>
<nav aria-label="Main navigation">
	<a href="/products">Products</a>
	<a href="/services">Services</a>
</nav>
<article>
	<h1>Content</h1>
	<p>Main content here.</p>
</article>
<footer><a href="/privacy">Privacy</a></footer>
</body>
</html>`

	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Check main content has no nav clutter
	content := doc.Content
	if len(content.Children) != 2 {
		t.Errorf("expected 2 content children (h1, p), got %d", len(content.Children))
	}

	// Check navigation was extracted
	if len(doc.Navigation) != 3 {
		t.Errorf("expected 3 navigation sections, got %d", len(doc.Navigation))
	}

	// Check nav section has correct label
	found := false
	for _, nav := range doc.Navigation {
		if nav.Text == "Main navigation" {
			found = true
			if len(nav.Children) != 2 {
				t.Errorf("expected 2 links in main nav, got %d", len(nav.Children))
			}
		}
	}
	if !found {
		t.Error("expected to find 'Main navigation' section")
	}
}
