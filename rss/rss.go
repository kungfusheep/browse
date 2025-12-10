// Package rss provides RSS and Atom feed parsing.
package rss

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"html"
	"strings"
	"time"
)

// FeedType distinguishes RSS from Atom feeds.
type FeedType int

const (
	FeedTypeRSS FeedType = iota
	FeedTypeAtom
)

// ParsedFeed represents a parsed RSS or Atom feed.
type ParsedFeed struct {
	Type        FeedType
	Title       string
	Description string
	Link        string // Site URL
	Items       []ParsedItem
}

// ParsedItem represents a single item from a feed.
type ParsedItem struct {
	GUID        string
	Title       string
	Link        string
	Description string
	Content     string
	Author      string
	Published   time.Time
}

// Parse detects the feed type and parses accordingly.
func Parse(data []byte) (*ParsedFeed, error) {
	// Trim BOM if present
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	// Try RSS 2.0 first (most common)
	if feed, err := parseRSS(data); err == nil && feed != nil {
		return feed, nil
	}

	// Try Atom
	if feed, err := parseAtom(data); err == nil && feed != nil {
		return feed, nil
	}

	// Try RSS 1.0 (RDF)
	if feed, err := parseRDF(data); err == nil && feed != nil {
		return feed, nil
	}

	return nil, errors.New("unable to parse feed: not valid RSS or Atom")
}

// RSS 2.0 structures
type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Author      string `xml:"author"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
}

func parseRSS(data []byte) (*ParsedFeed, error) {
	var root rssRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	// Validate it's actually RSS
	if root.Channel.Title == "" && len(root.Channel.Items) == 0 {
		return nil, errors.New("not a valid RSS feed")
	}

	feed := &ParsedFeed{
		Type:        FeedTypeRSS,
		Title:       cleanText(root.Channel.Title),
		Description: cleanText(root.Channel.Description),
		Link:        root.Channel.Link,
	}

	for _, item := range root.Channel.Items {
		parsed := ParsedItem{
			GUID:        item.GUID,
			Title:       cleanText(item.Title),
			Link:        item.Link,
			Description: cleanText(item.Description),
			Content:     item.Content,
			Author:      firstNonEmpty(item.Author, item.Creator),
			Published:   parseDate(item.PubDate),
		}

		// Generate GUID from link if missing
		if parsed.GUID == "" {
			parsed.GUID = hashGUID(parsed.Link)
		}

		feed.Items = append(feed.Items, parsed)
	}

	return feed, nil
}

// Atom structures
type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle"`
	Link     []atomLink  `xml:"link"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type atomEntry struct {
	Title     string      `xml:"title"`
	Link      []atomLink  `xml:"link"`
	ID        string      `xml:"id"`
	Published string      `xml:"published"`
	Updated   string      `xml:"updated"`
	Summary   string      `xml:"summary"`
	Content   atomContent `xml:"content"`
	Author    atomAuthor  `xml:"author"`
}

type atomContent struct {
	Type    string `xml:"type,attr"`
	Content string `xml:",chardata"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

func parseAtom(data []byte) (*ParsedFeed, error) {
	var root atomFeed
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	// Validate it's actually Atom
	if root.Title == "" && len(root.Entries) == 0 {
		return nil, errors.New("not a valid Atom feed")
	}

	// Find alternate link (main site URL)
	var link string
	for _, l := range root.Link {
		if l.Rel == "alternate" || l.Rel == "" {
			link = l.Href
			break
		}
	}

	feed := &ParsedFeed{
		Type:        FeedTypeAtom,
		Title:       cleanText(root.Title),
		Description: cleanText(root.Subtitle),
		Link:        link,
	}

	for _, entry := range root.Entries {
		var itemLink string
		for _, l := range entry.Link {
			if l.Rel == "alternate" || l.Rel == "" {
				itemLink = l.Href
				break
			}
		}

		parsed := ParsedItem{
			GUID:        entry.ID,
			Title:       cleanText(entry.Title),
			Link:        itemLink,
			Description: cleanText(entry.Summary),
			Content:     entry.Content.Content,
			Author:      entry.Author.Name,
			Published:   parseDate(firstNonEmpty(entry.Published, entry.Updated)),
		}

		// Generate GUID from ID or link if needed
		if parsed.GUID == "" {
			parsed.GUID = hashGUID(parsed.Link)
		}

		feed.Items = append(feed.Items, parsed)
	}

	return feed, nil
}

