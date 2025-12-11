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


func TestThemeColorExtraction(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name: "theme-color meta tag",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="#ff6600">
			</head><body><p>Content</p></body></html>`,
			expected: "#ff6600",
		},
		{
			name: "msapplication-TileColor",
			html: `<!DOCTYPE html><html><head>
				<meta name="msapplication-TileColor" content="#da532c">
			</head><body><p>Content</p></body></html>`,
			expected: "#da532c",
		},
		{
			name: "body bgcolor (legacy HN style)",
			html: `<!DOCTYPE html><html><head></head>
				<body bgcolor="#ff6600"><p>Content</p></body></html>`,
			expected: "#ff6600",
		},
		{
			name: "TileColor takes priority over theme-color",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="#1e2327">
				<meta name="msapplication-TileColor" content="#da532c">
			</head><body><p>Content</p></body></html>`,
			expected: "#da532c",
		},
		{
			name: "bgcolor takes priority over theme-color",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="#1e2327">
			</head><body bgcolor="#ff6600"><p>Content</p></body></html>`,
			expected: "#ff6600",
		},
		{
			name: "shorthand hex expansion",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="#f60">
			</head><body><p>Content</p></body></html>`,
			expected: "#ff6600",
		},
		{
			name: "named color",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="orange">
			</head><body><p>Content</p></body></html>`,
			expected: "#ffa500",
		},
		{
			name: "white theme-color filtered out",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="#ffffff">
			</head><body><p>Content</p></body></html>`,
			expected: "",
		},
		{
			name: "black theme-color filtered out",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="#000000">
			</head><body><p>Content</p></body></html>`,
			expected: "",
		},
		{
			name: "white theme-color but usable TileColor",
			html: `<!DOCTYPE html><html><head>
				<meta name="theme-color" content="#ffffff">
				<meta name="msapplication-TileColor" content="#da532c">
			</head><body><p>Content</p></body></html>`,
			expected: "#da532c",
		},
		{
			name: "no theme color",
			html: `<!DOCTYPE html><html><head></head>
				<body><p>Content</p></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseString(tt.html)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}
			if doc.ThemeColor != tt.expected {
				t.Errorf("ThemeColor = %q, expected %q", doc.ThemeColor, tt.expected)
			}
		})
	}
}

