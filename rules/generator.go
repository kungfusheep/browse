package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"browse/llm"
)

// Generator creates extraction rules using AI analysis.
type Generator struct {
	client *llm.Client
}

// NewGenerator creates a new rule generator.
func NewGenerator(client *llm.Client) *Generator {
	return &Generator{client: client}
}

// MaxValidationAttempts is the maximum number of conversation turns for validation.
const MaxValidationAttempts = 2

// MinItemsRequired is the minimum number of items for a valid extraction.
const MinItemsRequired = 3

// MinTitleLength is the minimum average title length (to catch empty/stub items).
const MinTitleLength = 5

// Generate analyzes HTML content and generates extraction rules.
// Uses a conversational feedback loop where the LLM can refine its approach.
func (g *Generator) Generate(ctx context.Context, domain, htmlContent string) (*Rule, error) {
	if !g.client.Available() {
		return nil, llm.ErrNoProvider
	}

	// Truncate HTML if too large (keep first ~50KB which should include structure)
	truncatedHTML := htmlContent
	if len(truncatedHTML) > 50000 {
		truncatedHTML = truncatedHTML[:50000] + "\n... [truncated]"
	}

	// Build conversation messages
	messages := []llm.Message{
		{Role: "user", Content: buildInitialPrompt(domain, truncatedHTML)},
	}

	var bestRule *Rule
	var bestItemCount int

	for attempt := 0; attempt <= MaxValidationAttempts; attempt++ {
		// Get response from LLM
		response, err := g.client.CompleteConversation(ctx, conversationSystemPrompt, messages)
		if err != nil {
			return nil, fmt.Errorf("LLM completion: %w", err)
		}

		// Add assistant response to conversation
		messages = append(messages, llm.Message{Role: "assistant", Content: response})

		// Check if LLM recommends skipping (default parser is better)
		if shouldSkip, reason := checkSkipResponse(response); shouldSkip {
			return nil, fmt.Errorf("LLM recommends default parser: %s", reason)
		}

		// Parse the JSON response
		rule, err := parseResponse(response)
		if err != nil {
			// Ask for correction in conversation
			messages = append(messages, llm.Message{
				Role:    "user",
				Content: fmt.Sprintf("That response wasn't valid JSON. Error: %v\n\nPlease respond with ONLY the JSON object, no explanation.", err),
			})
			continue
		}

		// Set metadata
		rule.Domain = domain
		rule.Version = 1
		rule.GeneratedAt = time.Now()
		if provider := g.client.Provider(); provider != nil {
			rule.GeneratedBy = provider.Name()
		}

		// Apply the rules and check results
		result := Apply(rule, htmlContent)

		// Guardrail: Check for minimum viable extraction
		if result == nil || len(result.Items) < MinItemsRequired {
			messages = append(messages, llm.Message{
				Role: "user",
				Content: fmt.Sprintf("Those selectors only extracted %d items (need at least %d). "+
					"The CSS selectors aren't matching the main content. "+
					"Look more carefully at the HTML structure - what elements contain the actual articles/items?",
					countItems(result), MinItemsRequired),
			})
			continue
		}

		// Guardrail: Check for empty/stub titles
		avgTitleLen := averageTitleLength(result)
		if avgTitleLen < MinTitleLength {
			messages = append(messages, llm.Message{
				Role: "user",
				Content: fmt.Sprintf("The extracted titles are too short (average %.1f chars). "+
					"This suggests the title selector isn't finding the actual headline text. "+
					"Look for the elements containing the full article/item titles.",
					avgTitleLen),
			})
			continue
		}

		// Guardrail: Check for link coverage on link-heavy sites
		linkRatio := linkCoverageRatio(result)
		if linkRatio < 0.5 && isLikelyLinkHeavySite(domain) {
			messages = append(messages, llm.Message{
				Role: "user",
				Content: fmt.Sprintf("Only %.0f%% of items have links, but this looks like a link-heavy site. "+
					"The title selector should target <a> elements or elements containing <a> tags. "+
					"Make sure the href is being captured from the link elements.",
					linkRatio*100),
			})
			continue
		}

		// Track best result so far
		if len(result.Items) > bestItemCount {
			bestRule = rule
			bestItemCount = len(result.Items)
		}

		// Build validation summary and ask LLM to confirm
		summary := buildExtractionSummary(result)
		messages = append(messages, llm.Message{
			Role: "user",
			Content: fmt.Sprintf("Here's what your selectors extracted:\n\n%s\n\n"+
				"Does this look correct? Are these the main content items with proper hierarchy?\n"+
				"If yes, respond with just: CONFIRMED\n"+
				"If no, explain what's wrong and provide corrected JSON selectors.",
				summary),
		})

		// Get confirmation
		confirmResponse, err := g.client.CompleteConversation(ctx, conversationSystemPrompt, messages)
		if err != nil {
			// On error, use what we have
			break
		}

		messages = append(messages, llm.Message{Role: "assistant", Content: confirmResponse})

		if strings.Contains(strings.ToUpper(confirmResponse), "CONFIRMED") {
			rule.Verified = true
			return rule, nil
		}

		// LLM wants to try again - parse new selectors if provided
		if newRule, err := parseResponse(confirmResponse); err == nil {
			newRule.Domain = domain
			newRule.Version = 1
			newRule.GeneratedAt = time.Now()
			if provider := g.client.Provider(); provider != nil {
				newRule.GeneratedBy = provider.Name()
			}
			bestRule = newRule
		}
	}

	// Return best rule we found
	if bestRule != nil {
		return bestRule, nil
	}

	return nil, fmt.Errorf("failed to generate valid rules after %d attempts", MaxValidationAttempts+1)
}

