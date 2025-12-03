# Dynamic Templates Architecture

## The Vision

The LLM doesn't just extract content - it designs the entire reading experience for each site and page type.

## Rule Structure (evolved)

```go
type Rule struct {
    Domain    string
    Version   int
    PageTypes map[string]PageType  // "home", "article", "search", "listing"
}

type PageType struct {
    // How to identify this page type
    Matcher   PageMatcher

    // What to extract
    Selectors map[string]string  // Named selectors: "headline" -> "h1.title"

    // How to render it
    Template  string  // Go template syntax
}

type PageMatcher struct {
    URLPattern  string   // Regex or glob: "/news/article-*"
    URLContains string   // Simple substring: "?q="
    HasElement  string   // CSS selector that must exist: "article.full-story"
}
```

## Example: BBC News

```json
{
  "domain": "bbc.com",
  "page_types": {
    "home": {
      "matcher": {"url_pattern": "^/(news)?$"},
      "selectors": {
        "featured": "[data-testid='hero'] h3",
        "featured_summary": "[data-testid='hero'] p",
        "top_stories": "[data-testid='topic-list'] li",
        "sections": "[data-testid='section']"
      },
      "template": "{{template \"news_feed\" .}}"
    },
    "article": {
      "matcher": {"has_element": "article[data-component='article']"},
      "selectors": {
        "headline": "h1",
        "byline": "[data-component='byline']",
        "timestamp": "time",
        "body": "article p",
        "related": "[data-component='related'] a"
      },
      "template": "{{template \"article_reader\" .}}"
    }
  }
}
```

## Built-in Template Functions

```go
// Text formatting
upper(s)           // UPPERCASE
title(s)           // Title Case
wrap(s, width)     // Word wrap to width
truncate(s, n)     // Truncate with ...
indent(s, n)       // Indent by n spaces

// Layout
hr(width, char)    // ═══════════════
box(content, w)    // Draw box around content
pad(n)             // n blank lines
cols(items, n)     // Arrange in n columns

// Content
limit(items, n)    // First n items
skip(items, n)     // Skip first n
grouped(items, k)  // Group by key

// Terminal
bold(s)            // **bold**
dim(s)             // (dimmed)
link(text, href)   // Make clickable [n]
```

## Template Examples

### News Feed (home page)
```
{{.Site | upper}}
{{hr 40 "═"}}

{{if .Featured}}
★ {{.Featured.Headline | bold}}
{{.Featured.Summary | wrap 60 | indent 2}}
{{pad 1}}
{{end}}

{{range .Sections}}
{{.Name | upper | dim}}
{{range .Items | limit 5}}
  • {{link .Title .Href}}{{if .Time}} — {{.Time | dim}}{{end}}
{{end}}
{{pad 1}}
{{end}}
```

### Article Reader
```
{{.Headline | upper | wrap 60}}
{{hr 40 "─"}}
{{if .Byline}}{{.Byline | dim}}{{end}}
{{if .Timestamp}}{{.Timestamp | dim}}{{end}}
{{pad 1}}

{{range .Body}}
{{. | wrap 70}}
{{pad 1}}
{{end}}

{{if .Related}}
{{hr 40 "─"}}
RELATED
{{range .Related | limit 5}}
  • {{link .Title .Href}}
{{end}}
{{end}}
```

### Search Results
```
SEARCH: "{{.Query}}"
{{.ResultCount}} results
{{hr 40 "─"}}

{{range .Results}}
{{.Index}}. {{link .Title .Href | bold}}
   {{.Snippet | wrap 65 | indent 3}}
   {{.URL | dim}}
{{pad 1}}
{{end}}

{{if .Pagination}}
{{.Pagination.Current}} of {{.Pagination.Total}} pages
{{end}}
```

## Implementation Steps

### Phase 1: Template Engine
- [ ] Create template renderer with custom functions
- [ ] Parse templates from rule JSON
- [ ] Map extracted data to template context

### Phase 2: Page Type Detection
- [ ] URL pattern matching
- [ ] Element presence detection
- [ ] LLM page type classification

### Phase 3: LLM Prompt Evolution
- [ ] Teach LLM about page types
- [ ] Generate selectors as named map
- [ ] Generate appropriate template per page type

### Phase 4: Multi-Page Rules
- [ ] Update Rule struct for page types
- [ ] Update cache to store/retrieve by page type
- [ ] Incremental learning (add page types as visited)

## Flow

```
User visits bbc.com/news
    ↓
Check cache for bbc.com
    ↓
Determine page type (home vs article)
    ↓
If no template for this page type:
    → LLM generates selectors + template
    → Cache for this page type
    ↓
Extract data using selectors
    ↓
Render using template
    ↓
Display beautiful terminal output
```

## Key Insight

The LLM becomes a **designer**, not just an extractor. It understands:
- What content matters on this page
- How that content should be visually organized
- What the reading experience should feel like

Each site gets a bespoke reading experience that respects both:
- The site's content hierarchy
- Our terminal aesthetic