// RDF/RSS 1.0 structures
type rdfRoot struct {
	XMLName xml.Name   `xml:"http://www.w3.org/1999/02/22-rdf-syntax-ns# RDF"`
	Channel rdfChannel `xml:"http://purl.org/rss/1.0/ channel"`
	Items   []rdfItem  `xml:"http://purl.org/rss/1.0/ item"`
}

type rdfChannel struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
}

type rdfItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Date        string `xml:"http://purl.org/dc/elements/1.1/ date"`
	Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
}

func parseRDF(data []byte) (*ParsedFeed, error) {
	var root rdfRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, err
	}

	// Validate it's actually RDF/RSS 1.0
	if root.Channel.Title == "" && len(root.Items) == 0 {
		return nil, errors.New("not a valid RDF feed")
	}

	feed := &ParsedFeed{
		Type:        FeedTypeRSS,
		Title:       cleanText(root.Channel.Title),
		Description: cleanText(root.Channel.Description),
		Link:        root.Channel.Link,
	}

	for _, item := range root.Items {
		parsed := ParsedItem{
			GUID:        hashGUID(item.Link),
			Title:       cleanText(item.Title),
			Link:        item.Link,
			Description: cleanText(item.Description),
			Author:      item.Creator,
			Published:   parseDate(item.Date),
		}
		feed.Items = append(feed.Items, parsed)
	}

	return feed, nil
}

// parseDate attempts to parse various date formats used in feeds.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}

	// Common date formats in RSS/Atom feeds
	formats := []string{
		time.RFC1123Z,                    // "Mon, 02 Jan 2006 15:04:05 -0700"
		time.RFC1123,                     // "Mon, 02 Jan 2006 15:04:05 MST"
		time.RFC3339,                     // "2006-01-02T15:04:05Z07:00"
		time.RFC3339Nano,                 // "2006-01-02T15:04:05.999999999Z07:00"
		"2006-01-02T15:04:05Z",           // ISO without timezone
		"2006-01-02T15:04:05",            // ISO without timezone
		"2006-01-02",                     // Just date
		"Mon, 2 Jan 2006 15:04:05 -0700", // RFC1123Z variant
		"Mon, 2 Jan 2006 15:04:05 MST",   // RFC1123 variant
		"02 Jan 2006 15:04:05 -0700",     // Without day name
		"02 Jan 2006 15:04:05 MST",       // Without day name
		"2 Jan 2006 15:04:05 -0700",      // Single digit day
		"January 2, 2006",                // Long format
		"Jan 2, 2006",                    // Short format
	}

	for _, format := range formats {
		if t, err := time.Parse(format, s); err == nil {
			return t
		}
	}

	return time.Time{}
}

// hashGUID generates a stable GUID from a URL.
func hashGUID(url string) string {
	if url == "" {
		return ""
	}
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:8]) // Use first 8 bytes
}

// cleanText removes HTML entities and excess whitespace.
func cleanText(s string) string {
	s = html.UnescapeString(s)
	s = strings.TrimSpace(s)
	// Collapse multiple spaces/newlines
	var result strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			if !prevSpace {
				result.WriteRune(' ')
				prevSpace = true
			}
		} else {
			result.WriteRune(r)
			prevSpace = false
		}
	}
	return strings.TrimSpace(result.String())
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(strs ...string) string {
	for _, s := range strs {
		if s != "" {
			return s
		}
	}
	return ""
}