func TestExtractThemeColorFromHTML(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "theme-color meta tag",
			html:     `<html><head><meta name="theme-color" content="#ff6600"></head></html>`,
			expected: "#ff6600",
		},
		{
			name:     "HN-style bgcolor",
			html:     `<html><head></head><body bgcolor="#ff6600">content</body></html>`,
			expected: "#ff6600",
		},
		{
			name:     "case insensitive meta name",
			html:     `<html><head><meta name="Theme-Color" content="#123456"></head></html>`,
			expected: "#123456",
		},
		{
			name:     "single quoted content",
			html:     `<html><head><meta name="theme-color" content='#abcdef'></head></html>`,
			expected: "#abcdef",
		},
		{
			name:     "no theme color",
			html:     `<html><head></head><body>no color</body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractThemeColorFromHTML(tt.html)
			if got != tt.expected {
				t.Errorf("ExtractThemeColorFromHTML() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		hex      string
		r, g, b  uint8
		expected bool
	}{
		{"#ff6600", 255, 102, 0, true},
		{"#FF6600", 255, 102, 0, true},
		{"ff6600", 255, 102, 0, true},
		{"#f60", 255, 102, 0, true},
		{"#000000", 0, 0, 0, true},
		{"#ffffff", 255, 255, 255, true},
		{"#1e2327", 30, 35, 39, true},
		{"invalid", 0, 0, 0, false},
		{"#gg0000", 0, 0, 0, false},
		{"#ff", 0, 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.hex, func(t *testing.T) {
			r, g, b, ok := ParseHexColor(tt.hex)
			if ok != tt.expected {
				t.Errorf("ParseHexColor(%q) ok = %v, expected %v", tt.hex, ok, tt.expected)
				return
			}
			if ok && (r != tt.r || g != tt.g || b != tt.b) {
				t.Errorf("ParseHexColor(%q) = (%d,%d,%d), expected (%d,%d,%d)",
					tt.hex, r, g, b, tt.r, tt.g, tt.b)
			}
		})
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

func TestImageExtraction(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		expectImages int
		expectAlt    string
		expectHref   string
	}{
		{
			name:         "img with alt text",
			html:         `<article><img src="/images/logo.png" alt="Company Logo"></article>`,
			expectImages: 1,
			expectAlt:    "[Image: Company Logo]",
			expectHref:   "/images/logo.png",
		},
		{
			name:         "img without alt uses filename",
			html:         `<article><img src="/path/to/screenshot.jpg"></article>`,
			expectImages: 1,
			expectAlt:    "[Image: screenshot]",
			expectHref:   "/path/to/screenshot.jpg",
		},
		{
			name:         "img with query string",
			html:         `<article><img src="https://example.com/photo.png?size=large"></article>`,
			expectImages: 1,
			expectAlt:    "[Image: photo]",
			expectHref:   "https://example.com/photo.png?size=large",
		},
		{
			name:         "inline img in paragraph",
			html:         `<article><p>Here is an image: <img src="/icon.gif" alt="icon"> in text</p></article>`,
			expectImages: 1,
			expectAlt:    "[Image: icon]",
			expectHref:   "/icon.gif",
		},
		{
			name:         "multiple images",
			html:         `<article><img src="/a.png" alt="A"><img src="/b.png" alt="B"></article>`,
			expectImages: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseString(tt.html)
			if err != nil {
				t.Fatalf("Parse failed: %v", err)
			}

			// Count image links
			var images []*Node
			var findImages func(n *Node)
			findImages = func(n *Node) {
				if n.Type == NodeLink && strings.HasPrefix(n.PlainText(), "[Image:") {
					images = append(images, n)
				}
				for _, child := range n.Children {
					findImages(child)
				}
			}
			findImages(doc.Content)

			if len(images) != tt.expectImages {
				t.Errorf("expected %d images, got %d", tt.expectImages, len(images))
			}

			if tt.expectAlt != "" && len(images) > 0 {
				if text := images[0].PlainText(); text != tt.expectAlt {
					t.Errorf("expected alt %q, got %q", tt.expectAlt, text)
				}
			}

			if tt.expectHref != "" && len(images) > 0 {
				if images[0].Href != tt.expectHref {
					t.Errorf("expected href %q, got %q", tt.expectHref, images[0].Href)
				}
			}
		})
	}
}

func TestExtractFilename(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"/images/logo.png", "logo"},
		{"https://example.com/path/to/screenshot.jpg", "screenshot"},
		{"/photo.png?size=large&format=webp", "photo"},
		{"/image.gif#section", "image"},
		{"simple.png", "simple"},
		{"/no-extension", "no-extension"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractFilename(tt.url)
			if got != tt.expected {
				t.Errorf("extractFilename(%q) = %q, expected %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestContentExtractionWithHeadingIdMain(t *testing.T) {
	// Regression test: pages with <h3 id="main">main</h3> should not treat
	// the heading as the content container (only div/section/article should match)
	input := `<!DOCTYPE html>
<html>
<body>
<div class="content">
  <h1>Page Title</h1>
  <p>First paragraph.</p>
  <h3 id="main">main</h3>
  <p>Content about main function.</p>
  <p>More content here.</p>
</div>
</body>
</html>`

	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should extract ALL content, not just the h3 with id="main"
	if len(doc.Content.Children) < 4 {
		t.Errorf("expected at least 4 children (h1, p, h3, p, p), got %d", len(doc.Content.Children))
	}

	// First child should be the page title, not "main"
	if doc.Content.Children[0].Type != NodeHeading1 {
		t.Errorf("expected first child to be NodeHeading1, got %v", doc.Content.Children[0].Type)
	}
	if doc.Content.Children[0].Text != "Page Title" {
		t.Errorf("expected first heading 'Page Title', got %q", doc.Content.Children[0].Text)
	}
}

func TestArticleListDetection(t *testing.T) {
	// Test that pages with multiple articles are detected and extracted as lists
	input := `<!DOCTYPE html>
<html>
<body>
<main>
  <article>
    <h2><a href="/article/1">First Article Title Is Long Enough</a></h2>
    <p>This is the description of the first article with enough text to be considered substantial content.</p>
    <span class="author">John Smith</span>
    <time datetime="2025-12-10">Dec 10, 2025</time>
  </article>
  <article>
    <h2><a href="/article/2">Second Article Has a Great Headline</a></h2>
    <p>Description for the second article which also has plenty of text to qualify as a description.</p>
    <span class="byline">Jane Doe</span>
    <time datetime="2025-12-09">Dec 9, 2025</time>
  </article>
  <article>
    <h2><a href="/article/3">Third Article Completes the Set</a></h2>
    <p>The third article description rounds out our test with more substantial paragraph text here.</p>
    <span class="author">Bob Wilson</span>
    <time datetime="2025-12-08">Dec 8, 2025</time>
  </article>
</main>
</body>
</html>`

	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should have a NodeList as the first child (after any anchor)
	var list *Node
	for _, child := range doc.Content.Children {
		if child.Type == NodeList {
			list = child
			break
		}
	}

	if list == nil {
		t.Fatal("expected a NodeList for multiple articles")
	}

	if len(list.Children) != 3 {
		t.Errorf("expected 3 list items, got %d", len(list.Children))
	}

	// Check first article has title link
	item := list.Children[0]
	var foundLink bool
	var linkHref, linkText string
	for _, child := range item.Children {
		if child.Type == NodeLink {
			foundLink = true
			linkHref = child.Href
			linkText = child.PlainText()
		}
	}

	if !foundLink {
		t.Error("expected first list item to have a link")
	}
	if linkHref != "/article/1" {
		t.Errorf("expected href '/article/1', got %q", linkHref)
	}
	if !strings.Contains(linkText, "First Article") {
		t.Errorf("expected link text containing 'First Article', got %q", linkText)
	}

	// Check that item has description and metadata
	plainText := item.PlainText()
	if !strings.Contains(plainText, "description") {
		t.Errorf("expected item to contain description, got %q", plainText)
	}
}

func TestArticleListSingleArticleNotTriggered(t *testing.T) {
	// Single article pages should NOT be converted to lists
	input := `<!DOCTYPE html>
<html>
<body>
<article>
  <h1>A Single Full Article</h1>
  <p>This is a complete article with multiple paragraphs of content.</p>
  <p>The second paragraph continues the article.</p>
  <p>And a third paragraph to round it out.</p>
</article>
</body>
</html>`

	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Should NOT have a NodeList - should have heading and paragraphs
	for _, child := range doc.Content.Children {
		if child.Type == NodeList {
			t.Error("single article page should not be converted to list")
			break
		}
	}

	// Should have the content extracted normally
	if len(doc.Content.Children) < 3 {
		t.Errorf("expected at least 3 children (h1 + paragraphs), got %d", len(doc.Content.Children))
	}
}

func TestArticleListSkipsSponsored(t *testing.T) {
	// Sponsored/ad articles should be skipped
	input := `<!DOCTYPE html>
<html>
<body>
<main>
  <article>
    <h2><a href="/1">Real Article One With Title</a></h2>
    <p>A genuine article with substantial description text for the first item.</p>
  </article>
  <article class="sponsored">
    <h2><a href="/sponsored">Sponsored Content Here</a></h2>
    <p>This is sponsored content and should be skipped.</p>
  </article>
  <article>
    <h2><a href="/2">Real Article Two With Title</a></h2>
    <p>Another genuine article with enough description text to qualify.</p>
  </article>
  <article class="ad-unit">
    <h2><a href="/ad">Advertisement Link</a></h2>
    <p>This is an ad and should also be skipped.</p>
  </article>
  <article>
    <h2><a href="/3">Real Article Three With Title</a></h2>
    <p>The third genuine article with sufficient content for a description.</p>
  </article>
</main>
</body>
</html>`

	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
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

	// Should have exactly 3 items (sponsored/ad articles skipped)
	if len(list.Children) != 3 {
		t.Errorf("expected 3 list items (ads skipped), got %d", len(list.Children))
	}

	// Verify none of the items are sponsored
	for _, item := range list.Children {
		text := item.PlainText()
		if strings.Contains(text, "Sponsored") || strings.Contains(text, "Advertisement") {
			t.Errorf("sponsored content should be skipped: %q", text)
		}
	}
}

func TestTruncateDescription(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"Short text", 100, "Short text"},
		{"This is a longer description that needs to be truncated at a word boundary", 50, "This is a longer description that needs to be..."},
		{"NoSpacesInThisTextSoItWillBeCutRightAtTheLimit", 20, "NoSpacesInThisTextSo..."},
		{"", 100, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input[:min(20, len(tt.input))], func(t *testing.T) {
			got := truncateDescription(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("truncateDescription(%q, %d) = %q, expected %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.example.com/path", "example.com"},
		{"http://news.site.org/article", "news.site.org"},
		{"/relative/path", ""},
		{"https://nytimes.com/section/tech", "nytimes.com"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractDomain(tt.url)
			if got != tt.expected {
				t.Errorf("extractDomain(%q) = %q, expected %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestFormatDate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2025-12-10T10:30:00Z", "Dec 10, 2025"},
		{"2025-01-05", "Jan 5, 2025"},
		{"2025-06-15T00:00:00.000Z", "Jun 15, 2025"},
		{"Dec 10", "Dec 10"}, // Already formatted, return as-is
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := formatDate(tt.input)
			if got != tt.expected {
				t.Errorf("formatDate(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLooksLikeNumberedItem(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Search results style - should match
		{"1. First Result Title", true},
		{"2. Second Result", true},
		{"10. Tenth Result", true},
		{"123. Some Title", true},
		{" 5. Leading space", true},

		// Real news headlines - should NOT match
		{"Why AI is Transforming Everything", false},
		{"Climate Summit Reaches Historic Agreement", false},
		{"Stock Markets Hit Record Highs", false},
		{"2025 Budget Proposal Released", false}, // Year, not numbered item
		{"The 5 Best Phones of 2025", false},     // Number in middle
		{"5G Networks Expand", false},            // No period after number
		{".", false},                             // Too short
		{"1", false},                             // Too short
		{"1.", false},                            // Too short
		{"", false},                              // Empty
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := looksLikeNumberedItem(tt.input)
			if got != tt.expected {
				t.Errorf("looksLikeNumberedItem(%q) = %v, expected %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLooksLikeNewsIndex(t *testing.T) {
	// Helper to create a simple list item with a link first
	makeListItem := func(href, title string) *Node {
		return &Node{
			Type: NodeListItem,
			Children: []*Node{
				{Type: NodeLink, Href: href, Children: []*Node{{Type: NodeText, Text: title}}},
			},
		}
	}

	// Helper to create a list with link-first items
	makeList := func(count int) *Node {
		list := &Node{Type: NodeList}
		for i := 0; i < count; i++ {
			list.Children = append(list.Children, makeListItem("http://example.com/"+string(rune('a'+i)), "Story "+string(rune('A'+i))))
		}
		return list
	}

	tests := []struct {
		name     string
		content  *Node
		expected bool
	}{
		{
			name:     "nil content",
			content:  nil,
			expected: false,
		},
		{
			name: "typical news index - many lists and anchors",
			content: &Node{
				Type: NodeDocument,
				Children: []*Node{
					{Type: NodeAnchor},
					makeList(3),
					{Type: NodeAnchor},
					{Type: NodeParagraph, Text: "Some description"},
					makeList(3),
					{Type: NodeAnchor},
					{Type: NodeAnchor},
					makeList(3),
					{Type: NodeAnchor},
				},
			},
			expected: true,
		},
		{
			name: "not enough lists",
			content: &Node{
				Type: NodeDocument,
				Children: []*Node{
					{Type: NodeAnchor},
					makeList(3),
					{Type: NodeAnchor},
					{Type: NodeAnchor},
					{Type: NodeAnchor},
					{Type: NodeAnchor},
				},
			},
			expected: false,
		},
		{
			name: "not enough anchors",
			content: &Node{
				Type: NodeDocument,
				Children: []*Node{
					makeList(3),
					makeList(3),
					makeList(3),
					{Type: NodeAnchor},
				},
			},
			expected: false,
		},
		{
			name: "regular article page - paragraphs only",
			content: &Node{
				Type: NodeDocument,
				Children: []*Node{
					{Type: NodeHeading1, Text: "Title"},
					{Type: NodeParagraph, Text: "Content paragraph 1"},
					{Type: NodeParagraph, Text: "Content paragraph 2"},
					{Type: NodeParagraph, Text: "Content paragraph 3"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeNewsIndex(tt.content)
			if got != tt.expected {
				t.Errorf("looksLikeNewsIndex() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestListHasLinkFirstItems(t *testing.T) {
	tests := []struct {
		name     string
		list     *Node
		expected bool
	}{
		{
			name:     "nil list",
			list:     nil,
			expected: false,
		},
		{
			name: "all items start with links",
			list: &Node{
				Type: NodeList,
				Children: []*Node{
					{Type: NodeListItem, Children: []*Node{{Type: NodeLink, Href: "a"}}},
					{Type: NodeListItem, Children: []*Node{{Type: NodeLink, Href: "b"}}},
					{Type: NodeListItem, Children: []*Node{{Type: NodeLink, Href: "c"}}},
				},
			},
			expected: true,
		},
		{
			name: "half items start with links",
			list: &Node{
				Type: NodeList,
				Children: []*Node{
					{Type: NodeListItem, Children: []*Node{{Type: NodeLink, Href: "a"}}},
					{Type: NodeListItem, Children: []*Node{{Type: NodeText, Text: "Not a link"}}},
					{Type: NodeListItem, Children: []*Node{{Type: NodeLink, Href: "c"}}},
					{Type: NodeListItem, Children: []*Node{{Type: NodeText, Text: "Also not a link"}}},
				},
			},
			expected: true, // 2/4 = 50%, which meets threshold
		},
		{
			name: "no items start with links",
			list: &Node{
				Type: NodeList,
				Children: []*Node{
					{Type: NodeListItem, Children: []*Node{{Type: NodeText, Text: "Text first"}}},
					{Type: NodeListItem, Children: []*Node{{Type: NodeStrong, Text: "Bold first"}}},
				},
			},
			expected: false,
		},
		{
			name:     "wrong node type",
			list:     &Node{Type: NodeParagraph},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listHasLinkFirstItems(tt.list)
			if got != tt.expected {
				t.Errorf("listHasLinkFirstItems() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestDiscoverFeeds(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected []FeedLink
	}{
		{
			name:     "no feeds",
			html:     `<html><head><title>Test</title></head><body>Content</body></html>`,
			expected: nil,
		},
		{
			name: "single RSS feed",
			html: `<html><head>
				<link rel="alternate" type="application/rss+xml" href="/feed.xml" title="RSS Feed">
			</head><body>Content</body></html>`,
			expected: []FeedLink{{URL: "/feed.xml", Title: "RSS Feed", Type: "rss"}},
		},
		{
			name: "single Atom feed",
			html: `<html><head>
				<link rel="alternate" type="application/atom+xml" href="/atom.xml" title="Atom Feed">
			</head><body>Content</body></html>`,
			expected: []FeedLink{{URL: "/atom.xml", Title: "Atom Feed", Type: "atom"}},
		},
		{
			name: "multiple feeds",
			html: `<html><head>
				<link rel="alternate" type="application/rss+xml" href="/rss.xml" title="RSS">
				<link rel="alternate" type="application/atom+xml" href="/atom.xml" title="Atom">
			</head><body>Content</body></html>`,
			expected: []FeedLink{
				{URL: "/rss.xml", Title: "RSS", Type: "rss"},
				{URL: "/atom.xml", Title: "Atom", Type: "atom"},
			},
		},
		{
			name: "ignores non-feed links",
			html: `<html><head>
				<link rel="stylesheet" href="/style.css">
				<link rel="alternate" type="application/rss+xml" href="/feed.xml">
				<link rel="canonical" href="https://example.com">
			</head><body>Content</body></html>`,
			expected: []FeedLink{{URL: "/feed.xml", Title: "", Type: "rss"}},
		},
		{
			name: "absolute URL",
			html: `<html><head>
				<link rel="alternate" type="application/rss+xml" href="https://example.com/feed.xml" title="Feed">
			</head><body>Content</body></html>`,
			expected: []FeedLink{{URL: "https://example.com/feed.xml", Title: "Feed", Type: "rss"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feeds := DiscoverFeeds(tt.html)
			if len(feeds) != len(tt.expected) {
				t.Errorf("got %d feeds, expected %d", len(feeds), len(tt.expected))
				return
			}
			for i, feed := range feeds {
				if feed.URL != tt.expected[i].URL {
					t.Errorf("feed[%d].URL = %q, expected %q", i, feed.URL, tt.expected[i].URL)
				}
				if feed.Title != tt.expected[i].Title {
					t.Errorf("feed[%d].Title = %q, expected %q", i, feed.Title, tt.expected[i].Title)
				}
				if feed.Type != tt.expected[i].Type {
					t.Errorf("feed[%d].Type = %q, expected %q", i, feed.Type, tt.expected[i].Type)
				}
			}
		})
	}
}
