package rules

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestBuildSelectorTree(t *testing.T) {
	selectors := map[string]string{
		"Sections":            "section[]",
		"Sections.name":       ".title",
		"Sections.items":      ".card[]",
		"Sections.items.text": ".headline",
		"Sections.items.time": ".time",
	}

	tree := buildSelectorTree(selectors)

	// Check root level
	if tree["Sections"] == nil {
		t.Fatal("expected Sections in tree")
	}
	if tree["Sections"].selector != "section[]" {
		t.Errorf("expected 'section[]', got %q", tree["Sections"].selector)
	}
	if !tree["Sections"].isArray {
		t.Error("expected Sections to be an array")
	}

	// Check first level children
	if tree["Sections"].children["name"] == nil {
		t.Fatal("expected name child")
	}
	if tree["Sections"].children["name"].selector != ".title" {
		t.Errorf("expected '.title', got %q", tree["Sections"].children["name"].selector)
	}

	// Check nested items
	items := tree["Sections"].children["items"]
	if items == nil {
		t.Fatal("expected items child")
	}
	if items.selector != ".card[]" {
		t.Errorf("expected '.card[]', got %q", items.selector)
	}
	if !items.isArray {
		t.Error("expected items to be an array")
	}

	// Check deeply nested children
	if items.children["text"] == nil {
		t.Fatal("expected text child inside items")
	}
	if items.children["text"].selector != ".headline" {
		t.Errorf("expected '.headline', got %q", items.children["text"].selector)
	}
}

func TestExtractWithSelectorsNestedHierarchy(t *testing.T) {
	html := `<html>
<body>
	<section>
		<h2 class="title">Europe</h2>
		<div class="card">
			<span class="headline">Brexit news</span>
			<span class="time">2h ago</span>
		</div>
		<div class="card">
			<span class="headline">Paris protests</span>
			<span class="time">3h ago</span>
		</div>
	</section>
	<section>
		<h2 class="title">World</h2>
		<div class="card">
			<span class="headline">US elections</span>
			<span class="time">1h ago</span>
		</div>
	</section>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}

	selectors := map[string]string{
		"Sections":            "section[]",
		"Sections.name":       ".title",
		"Sections.items":      ".card[]",
		"Sections.items.text": ".headline",
		"Sections.items.time": ".time",
	}

	data := extractWithSelectors(doc, selectors)

	// Check we got sections
	sections, ok := data["Sections"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", data["Sections"])
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}

	// Check first section
	if sections[0]["name"] != "Europe" {
		t.Errorf("expected 'Europe', got %q", sections[0]["name"])
	}

	items, ok := sections[0]["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected items array, got %T", sections[0]["items"])
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items in Europe, got %d", len(items))
	}
	if items[0]["text"] != "Brexit news" {
		t.Errorf("expected 'Brexit news', got %q", items[0]["text"])
	}
	if items[0]["time"] != "2h ago" {
		t.Errorf("expected '2h ago', got %q", items[0]["time"])
	}

	// Check second section
	if sections[1]["name"] != "World" {
		t.Errorf("expected 'World', got %q", sections[1]["name"])
	}

	worldItems, ok := sections[1]["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected items array in World, got %T", sections[1]["items"])
	}
	if len(worldItems) != 1 {
		t.Errorf("expected 1 item in World, got %d", len(worldItems))
	}
	if worldItems[0]["text"] != "US elections" {
		t.Errorf("expected 'US elections', got %q", worldItems[0]["text"])
	}
}

func TestExtractWithSelectorsFlatArray(t *testing.T) {
	html := `<html>
<body>
	<div class="story">
		<a href="/story1" class="headline">First story</a>
		<span class="meta">5 points</span>
	</div>
	<div class="story">
		<a href="/story2" class="headline">Second story</a>
		<span class="meta">10 points</span>
	</div>
</body>
</html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}

	selectors := map[string]string{
		"Items":      ".story[]",
		"Items.text": ".headline",
		"Items.meta": ".meta",
	}

	data := extractWithSelectors(doc, selectors)

	items, ok := data["Items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", data["Items"])
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	if items[0]["text"] != "First story" {
		t.Errorf("expected 'First story', got %q", items[0]["text"])
	}
	if items[0]["meta"] != "5 points" {
		t.Errorf("expected '5 points', got %q", items[0]["meta"])
	}

	// Check href was extracted
	if items[0]["href"] != "/story1" {
		t.Errorf("expected '/story1' href, got %q", items[0]["href"])
	}
}
