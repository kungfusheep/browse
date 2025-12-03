package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"browse/llm"

	"github.com/PuerkitoBio/goquery"
)

// GeneratorV2 creates template-based extraction rules using AI analysis.
type GeneratorV2 struct {
	client *llm.Client
}

// NewGeneratorV2 creates a new v2 rule generator.
func NewGeneratorV2(client *llm.Client) *GeneratorV2 {
	return &GeneratorV2{client: client}
}

// V2 quality thresholds
const (
	V2MinItems         = 5
	V2MinTitleLen      = 10
	V2MinLinkRatio     = 0.5
	V2MaxRetries       = 2
)

// GeneratePageType analyzes HTML and generates a PageType with selectors and template.
// Includes a feedback loop to refine results if initial extraction is poor.
func (g *GeneratorV2) GeneratePageType(ctx context.Context, domain, url, htmlContent string) (*Rule, error) {
	if !g.client.Available() {
		return nil, llm.ErrNoProvider
	}

	// Parse HTML once for validation
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("parsing HTML: %w", err)
	}

	// Discover available selectors to include in feedback if needed
	availableSelectors := discoverAvailableSelectors(doc)

	// Truncate HTML if too large
	truncatedHTML := htmlContent
	if len(truncatedHTML) > 50000 {
		truncatedHTML = truncatedHTML[:50000] + "\n... [truncated]"
	}

	var bestRule *Rule
	var bestScore int
	var lastFeedback string

	for attempt := 0; attempt <= V2MaxRetries; attempt++ {
		// Build prompt with available selectors upfront + any feedback
		prompt := buildV2Prompt(domain, url, availableSelectors, truncatedHTML)
		if lastFeedback != "" {
			prompt = prompt + "\n\nPREVIOUS ATTEMPT FEEDBACK:\n" + lastFeedback
		}

		// Get response (single completion to avoid session issues)
		response, err := g.client.CompleteWithSystem(ctx, v2SystemPrompt, prompt)
		if err != nil {
			return nil, fmt.Errorf("LLM completion: %w", err)
		}


		// Check for skip recommendation
		if shouldSkip, reason := checkSkipResponse(response); shouldSkip {
			return nil, fmt.Errorf("LLM recommends default parser: %s", reason)
		}

		// Parse the response
		rule, _, err := parseV2Response(response)
		if err != nil {
			lastFeedback = fmt.Sprintf("Invalid JSON: %v. Please respond with valid JSON only.", err)
			continue
		}

		// Set metadata
		rule.Domain = domain
		rule.Version = 2
		rule.GeneratedAt = time.Now()
		if provider := g.client.Provider(); provider != nil {
			rule.GeneratedBy = provider.Name()
		}

		pageType := rule.PageTypes[findFirstPageType(rule)]

		// CRITICAL: Check if selectors actually match elements BEFORE applying template
		_, matchIssues := validateSelectorMatches(doc, pageType.Selectors)
		if len(matchIssues) > 0 {
			// Selectors don't match - provide helpful feedback with available selectors
			lastFeedback = "SELECTOR MATCH ERRORS:\n" + strings.Join(matchIssues, "\n") +
				"\n\nYour selectors don't match any elements in the HTML!" +
				availableSelectors +
				"\n\nPlease use selectors from the list above. Respond with corrected JSON."
			continue
		}

		// Apply and validate
		result, err := ApplyV2(rule, url, htmlContent)
		if err != nil {
			lastFeedback = fmt.Sprintf("Template error: %v. Fix the template syntax.", err)
			continue
		}
		if result == nil {
			lastFeedback = "Template application returned no results. Check template syntax."
			continue
		}

		// Check selector quality (robustness)
		selectorIssues := validateSelectors(pageType.Selectors)

		// Quality check
		score, feedback := evaluateV2Result(result)

		// Add selector robustness issues to feedback
		if len(selectorIssues) > 0 {
			score -= len(selectorIssues) * 5
			if feedback == "" {
				feedback = "SELECTOR ISSUES:\n" + strings.Join(selectorIssues, "\n") + "\n\nPlease fix and respond with corrected JSON."
			} else {
				feedback = feedback + "\n\nSELECTOR ISSUES:\n" + strings.Join(selectorIssues, "\n")
			}
		}

		// If extraction failed, include available selectors in feedback
		if score < 40 && availableSelectors != "" {
			feedback = feedback + availableSelectors
		}

		if score > bestScore {
			bestScore = score
			bestRule = rule
		}

		// If quality is good enough, we're done
		if score >= 80 {
			rule.Verified = true
			return rule, nil
		}

		// Store feedback for next attempt
		lastFeedback = feedback
	}

	// Return best attempt
	if bestRule != nil {
		return bestRule, nil
	}

	return nil, fmt.Errorf("failed to generate valid rules after %d attempts", V2MaxRetries+1)
}

