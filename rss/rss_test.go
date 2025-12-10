package rss

import (
	"testing"
	"time"
)

func TestParseRSS2(t *testing.T) {
	// Sample RSS 2.0 feed
	rss2 := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example News</title>
    <link>https://example.com</link>
    <description>Latest news from Example</description>
    <item>
      <title>Breaking: Something Happened</title>
      <link>https://example.com/article/1</link>
      <description>Details about the event...</description>
      <guid>article-1</guid>
      <pubDate>Mon, 10 Dec 2025 15:04:05 +0000</pubDate>
      <author>john@example.com</author>
    </item>
    <item>
      <title>Another Story</title>
      <link>https://example.com/article/2</link>
      <description>More news content...</description>
      <guid>article-2</guid>
      <pubDate>Sun, 09 Dec 2025 10:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`

	feed, err := Parse([]byte(rss2))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if feed.Type != FeedTypeRSS {
		t.Errorf("expected FeedTypeRSS, got %v", feed.Type)
	}
	if feed.Title != "Example News" {
		t.Errorf("expected title 'Example News', got %q", feed.Title)
	}
	if feed.Link != "https://example.com" {
		t.Errorf("expected link 'https://example.com', got %q", feed.Link)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(feed.Items))
	}

	item := feed.Items[0]
	if item.Title != "Breaking: Something Happened" {
		t.Errorf("expected title 'Breaking: Something Happened', got %q", item.Title)
	}
	if item.GUID != "article-1" {
		t.Errorf("expected GUID 'article-1', got %q", item.GUID)
	}
	if item.Published.IsZero() {
		t.Error("expected non-zero publish date")
	}
}

func TestParseAtom(t *testing.T) {
	// Sample Atom feed
	atom := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Blog</title>
  <subtitle>Thoughts and musings</subtitle>
  <link href="https://blog.example.com" rel="alternate"/>
  <link href="https://blog.example.com/feed.xml" rel="self"/>
  <entry>
    <title>My First Post</title>
    <link href="https://blog.example.com/posts/first" rel="alternate"/>
    <id>urn:uuid:1234-5678-90ab</id>
    <published>2025-12-10T12:00:00Z</published>
    <summary>This is my first blog post...</summary>
    <author>
      <name>Jane Doe</name>
    </author>
  </entry>
  <entry>
    <title>Second Post</title>
    <link href="https://blog.example.com/posts/second" rel="alternate"/>
    <id>urn:uuid:abcd-efgh-ijkl</id>
    <updated>2025-12-09T08:30:00Z</updated>
    <summary>Another post here...</summary>
  </entry>
</feed>`

	feed, err := Parse([]byte(atom))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if feed.Type != FeedTypeAtom {
		t.Errorf("expected FeedTypeAtom, got %v", feed.Type)
	}
	if feed.Title != "Example Blog" {
		t.Errorf("expected title 'Example Blog', got %q", feed.Title)
	}
	if feed.Description != "Thoughts and musings" {
		t.Errorf("expected description 'Thoughts and musings', got %q", feed.Description)
	}
	if feed.Link != "https://blog.example.com" {
		t.Errorf("expected link 'https://blog.example.com', got %q", feed.Link)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(feed.Items))
	}

	item := feed.Items[0]
	if item.Title != "My First Post" {
		t.Errorf("expected title 'My First Post', got %q", item.Title)
	}
	if item.Author != "Jane Doe" {
		t.Errorf("expected author 'Jane Doe', got %q", item.Author)
	}
	if item.GUID != "urn:uuid:1234-5678-90ab" {
		t.Errorf("expected GUID 'urn:uuid:1234-5678-90ab', got %q", item.GUID)
	}
}

func TestParseDateFormats(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Time
	}{
		{
			input:    "Mon, 02 Jan 2006 15:04:05 -0700",
			expected: time.Date(2006, 1, 2, 15, 4, 5, 0, time.FixedZone("", -7*3600)),
		},
		{
			input:    "2025-12-10T12:00:00Z",
			expected: time.Date(2025, 12, 10, 12, 0, 0, 0, time.UTC),
		},
		{
			input:    "2025-12-10",
			expected: time.Date(2025, 12, 10, 0, 0, 0, 0, time.UTC),
		},
		{
			input:    "", // Empty should return zero time
			expected: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDate(tt.input)
			if tt.expected.IsZero() {
				if !got.IsZero() {
					t.Errorf("expected zero time, got %v", got)
				}
				return
			}
			if !got.Equal(tt.expected) {
				t.Errorf("parseDate(%q) = %v, expected %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHashGUID(t *testing.T) {
	// Same input should produce same output
	guid1 := hashGUID("https://example.com/article/1")
	guid2 := hashGUID("https://example.com/article/1")
	if guid1 != guid2 {
		t.Error("same URL should produce same GUID")
	}

	// Different inputs should produce different outputs
	guid3 := hashGUID("https://example.com/article/2")
	if guid1 == guid3 {
		t.Error("different URLs should produce different GUIDs")
	}

	// Empty input should return empty
	if hashGUID("") != "" {
		t.Error("empty URL should return empty GUID")
	}
}

func TestCleanText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"  hello  world  ", "hello world"},
		{"line1\n\n\nline2", "line1 line2"},
		{"&amp; &lt; &gt;", "& < >"},
		{"  \t\n  ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanText(tt.input)
			if got != tt.expected {
				t.Errorf("cleanText(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseInvalidFeed(t *testing.T) {
	invalid := []string{
		"not xml at all",
		"<html><body>Not a feed</body></html>",
		"<?xml version=\"1.0\"?><something>random</something>",
	}

	for _, data := range invalid {
		_, err := Parse([]byte(data))
		if err == nil {
			t.Errorf("expected error for invalid feed: %s", data[:20])
		}
	}
}

func TestParseGUIDFallback(t *testing.T) {
	// RSS without GUID should generate one from link
	rss := `<?xml version="1.0"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <item>
      <title>No GUID Item</title>
      <link>https://example.com/no-guid</link>
    </item>
  </channel>
</rss>`

	feed, err := Parse([]byte(rss))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(feed.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(feed.Items))
	}

	if feed.Items[0].GUID == "" {
		t.Error("expected generated GUID, got empty")
	}
}

func TestParseDCCreator(t *testing.T) {
	// RSS with dc:creator instead of author
	rss := `<?xml version="1.0"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>Test</title>
    <item>
      <title>DC Creator Test</title>
      <link>https://example.com/dc</link>
      <dc:creator>John Smith</dc:creator>
    </item>
  </channel>
</rss>`

	feed, err := Parse([]byte(rss))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if feed.Items[0].Author != "John Smith" {
		t.Errorf("expected author 'John Smith', got %q", feed.Items[0].Author)
	}
}