const conversationSystemPrompt = `You are helping "browse" - a terminal-based web browser that renders websites as clean, readable text documents.

THE PRODUCT:
- browse runs in a terminal (like vim or less)
- No images, no CSS styling, no JavaScript
- Content is rendered as structured text with headings, lists, and links
- Users navigate by typing link labels (like vimium)
- The goal: make ANY website readable and beautiful in a terminal

OUR AESTHETIC:
When rendered, a good extraction looks like this:

  HACKER NEWS
  ═══════════

  30 items

  • Show HN: I built a terminal browser — 142 points, 89 comments
  • Why Rust is taking over systems programming — 523 points, 312 comments
  • The future of web rendering — 87 points, 45 comments

Key aesthetic principles:
- Clean title at the top (from domain/site name)
- Bulleted list of items, each with title + metadata on one line
- Titles should be the ACTUAL HEADLINES users want to read
- Metadata (points, comments, dates, authors) adds context
- Links let users click through to read more

WHAT MAKES A GOOD EXTRACTION:
✓ Titles are real article headlines (10-200 chars typically)
✓ Each item links to actual content
✓ Metadata is meaningful (dates, authors, engagement)
✓ Correct hierarchy - articles, not paragraphs within articles
✓ No navigation, ads, or boilerplate

WHAT MAKES A BAD EXTRACTION:
✗ Titles are section numbers ("1.1", "1.1.1")
✗ Titles are single words ("Home", "About", "EN")
✗ No links or broken links
✗ Extracting every element instead of main content
✗ Mixed hierarchy (headers mixed with list items)

YOUR TASK:
1. Analyze the HTML to find the MAIN CONTENT items
2. Create CSS selectors that extract headlines with their links
3. Verify your extraction produces beautiful, readable output

Respond with JSON only when providing selectors. No markdown, no explanation.`

func buildInitialPrompt(domain, html string) string {
	return fmt.Sprintf(`Analyze this HTML from %s and create extraction rules.

First, identify what KIND of page this is:
- NEWS AGGREGATOR (HN, Reddit, Lobsters): List of story links with points/comments
- NEWS SITE (BBC, CNN): Headlines linking to articles, maybe with summaries
- WIKI/DOCS: Structured content with sections (may not need rules - default parser works)
- BLOG INDEX: List of post titles with dates
- SINGLE ARTICLE: One main piece of content (may not need rules)

For pages with a LIST of items, I need selectors to extract each item's:
- Title: The actual headline text users want to read
- Link: The URL to the full content
- Metadata: Points, comments, dates, authors (optional)

HTML to analyze:
%s

If this page would work fine with a standard HTML parser (like Wikipedia articles, documentation),
respond with: {"skip": true, "reason": "explanation"}

Otherwise, respond with ONLY this JSON (no markdown):
{
  "content": {
    "root": "selector for content container",
    "articles": "selector for each repeating item",
    "title": "selector for headline (must capture the <a> link)",
    "metadata": "selector for metadata"
  },
  "layout": {
    "type": "list|newspaper|forum",
    "metadata_position": "inline|below|hidden"
  },
  "quirks": {
    "table_layout": true if content uses HTML tables for layout
  }
}`, domain, html)
}

