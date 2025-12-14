package preview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Meta contains metadata about a URL for preview display
type Meta struct {
	FinalURL    string
	Title       string
	Description string
	SiteName    string
	ContentType string // Article, Discussion, Repository, Video, etc.
	ReadingTime string // "5 min read", "< 1 min read"
	Extra       string // Site-specific extra info (stars, points, etc.)
	Error       error
}

// Fetcher handles metadata fetching with smart site detection
type Fetcher struct {
	client    *http.Client
	userAgent string
}

// NewFetcher creates a new preview metadata fetcher
func NewFetcher(userAgent string) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		userAgent: userAgent,
	}
}

// Fetch retrieves metadata for a URL, using smart site detection where available
func (f *Fetcher) Fetch(ctx context.Context, targetURL string) *Meta {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return &Meta{FinalURL: targetURL, Error: err}
	}

	host := strings.ToLower(parsed.Host)
	host = strings.TrimPrefix(host, "www.")

	// Try smart site handlers first
	switch {
	case host == "github.com":
		if meta := f.fetchGitHub(ctx, parsed); meta != nil {
			return meta
		}
	case host == "news.ycombinator.com":
		if meta := f.fetchHackerNews(ctx, parsed); meta != nil {
			return meta
		}
	case host == "reddit.com" || host == "old.reddit.com":
		if meta := f.fetchReddit(ctx, parsed); meta != nil {
			return meta
		}
	case host == "youtube.com" || host == "youtu.be" || host == "m.youtube.com":
		if meta := f.fetchYouTube(ctx, targetURL); meta != nil {
			return meta
		}
	case strings.HasSuffix(host, ".wikipedia.org"):
		if meta := f.fetchWikipedia(ctx, parsed); meta != nil {
			return meta
		}
	}

	// Fallback to generic OG tag fetch
	return f.fetchGeneric(ctx, targetURL)
}

// fetchGitHub fetches metadata from GitHub API
func (f *Fetcher) fetchGitHub(ctx context.Context, u *url.URL) *Meta {
	// Match /owner/repo pattern
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil
	}
	owner, repo := parts[0], parts[1]

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := f.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Stars       int    `json:"stargazers_count"`
		Language    string `json:"language"`
		License     struct {
			Name string `json:"name"`
		} `json:"license"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	extra := fmt.Sprintf("⭐ %s", formatCount(data.Stars))
	if data.Language != "" {
		extra += " · " + data.Language
	}

	return &Meta{
		FinalURL:    u.String(),
		Title:       data.FullName,
		Description: data.Description,
		SiteName:    "GitHub",
		ContentType: "Repository",
		Extra:       extra,
	}
}

// fetchHackerNews fetches metadata from HN Firebase API
func (f *Fetcher) fetchHackerNews(ctx context.Context, u *url.URL) *Meta {
	// Extract item ID from ?id=XXX
	id := u.Query().Get("id")
	if id == "" {
		return nil
	}

	apiURL := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%s.json", id)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Title       string `json:"title"`
		Score       int    `json:"score"`
		Descendants int    `json:"descendants"`
		By          string `json:"by"`
		Type        string `json:"type"`
		URL         string `json:"url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	contentType := "Discussion"
	if data.Type == "job" {
		contentType = "Job Posting"
	} else if data.Type == "poll" {
		contentType = "Poll"
	}

	extra := fmt.Sprintf("%d points · %d comments", data.Score, data.Descendants)

	desc := ""
	if data.URL != "" {
		desc = data.URL
	}

	return &Meta{
		FinalURL:    u.String(),
		Title:       data.Title,
		Description: desc,
		SiteName:    "Hacker News",
		ContentType: contentType,
		Extra:       extra,
	}
}