// evaluateV2Result scores extraction quality and returns feedback
func evaluateV2Result(result *ApplyV2Result) (score int, feedback string) {
	if result == nil {
		return 0, "Extraction returned no results. Check your selectors - they may not match any elements in the HTML."
	}

	itemCount := len(result.Links)
	if itemCount == 0 {
		// Try to estimate from content
		lines := strings.Split(result.Content, "\n")
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "•") ||
				strings.HasPrefix(strings.TrimSpace(line), "-") {
				itemCount++
			}
		}
	}

	var issues []string

	// Check item count
	if itemCount < V2MinItems {
		issues = append(issues, fmt.Sprintf("Only %d items extracted (need at least %d). Your selectors aren't finding the main content. Look for article cards, story containers.", itemCount, V2MinItems))
		score += itemCount * 10
	} else {
		score += 40
	}

	// Check link count
	if len(result.Links) == 0 {
		issues = append(issues, "No links extracted. Each content item should have a clickable link. Make sure Items.href selector targets <a> elements or uses @href.")
	} else if len(result.Links) < V2MinItems {
		issues = append(issues, fmt.Sprintf("Only %d links found. Most items should have links to full content.", len(result.Links)))
		score += 20
	} else {
		score += 30
	}

	// Check for navigation-like content (bad) - this is a CRITICAL failure
	navKeywords := []string{"home", "contact", "about", "login", "sign up", "menu", "section", "news", "sport", "business", "weather", "culture"}
	navCount := 0
	for _, link := range result.Links {
		textLower := strings.ToLower(link.Text)
		for _, kw := range navKeywords {
			if textLower == kw || strings.HasPrefix(textLower, kw+" ") {
				navCount++
				break
			}
		}
	}
	if navCount > 3 {
		// HEAVILY penalize navigation extraction - this is the wrong content
		score -= 50
		issues = append(issues, fmt.Sprintf("CRITICAL: Found %d navigation items (Home, News, Sport, etc). You're extracting the nav menu! Use a more specific selector like [data-testid='card-headline'] that targets actual news stories.", navCount))
	} else {
		score += 20
	}

	// Check content length
	if len(result.Content) < 100 {
		issues = append(issues, "Very little content rendered. Template may have errors or selectors aren't matching.")
	} else {
		score += 10
	}

	if len(issues) == 0 {
		return score, ""
	}

	return score, "ISSUES:\n" + strings.Join(issues, "\n") + "\n\nPlease fix the selectors and try again. Respond with corrected JSON."
}

// findFirstPageType returns the name of the first page type in a rule
func findFirstPageType(rule *Rule) string {
	for name := range rule.PageTypes {
		return name
	}
	return ""
}

// selectorMatchInfo tracks how many elements each selector finds
type selectorMatchInfo struct {
	name     string
	selector string
	count    int
}

