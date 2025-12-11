package rss

import (
	"strings"
	"testing"
	"time"
)

func TestImportOPML(t *testing.T) {
	opmlData := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head>
    <title>My Feeds</title>
  </head>
  <body>
    <outline text="BBC News" title="BBC News" type="rss" xmlUrl="https://feeds.bbci.co.uk/news/rss.xml" htmlUrl="https://www.bbc.com/news"/>
    <outline text="Tech" title="Tech">
      <outline text="HN" title="Hacker News" type="rss" xmlUrl="https://news.ycombinator.com/rss" htmlUrl="https://news.ycombinator.com"/>
      <outline text="Go Blog" title="Go Blog" type="rss" xmlUrl="https://go.dev/blog/feed.atom" htmlUrl="https://go.dev/blog"/>
    </outline>
  </body>
</opml>`

	feeds, err := ImportOPML([]byte(opmlData))
	if err != nil {
		t.Fatalf("ImportOPML failed: %v", err)
	}

	if len(feeds) != 3 {
		t.Errorf("expected 3 feeds, got %d", len(feeds))
	}

	// Check uncategorized feed
	if feeds[0].URL != "https://feeds.bbci.co.uk/news/rss.xml" {
		t.Errorf("expected BBC News URL, got %s", feeds[0].URL)
	}
	if feeds[0].Category != "" {
		t.Errorf("expected empty category, got %s", feeds[0].Category)
	}

	// Check categorized feeds
	if feeds[1].Category != "Tech" {
		t.Errorf("expected Tech category, got %s", feeds[1].Category)
	}
	if feeds[2].Category != "Tech" {
		t.Errorf("expected Tech category, got %s", feeds[2].Category)
	}
}

func TestImportOPMLNestedCategories(t *testing.T) {
	opmlData := `<?xml version="1.0"?>
<opml version="2.0">
  <body>
    <outline text="News">
      <outline text="Tech">
        <outline text="Feed1" type="rss" xmlUrl="https://example.com/feed1.xml"/>
      </outline>
    </outline>
  </body>
</opml>`

	feeds, err := ImportOPML([]byte(opmlData))
	if err != nil {
		t.Fatalf("ImportOPML failed: %v", err)
	}

	if len(feeds) != 1 {
		t.Errorf("expected 1 feed, got %d", len(feeds))
	}

	if feeds[0].Category != "News/Tech" {
		t.Errorf("expected nested category 'News/Tech', got %s", feeds[0].Category)
	}
}

func TestExportOPML(t *testing.T) {
	feeds := []Feed{
		{URL: "https://example.com/feed1.xml", Title: "Feed 1", SiteURL: "https://example.com"},
		{URL: "https://example.com/feed2.xml", Title: "Feed 2", Category: "Tech"},
	}

	data, err := ExportOPML(feeds, "My Subscriptions")
	if err != nil {
		t.Fatalf("ExportOPML failed: %v", err)
	}

	opmlStr := string(data)

	// Check XML declaration
	if !strings.HasPrefix(opmlStr, "<?xml version=") {
		t.Error("missing XML declaration")
	}

	// Check OPML structure
	if !strings.Contains(opmlStr, `<opml version="2.0">`) {
		t.Error("missing OPML version")
	}

	if !strings.Contains(opmlStr, `<title>My Subscriptions</title>`) {
		t.Error("missing title")
	}

	// Check feeds are included
	if !strings.Contains(opmlStr, `xmlUrl="https://example.com/feed1.xml"`) {
		t.Error("missing feed1 URL")
	}

	if !strings.Contains(opmlStr, `xmlUrl="https://example.com/feed2.xml"`) {
		t.Error("missing feed2 URL")
	}
}

func TestExportImportRoundtrip(t *testing.T) {
	original := []Feed{
		{URL: "https://example.com/feed1.xml", Title: "Feed 1", SiteURL: "https://example.com"},
		{URL: "https://example.com/feed2.xml", Title: "Feed 2", SiteURL: "https://example2.com", Category: "News"},
	}

	// Export
	data, err := ExportOPML(original, "Test")
	if err != nil {
		t.Fatalf("ExportOPML failed: %v", err)
	}

	// Import
	imported, err := ImportOPML(data)
	if err != nil {
		t.Fatalf("ImportOPML failed: %v", err)
	}

	if len(imported) != len(original) {
		t.Errorf("expected %d feeds, got %d", len(original), len(imported))
	}

	// Check URLs match (order may differ due to category grouping)
	urls := make(map[string]bool)
	for _, f := range imported {
		urls[f.URL] = true
	}

	for _, f := range original {
		if !urls[f.URL] {
			t.Errorf("missing feed URL: %s", f.URL)
		}
	}
}

func TestMergeOPMLFeeds(t *testing.T) {
	store := &Store{
		Feeds: []Feed{
			{URL: "https://existing.com/feed.xml", Title: "Existing", AddedAt: time.Now()},
		},
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	newFeeds := []Feed{
		{URL: "https://existing.com/feed.xml", Title: "Duplicate"},           // Should be skipped
		{URL: "https://EXISTING.com/feed.xml", Title: "Duplicate Case"},      // Case-insensitive duplicate
		{URL: "https://new.com/feed.xml", Title: "New Feed", AddedAt: time.Now()},
	}

	added := store.MergeOPMLFeeds(newFeeds)

	if added != 1 {
		t.Errorf("expected 1 new feed added, got %d", added)
	}

	if len(store.Feeds) != 2 {
		t.Errorf("expected 2 total feeds, got %d", len(store.Feeds))
	}
}