func buildExtractionSummary(result *ApplyResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Extracted %d items\n", len(result.Items)))

	withLinks := 0
	for _, item := range result.Items {
		if item.Href != "" {
			withLinks++
		}
	}
	sb.WriteString(fmt.Sprintf("Items with links: %d/%d\n\n", withLinks, len(result.Items)))

	sb.WriteString("First 5 items:\n")
	for i, item := range result.Items {
		if i >= 5 {
			break
		}
		title := item.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, title))
		if item.Href != "" {
			href := item.Href
			if len(href) > 50 {
				href = href[:47] + "..."
			}
			sb.WriteString(fmt.Sprintf("   → %s\n", href))
		}
		if item.Metadata != "" {
			meta := item.Metadata
			if len(meta) > 60 {
				meta = meta[:57] + "..."
			}
			sb.WriteString(fmt.Sprintf("   [%s]\n", meta))
		}
	}

	if len(result.Items) > 5 {
		sb.WriteString(fmt.Sprintf("\n... and %d more\n", len(result.Items)-5))
	}

	return sb.String()
}

// Helper functions for guardrails

func countItems(result *ApplyResult) int {
	if result == nil {
		return 0
	}
	return len(result.Items)
}

func averageTitleLength(result *ApplyResult) float64 {
	if result == nil || len(result.Items) == 0 {
		return 0
	}
	total := 0
	for _, item := range result.Items {
		total += len(item.Title)
	}
	return float64(total) / float64(len(result.Items))
}

func linkCoverageRatio(result *ApplyResult) float64 {
	if result == nil || len(result.Items) == 0 {
		return 0
	}
	withLinks := 0
	for _, item := range result.Items {
		if item.Href != "" {
			withLinks++
		}
	}
	return float64(withLinks) / float64(len(result.Items))
}

func isLikelyLinkHeavySite(domain string) bool {
	linkHeavyDomains := []string{
		"news.ycombinator.com",
		"lobste.rs",
		"reddit.com",
		"bbc.com",
		"cnn.com",
		"theguardian.com",
		"nytimes.com",
		"reuters.com",
		"apnews.com",
	}
	for _, d := range linkHeavyDomains {
		if strings.Contains(domain, d) {
			return true
		}
	}
	// Also check if domain looks like a news site
	newsIndicators := []string{"news", "times", "post", "daily", "herald", "tribune"}
	for _, ind := range newsIndicators {
		if strings.Contains(strings.ToLower(domain), ind) {
			return true
		}
	}
	return false
}

// checkSkipResponse checks if the LLM recommends using the default parser.
func checkSkipResponse(response string) (bool, string) {
	response = strings.TrimSpace(response)

	// Look for skip JSON
	if !strings.Contains(response, `"skip"`) {
		return false, ""
	}

	// Try to parse as skip response
	var skip struct {
		Skip   bool   `json:"skip"`
		Reason string `json:"reason"`
	}

	// Find JSON boundaries
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return false, ""
	}

	if err := json.Unmarshal([]byte(response[start:end+1]), &skip); err != nil {
		return false, ""
	}

	if skip.Skip {
		return true, skip.Reason
	}

	return false, ""
}

func parseResponse(response string) (*Rule, error) {
	// Clean up response - remove any markdown code blocks
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	// Find JSON object boundaries
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no valid JSON object found in response")
	}
	response = response[start : end+1]

	var rule Rule
	if err := json.Unmarshal([]byte(response), &rule); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	return &rule, nil
}
