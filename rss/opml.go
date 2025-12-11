package rss

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// OPML represents an OPML document for RSS subscription import/export.
type OPML struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    OPMLHead `xml:"head"`
	Body    OPMLBody `xml:"body"`
}

// OPMLHead contains OPML document metadata.
type OPMLHead struct {
	Title       string `xml:"title,omitempty"`
	DateCreated string `xml:"dateCreated,omitempty"`
	OwnerName   string `xml:"ownerName,omitempty"`
}

// OPMLBody contains the outline elements.
type OPMLBody struct {
	Outlines []OPMLOutline `xml:"outline"`
}

// OPMLOutline represents an outline element (feed or category).
type OPMLOutline struct {
	Text     string        `xml:"text,attr,omitempty"`
	Title    string        `xml:"title,attr,omitempty"`
	Type     string        `xml:"type,attr,omitempty"`
	XMLURL   string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string        `xml:"htmlUrl,attr,omitempty"`
	Category string        `xml:"category,attr,omitempty"`
	Outlines []OPMLOutline `xml:"outline,omitempty"` // Nested outlines (categories)
}

// ImportOPML parses an OPML file and returns the feeds found.
func ImportOPML(data []byte) ([]Feed, error) {
	var opml OPML
	if err := xml.Unmarshal(data, &opml); err != nil {
		return nil, fmt.Errorf("parsing OPML: %w", err)
	}

	var feeds []Feed
	var extractFeeds func(outlines []OPMLOutline, category string)
	extractFeeds = func(outlines []OPMLOutline, category string) {
		for _, outline := range outlines {
			// If this outline has nested outlines, it's a category
			if len(outline.Outlines) > 0 {
				// Build category path
				catPath := outline.Text
				if catPath == "" {
					catPath = outline.Title
				}
				if category != "" {
					catPath = category + "/" + catPath
				}
				extractFeeds(outline.Outlines, catPath)
				continue
			}

			// If it has an XML URL, it's a feed
			if outline.XMLURL != "" {
				title := outline.Title
				if title == "" {
					title = outline.Text
				}
				feeds = append(feeds, Feed{
					URL:      outline.XMLURL,
					Title:    title,
					SiteURL:  outline.HTMLURL,
					Category: category,
					AddedAt:  time.Now(),
				})
			}
		}
	}

	extractFeeds(opml.Body.Outlines, "")
	return feeds, nil
}

// ExportOPML generates an OPML file from the given feeds.
func ExportOPML(feeds []Feed, title string) ([]byte, error) {
	opml := OPML{
		Version: "2.0",
		Head: OPMLHead{
			Title:       title,
			DateCreated: time.Now().Format(time.RFC1123),
		},
	}

	// Group feeds by category
	categories := make(map[string][]Feed)
	for _, feed := range feeds {
		cat := feed.Category
		if cat == "" {
			cat = "__uncategorized__"
		}
		categories[cat] = append(categories[cat], feed)
	}

	// Build outline structure
	for category, catFeeds := range categories {
		if category == "__uncategorized__" {
			// Add uncategorized feeds directly to body
			for _, feed := range catFeeds {
				opml.Body.Outlines = append(opml.Body.Outlines, OPMLOutline{
					Type:    "rss",
					Text:    feed.Title,
					Title:   feed.Title,
					XMLURL:  feed.URL,
					HTMLURL: feed.SiteURL,
				})
			}
		} else {
			// Create category outline with nested feeds
			catOutline := OPMLOutline{
				Text:  category,
				Title: category,
			}
			for _, feed := range catFeeds {
				catOutline.Outlines = append(catOutline.Outlines, OPMLOutline{
					Type:    "rss",
					Text:    feed.Title,
					Title:   feed.Title,
					XMLURL:  feed.URL,
					HTMLURL: feed.SiteURL,
				})
			}
			opml.Body.Outlines = append(opml.Body.Outlines, catOutline)
		}
	}

	// Generate XML with proper formatting
	output, err := xml.MarshalIndent(opml, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("generating OPML: %w", err)
	}

	// Add XML declaration
	result := []byte(xml.Header)
	result = append(result, output...)

	return result, nil
}

// MergeOPMLFeeds merges imported feeds with existing store, avoiding duplicates.
// Returns the number of new feeds added.
func (s *Store) MergeOPMLFeeds(feeds []Feed) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build set of existing URLs
	existing := make(map[string]bool)
	for _, f := range s.Feeds {
		existing[strings.ToLower(f.URL)] = true
	}

	added := 0
	for _, feed := range feeds {
		if !existing[strings.ToLower(feed.URL)] {
			s.Feeds = append(s.Feeds, feed)
			existing[strings.ToLower(feed.URL)] = true
			added++
		}
	}

	return added
}