// validateSelectorMatches checks if selectors actually match elements in the HTML
func validateSelectorMatches(doc *goquery.Document, selectors map[string]string) (matches []selectorMatchInfo, issues []string) {
	for name, selector := range selectors {
		// Clean selector for query
		sel := strings.Split(selector, "@")[0]
		sel = strings.TrimSuffix(sel, "[]")
		sel = strings.TrimSpace(sel)

		if sel == "" {
			continue
		}

		count := doc.Find(sel).Length()
		matches = append(matches, selectorMatchInfo{
			name:     name,
			selector: selector,
			count:    count,
		})

		if count == 0 {
			issues = append(issues, fmt.Sprintf("Selector '%s' (%s) matches 0 elements!", name, selector))
		}
	}
	return matches, issues
}

// discoverAvailableSelectors finds useful selector patterns in the HTML
func discoverAvailableSelectors(doc *goquery.Document) string {
	var hints []string

	// Find section-like containers FIRST (for hierarchy)
	sectionCount := doc.Find("section").Length()
	if sectionCount > 1 {
		hints = append(hints, fmt.Sprintf("SECTIONS DETECTED: <section> (%d elements) - USE HIERARCHICAL EXTRACTION!", sectionCount))
	}

	// Find data-testid values (very common in modern sites)
	testIds := make(map[string]int)
	doc.Find("[data-testid]").Each(func(i int, s *goquery.Selection) {
		if id, exists := s.Attr("data-testid"); exists {
			testIds[id]++
		}
	})

	// Sort by count and show top ones
	type idCount struct {
		id    string
		count int
	}
	var sorted []idCount
	for id, count := range testIds {
		if count >= 3 { // Only show patterns that repeat
			sorted = append(sorted, idCount{id, count})
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	if len(sorted) > 0 {
		hints = append(hints, "Available data-testid values (with counts):")
		for i, item := range sorted {
			if i >= 10 {
				break
			}
			hints = append(hints, fmt.Sprintf("  [data-testid=\"%s\"] (%d elements)", item.id, item.count))
		}
	}

	// Find semantic elements with counts
	semanticTags := []string{"article", "section", "main", "aside", "nav", "header", "footer"}
	var semanticHints []string
	for _, tag := range semanticTags {
		count := doc.Find(tag).Length()
		if count > 0 {
			semanticHints = append(semanticHints, fmt.Sprintf("  <%s> (%d elements)", tag, count))
		}
	}
	if len(semanticHints) > 0 {
		hints = append(hints, "\nSemantic elements:")
		hints = append(hints, semanticHints...)
	}

	// Find heading elements (often contain titles)
	for _, h := range []string{"h1", "h2", "h3"} {
		count := doc.Find(h).Length()
		if count > 0 {
			hints = append(hints, fmt.Sprintf("\n<%s> elements: %d", h, count))
		}
	}

	// Look for common content container classes
	contentClasses := []string{"card", "article", "story", "post", "item", "entry", "headline", "title"}
	var classHints []string
	for _, cls := range contentClasses {
		selector := fmt.Sprintf("[class*='%s']", cls)
		count := doc.Find(selector).Length()
		if count >= 3 {
			classHints = append(classHints, fmt.Sprintf("  [class*='%s'] (%d elements)", cls, count))
		}
	}
	if len(classHints) > 0 {
		hints = append(hints, "\nClasses containing common patterns:")
		hints = append(hints, classHints...)
	}

	// Find all classes with high frequency (for sites like HN with simple class names)
	allClasses := make(map[string]int)
	doc.Find("[class]").Each(func(i int, s *goquery.Selection) {
		if cls, exists := s.Attr("class"); exists {
			// Split multi-class attributes
			for _, c := range strings.Fields(cls) {
				allClasses[c]++
			}
		}
	})

	// Find frequently-used classes (likely content containers)
	var frequentClasses []idCount
	for cls, count := range allClasses {
		// Filter out likely layout/utility classes
		if count >= 10 && count <= 100 {
			// Skip generic words
			lower := strings.ToLower(cls)
			if lower != "container" && lower != "wrapper" && lower != "hidden" && lower != "active" {
				frequentClasses = append(frequentClasses, idCount{cls, count})
			}
		}
	}
	sort.Slice(frequentClasses, func(i, j int) bool {
		return frequentClasses[i].count > frequentClasses[j].count
	})

	if len(frequentClasses) > 0 {
		hints = append(hints, "\nFrequent CSS classes (potential content containers):")
		for i, item := range frequentClasses {
			if i >= 8 {
				break
			}
			hints = append(hints, fmt.Sprintf("  .%s (%d elements)", item.id, item.count))
		}
	}

	if len(hints) == 0 {
		return ""
	}

	return "\nAVAILABLE SELECTORS IN THIS HTML:\n" + strings.Join(hints, "\n")
}

// validateSelectors checks if selectors are robust and portable
func validateSelectors(selectors map[string]string) (issues []string) {
	for name, selector := range selectors {
		// Skip attribute extraction modifiers
		sel := strings.Split(selector, "@")[0]
		sel = strings.TrimSuffix(sel, "[]")
		sel = strings.TrimSpace(sel)

		// Check for brittle patterns
		if strings.Contains(sel, ":nth-child") || strings.Contains(sel, ":nth-of-type") {
			issues = append(issues, fmt.Sprintf("%s: Avoid nth-child/nth-of-type - position may change. Use class/attribute selectors.", name))
		}

		// Check for auto-generated looking class names (long random strings)
		parts := strings.Fields(sel)
		for _, part := range parts {
			// Look for classes that look auto-generated (long alphanumeric)
			if strings.HasPrefix(part, ".") {
				className := strings.TrimPrefix(part, ".")
				className = strings.Split(className, "[")[0] // Remove attribute parts
				if len(className) > 20 && !strings.Contains(className, "-") {
					issues = append(issues, fmt.Sprintf("%s: Class '%s' looks auto-generated and may change between page loads. Use data-* attributes or semantic selectors.", name, className))
				}
			}
		}

		// Check for overly specific paths (more than 4 levels deep)
		if strings.Count(sel, " > ") > 3 || strings.Count(sel, " ") > 5 {
			issues = append(issues, fmt.Sprintf("%s: Selector is very specific and may break if page structure changes. Simplify to target key elements directly.", name))
		}

		// Check for ID selectors that look dynamic
		if strings.Contains(sel, "#") {
			// IDs with numbers often change
			idPart := strings.Split(sel, "#")[1]
			idPart = strings.Split(idPart, " ")[0]
			idPart = strings.Split(idPart, ".")[0]
			if strings.ContainsAny(idPart, "0123456789") && len(idPart) > 10 {
				issues = append(issues, fmt.Sprintf("%s: ID '%s' contains numbers and may be dynamically generated. Prefer class or data-* selectors.", name, idPart))
			}
		}
	}

	return issues
}

const v2SystemPrompt = `You extract content from web pages for a terminal browser.

WHEN TO SKIP (use default parser instead):
Respond with {"skip": true, "reason": "..."} for:
- Article/content pages (Wikipedia articles, blog posts, news articles with body text)
- Already clean/simple HTML (text-only sites like text.npr.org)
- Pages without clear repeating item structure
- Documentation pages, wiki pages, long-form content

Only create extraction rules for INDEX/LIST pages like:
- News homepages with multiple story cards
- Forum listing pages (HN, Reddit, Lobsters)
- Search results pages
- Category/tag listing pages

EXACT JSON FORMAT FOR LIST PAGES:
{
  "page_type": "home",
  "selectors": {
    "Items": "[data-testid='card-text-wrapper'][]",
    "Items.text": "[data-testid='card-headline']",
    "Items.time": "[data-testid='card-metadata']"
  },
  "template": "SITE\n{{hr 50 \"═\"}}\n\n{{range .Items | limit 20}}  → {{.text}}{{if .time}} {{.time | dim}}{{end}}\n{{end}}"
}

HIERARCHICAL (if clear sections with category names exist):
{
  "page_type": "home",
  "selectors": {
    "Sections": "section[]",
    "Sections.name": "[data-testid*='section-title']",
    "Sections.items": "[data-testid='card-text-wrapper'][]",
    "Sections.items.text": "[data-testid='card-headline']"
  },
  "template": "{{range .Sections}}{{if .name}}{{.name | upper}}\n{{hr 40 \"─\"}}{{end}}\n{{range .items | limit 5}}  → {{.text}}\n{{end}}\n{{end}}"
}

IMPORTANT:
- Sections.name must be CATEGORY names (Europe, World) not story headlines
- If unsure about section names, use flat Items structure
- All template variables MUST exist in selectors (no {{.missing}} allowed)

RESPOND WITH VALID JSON.`

func buildV2Prompt(domain, url, availableSelectors, html string) string {
	return fmt.Sprintf(`Domain: %s
URL: %s

FIRST: Determine if this is an ARTICLE page or a LIST page.
- If this is an ARTICLE (Wikipedia, blog post, news article with body text): respond {"skip": true, "reason": "article page"}
- If this is a LIST page (news homepage, forum, search results): continue with extraction rules

%s

For LIST pages, look for repeating item containers:
- News cards, story items, forum posts
- Use flat Items structure unless clear section categories exist

If clear sections with category titles exist (Europe, World, Business):
  Sections = "section[]"
  Sections.name = "[data-testid*='section-title']"
  Sections.items = "[data-testid='card-text-wrapper'][]"
  Sections.items.text = "[data-testid='card-headline']"

Otherwise use flat structure:
  Items = "[data-testid='card-text-wrapper'][]"
  Items.text = "[data-testid='card-headline']"

AVOID: navigation, menus, headers, footers, table of contents

HTML:
%s

JSON only.`, domain, url, availableSelectors, html)
}

func detectPageTypeFromURL(url string) string {
	url = strings.ToLower(url)

	// Article patterns
	if strings.Contains(url, "/article") ||
		strings.Contains(url, "/story/") ||
		strings.Contains(url, "/news/") && strings.Count(url, "/") > 4 ||
		strings.Contains(url, "/post/") ||
		strings.Contains(url, "/blog/") && strings.Count(url, "/") > 4 {
		return "article"
	}

	// Search patterns
	if strings.Contains(url, "search") ||
		strings.Contains(url, "?q=") ||
		strings.Contains(url, "?query=") {
		return "search"
	}

	// Listing patterns
	if strings.Contains(url, "/category/") ||
		strings.Contains(url, "/tag/") ||
		strings.Contains(url, "/topics/") {
		return "listing"
	}

	// Default to home for root-like paths
	return "home"
}

func parseV2Response(response string) (*Rule, string, error) {
	// Clean up response
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Find JSON boundaries
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, "", fmt.Errorf("no valid JSON found")
	}
	response = response[start : end+1]

	// Parse into intermediate struct
	var parsed struct {
		PageType  string            `json:"page_type"`
		Matcher   PageMatcher       `json:"matcher"`
		Selectors map[string]string `json:"selectors"`
		Template  string            `json:"template"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		return nil, "", fmt.Errorf("JSON parse error: %w", err)
	}

	// Build Rule with PageType
	pageTypeName := parsed.PageType
	if pageTypeName == "" {
		pageTypeName = PageTypeDefault
	}

	// If no matcher conditions set, make it the default
	matcher := parsed.Matcher
	if matcher.URLPattern == "" && matcher.URLContains == "" && matcher.HasElement == "" && matcher.NotElement == "" {
		matcher.IsDefault = true
	}

	rule := &Rule{
		PageTypes: map[string]*PageType{
			pageTypeName: {
				Matcher:   matcher,
				Selectors: parsed.Selectors,
				Template:  parsed.Template,
			},
		},
	}

	return rule, pageTypeName, nil
}
