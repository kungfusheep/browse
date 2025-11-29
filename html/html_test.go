package html

import (
	"testing"
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

	if doc.Type != NodeDocument {
		t.Errorf("expected NodeDocument, got %v", doc.Type)
	}

	if len(doc.Children) < 4 {
		t.Fatalf("expected at least 4 children, got %d", len(doc.Children))
	}

	// Check h1
	if doc.Children[0].Type != NodeHeading1 {
		t.Errorf("expected NodeHeading1, got %v", doc.Children[0].Type)
	}
	if doc.Children[0].Text != "Test Title" {
		t.Errorf("expected 'Test Title', got %q", doc.Children[0].Text)
	}

	// Check paragraph
	if doc.Children[1].Type != NodeParagraph {
		t.Errorf("expected NodeParagraph, got %v", doc.Children[1].Type)
	}

	// Check h2
	if doc.Children[2].Type != NodeHeading2 {
		t.Errorf("expected NodeHeading2, got %v", doc.Children[2].Type)
	}

	// Check list
	if doc.Children[3].Type != NodeList {
		t.Errorf("expected NodeList, got %v", doc.Children[3].Type)
	}
	if len(doc.Children[3].Children) != 2 {
		t.Errorf("expected 2 list items, got %d", len(doc.Children[3].Children))
	}
}

func TestPlainText(t *testing.T) {
	input := `<article><p>Hello <strong>world</strong>!</p></article>`
	doc, err := ParseString(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(doc.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(doc.Children))
	}

	text := doc.Children[0].PlainText()
	if text != "Hello world!" {
		t.Errorf("PlainText() = %q, expected 'Hello world!'", text)
	}
}