// fetchReddit fetches metadata from Reddit JSON API
func (f *Fetcher) fetchReddit(ctx context.Context, u *url.URL) *Meta {
	// Match /r/subreddit/comments/id pattern
	re := regexp.MustCompile(`/r/([^/]+)/comments/([^/]+)`)
	matches := re.FindStringSubmatch(u.Path)
	if len(matches) < 3 {
		return nil
	}

	// Reddit requires a specific user agent
	apiURL := u.String() + ".json"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	var data []struct {
		Data struct {
			Children []struct {
				Data struct {
					Title     string `json:"title"`
					Subreddit string `json:"subreddit"`
					Score     int    `json:"score"`
					NumComments int  `json:"num_comments"`
					Selftext  string `json:"selftext"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	if len(data) == 0 || len(data[0].Data.Children) == 0 {
		return nil
	}

	post := data[0].Data.Children[0].Data
	extra := fmt.Sprintf("r/%s · ▲ %s · %d comments", post.Subreddit, formatCount(post.Score), post.NumComments)

	desc := post.Selftext
	if len(desc) > 200 {
		desc = desc[:200] + "..."
	}

	return &Meta{
		FinalURL:    u.String(),
		Title:       post.Title,
		Description: desc,
		SiteName:    "Reddit",
		ContentType: "Discussion",
		Extra:       extra,
	}
}

// fetchYouTube fetches metadata using oEmbed
func (f *Fetcher) fetchYouTube(ctx context.Context, targetURL string) *Meta {
	apiURL := fmt.Sprintf("https://www.youtube.com/oembed?url=%s&format=json", url.QueryEscape(targetURL))
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Title      string `json:"title"`
		AuthorName string `json:"author_name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	return &Meta{
		FinalURL:    targetURL,
		Title:       data.Title,
		Description: "",
		SiteName:    "YouTube",
		ContentType: "Video",
		Extra:       data.AuthorName,
	}
}

// fetchWikipedia fetches metadata from Wikipedia REST API
func (f *Fetcher) fetchWikipedia(ctx context.Context, u *url.URL) *Meta {
	// Extract article title from /wiki/Title
	if !strings.HasPrefix(u.Path, "/wiki/") {
		return nil
	}
	title := strings.TrimPrefix(u.Path, "/wiki/")

	// Extract language from subdomain
	lang := "en"
	if parts := strings.Split(u.Host, "."); len(parts) > 0 {
		lang = parts[0]
	}

	apiURL := fmt.Sprintf("https://%s.wikipedia.org/api/rest_v1/page/summary/%s", lang, title)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	var data struct {
		Title       string `json:"title"`
		Extract     string `json:"extract"`
		Description string `json:"description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	desc := data.Extract
	if len(desc) > 200 {
		desc = desc[:200] + "..."
	}

	return &Meta{
		FinalURL:    u.String(),
		Title:       data.Title,
		Description: desc,
		SiteName:    "Wikipedia",
		ContentType: "Article",
		Extra:       data.Description,
	}
}

// fetchGeneric fetches OG tags with a lightweight partial fetch
func (f *Fetcher) fetchGeneric(ctx context.Context, targetURL string) *Meta {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return &Meta{FinalURL: targetURL, Error: err}
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Range", "bytes=0-16384") // ~16KB should cover most <head> sections

	resp, err := f.client.Do(req)
	if err != nil {
		return &Meta{FinalURL: targetURL, Error: err}
	}
	defer resp.Body.Close()

	// Read up to 16KB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if err != nil {
		return &Meta{FinalURL: resp.Request.URL.String(), Error: err}
	}

	meta := ExtractMetaTags(body)
	meta.FinalURL = resp.Request.URL.String()

	// Try to determine content type
	if meta.ContentType == "" {
		meta.ContentType = guessContentType(resp.Request.URL)
	}

	return meta
}

// ExtractMetaTags parses HTML bytes to extract OG and meta tags
func ExtractMetaTags(body []byte) *Meta {
	html := string(body)
	meta := &Meta{}

	// Extract og:title or <title>
	if val := extractMetaContent(html, `property="og:title"`); val != "" {
		meta.Title = val
	} else if val := extractMetaContent(html, `name="twitter:title"`); val != "" {
		meta.Title = val
	} else {
		meta.Title = extractTagContent(html, "title")
	}

	// Extract og:description or meta description
	if val := extractMetaContent(html, `property="og:description"`); val != "" {
		meta.Description = val
	} else if val := extractMetaContent(html, `name="twitter:description"`); val != "" {
		meta.Description = val
	} else if val := extractMetaContent(html, `name="description"`); val != "" {
		meta.Description = val
	}

	// Truncate description
	if len(meta.Description) > 200 {
		meta.Description = meta.Description[:200] + "..."
	}

	// Extract og:site_name
	if val := extractMetaContent(html, `property="og:site_name"`); val != "" {
		meta.SiteName = val
	}

	// Extract og:type
	if val := extractMetaContent(html, `property="og:type"`); val != "" {
		meta.ContentType = capitalizeContentType(val)
	}

	return meta
}

// extractMetaContent extracts content attribute from a meta tag
func extractMetaContent(html, attr string) string {
	// Find the meta tag with the attribute
	idx := strings.Index(html, attr)
	if idx == -1 {
		return ""
	}

	// Find the tag boundaries
	tagStart := strings.LastIndex(html[:idx], "<meta")
	if tagStart == -1 {
		return ""
	}
	tagEnd := strings.Index(html[tagStart:], ">")
	if tagEnd == -1 {
		return ""
	}

	tag := html[tagStart : tagStart+tagEnd+1]

	// Extract content attribute
	contentIdx := strings.Index(tag, `content="`)
	if contentIdx == -1 {
		contentIdx = strings.Index(tag, `content='`)
		if contentIdx == -1 {
			return ""
		}
		contentIdx += 9
		endIdx := strings.Index(tag[contentIdx:], "'")
		if endIdx == -1 {
			return ""
		}
		return decodeHTMLEntities(tag[contentIdx : contentIdx+endIdx])
	}

	contentIdx += 9
	endIdx := strings.Index(tag[contentIdx:], `"`)
	if endIdx == -1 {
		return ""
	}

	return decodeHTMLEntities(tag[contentIdx : contentIdx+endIdx])
}

// extractTagContent extracts content from a simple HTML tag
func extractTagContent(html, tagName string) string {
	openTag := "<" + tagName
	closeTag := "</" + tagName + ">"

	startIdx := strings.Index(strings.ToLower(html), strings.ToLower(openTag))
	if startIdx == -1 {
		return ""
	}

	// Find end of open tag
	tagEnd := strings.Index(html[startIdx:], ">")
	if tagEnd == -1 {
		return ""
	}
	contentStart := startIdx + tagEnd + 1

	endIdx := strings.Index(strings.ToLower(html[contentStart:]), strings.ToLower(closeTag))
	if endIdx == -1 {
		return ""
	}

	return strings.TrimSpace(decodeHTMLEntities(html[contentStart : contentStart+endIdx]))
}

// decodeHTMLEntities decodes common HTML entities
func decodeHTMLEntities(s string) string {
	replacements := map[string]string{
		"&amp;":  "&",
		"&lt;":   "<",
		"&gt;":   ">",
		"&quot;": "\"",
		"&#39;":  "'",
		"&apos;": "'",
		"&#x27;": "'",
		"&nbsp;": " ",
	}
	for old, new := range replacements {
		s = strings.ReplaceAll(s, old, new)
	}
	return s
}

// capitalizeContentType converts og:type values to display strings
func capitalizeContentType(t string) string {
	switch strings.ToLower(t) {
	case "article":
		return "Article"
	case "website":
		return "Website"
	case "video", "video.other":
		return "Video"
	case "music.song", "music.album":
		return "Music"
	case "profile":
		return "Profile"
	case "book":
		return "Book"
	default:
		if t != "" {
			return strings.Title(strings.ReplaceAll(t, ".", " "))
		}
		return ""
	}
}

// guessContentType attempts to guess content type from URL
func guessContentType(u *url.URL) string {
	path := strings.ToLower(u.Path)

	switch {
	case strings.Contains(path, "/article") || strings.Contains(path, "/post") || strings.Contains(path, "/blog"):
		return "Article"
	case strings.Contains(path, "/video") || strings.Contains(path, "/watch"):
		return "Video"
	case strings.Contains(path, "/doc") || strings.Contains(path, "/documentation"):
		return "Documentation"
	case strings.HasSuffix(path, ".pdf"):
		return "PDF"
	case strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") || strings.HasSuffix(path, ".gif"):
		return "Image"
	default:
		return ""
	}
}

// formatCount formats large numbers with K/M suffixes
func formatCount(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// EstimateReadingTime estimates reading time from word count
func EstimateReadingTime(wordCount int) string {
	minutes := wordCount / 200 // ~200 WPM average
	if minutes < 1 {
		return "< 1 min read"
	}
	if minutes == 1 {
		return "1 min read"
	}
	return fmt.Sprintf("%d min read", minutes)
}
