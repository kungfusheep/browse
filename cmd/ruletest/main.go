// Command ruletest tests rule generation and extraction against real sites.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"browse/llm"
	"browse/rules"
)

var (
	site    = flag.String("site", "", "Test specific site (hn, bbc, lobsters, npr, wikipedia)")
	verbose = flag.Bool("v", false, "Verbose output")
	useV2   = flag.Bool("v2", false, "Use V2 template-based generator")
)

type testSite struct {
	name       string
	domain     string
	url        string
	minItems   int
	maxItems   int
	hasLinks   bool
	minTitleLen int
	shouldSkip bool
}

var sites = map[string]testSite{
	"hn": {
		name:       "Hacker News",
		domain:     "news.ycombinator.com",
		url:        "https://news.ycombinator.com",
		minItems:   20,
		maxItems:   40,
		hasLinks:   true,
		minTitleLen: 20,
	},
	"bbc": {
		name:       "BBC News",
		domain:     "www.bbc.com",
		url:        "https://www.bbc.com/news",
		minItems:   10,
		maxItems:   100,
		hasLinks:   true,
		minTitleLen: 20,
	},
	"lobsters": {
		name:       "Lobsters",
		domain:     "lobste.rs",
		url:        "https://lobste.rs",
		minItems:   15,
		maxItems:   30,
		hasLinks:   true,
		minTitleLen: 15,
	},
	"npr": {
		name:       "NPR Text",
		domain:     "text.npr.org",
		url:        "https://text.npr.org",
		minItems:   10,
		maxItems:   50,
		hasLinks:   true,
		minTitleLen: 15,
		shouldSkip: true, // text.npr.org is already clean HTML, skip is fine
	},
	"wikipedia": {
		name:       "Wikipedia",
		domain:     "en.wikipedia.org",
		url:        "https://en.wikipedia.org/wiki/Go_(programming_language)",
		shouldSkip: true,
	},
	"guardian": {
		name:       "The Guardian",
		domain:     "www.theguardian.com",
		url:        "https://www.theguardian.com/uk",
		minItems:   10,
		maxItems:   100,
		hasLinks:   true,
		minTitleLen: 20,
	},
}

func main() {
	flag.Parse()

	// Set up LLM client
	client := llm.NewClient(
		llm.NewClaudeCode(),
		llm.NewClaudeAPI(""),
	)

	if !client.Available() {
		fmt.Println("No LLM provider available!")
		os.Exit(1)
	}

	fmt.Printf("Using LLM: %s\n\n", client.Provider().Name())

	if *site != "" {
		// Test specific site
		s, ok := sites[*site]
		if !ok {
			fmt.Printf("Unknown site: %s\n", *site)
			fmt.Println("Available: hn, bbc, lobsters, npr, wikipedia, guardian")
			os.Exit(1)
		}
		runSiteTest(client, s)
	} else {
		// Test all sites
		for key, s := range sites {
			fmt.Printf("=== Testing %s (%s) ===\n", s.name, key)
			runSiteTest(client, s)
			fmt.Println()
		}
	}
}

func runSiteTest(client *llm.Client, s testSite) {
	// Fetch HTML
	fmt.Printf("Fetching %s...\n", s.url)
	html, err := fetchHTML(s.url)
	if err != nil {
		fmt.Printf("  ✗ Fetch error: %v\n", err)
		return
	}
	fmt.Printf("  Got %d bytes\n", len(html))

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	if *useV2 {
		runV2Test(ctx, client, s, html)
	} else {
		runV1Test(ctx, client, s, html)
	}
}

func runV1Test(ctx context.Context, client *llm.Client, s testSite, html string) {
	// Generate rules
	fmt.Println("Generating rules (V1)...")
	generator := rules.NewGenerator(client)

	rule, err := generator.Generate(ctx, s.domain, html)
	if err != nil {
		if s.shouldSkip && strings.Contains(err.Error(), "default parser") {
			fmt.Printf("  ✓ Correctly recommended default parser\n")
			return
		}
		fmt.Printf("  ✗ Generation error: %v\n", err)
		return
	}

	if s.shouldSkip {
		fmt.Printf("  ✗ Should have recommended default parser, but generated rules\n")
		return
	}

	// Show rule
	if *verbose {
		fmt.Printf("  Root: %s\n", rule.Content.Root)
		fmt.Printf("  Articles: %s\n", rule.Content.Articles)
		fmt.Printf("  Title: %s\n", rule.Content.Title)
		fmt.Printf("  Verified: %v\n", rule.Verified)
	}

	// Apply rules
	result := rules.Apply(rule, html)
	if result == nil {
		fmt.Printf("  ✗ Apply returned nil\n")
		return
	}

	// Evaluate V1 results
	fmt.Printf("  Extracted %d items\n", len(result.Items))

	// Check item count
	if len(result.Items) < s.minItems {
		fmt.Printf("  ✗ Too few items (expected >= %d)\n", s.minItems)
	} else if s.maxItems > 0 && len(result.Items) > s.maxItems {
		fmt.Printf("  ⚠ Many items (expected <= %d)\n", s.maxItems)
	} else {
		fmt.Printf("  ✓ Item count OK\n")
	}

	// Check links
	withLinks := 0
	totalTitleLen := 0
	for _, item := range result.Items {
		if item.Href != "" {
			withLinks++
		}
		totalTitleLen += len(item.Title)
	}

	linkRatio := float64(withLinks) / float64(len(result.Items))
	if s.hasLinks && linkRatio < 0.5 {
		fmt.Printf("  ✗ Low link coverage: %.0f%% (expected >= 50%%)\n", linkRatio*100)
	} else {
		fmt.Printf("  ✓ Links: %d/%d (%.0f%%)\n", withLinks, len(result.Items), linkRatio*100)
	}

	// Check title length
	avgTitleLen := float64(totalTitleLen) / float64(len(result.Items))
	if avgTitleLen < float64(s.minTitleLen) {
		fmt.Printf("  ✗ Short titles: avg %.1f chars (expected >= %d)\n", avgTitleLen, s.minTitleLen)
	} else {
		fmt.Printf("  ✓ Avg title length: %.1f chars\n", avgTitleLen)
	}

	// Show sample items
	fmt.Println("  Sample items:")
	for i, item := range result.Items {
		if i >= 5 {
			break
		}
		title := item.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		linkStatus := "✓"
		if item.Href == "" {
			linkStatus = "✗"
		}
		fmt.Printf("    %d. [%s] %s\n", i+1, linkStatus, title)
	}
}

func runV2Test(ctx context.Context, client *llm.Client, s testSite, html string) {
	// Generate rules using V2 template system
	fmt.Println("Generating rules (V2 template)...")
	generator := rules.NewGeneratorV2(client)

	rule, err := generator.GeneratePageType(ctx, s.domain, s.url, html)
	if err != nil {
		if s.shouldSkip && strings.Contains(err.Error(), "default parser") {
			fmt.Printf("  ✓ Correctly recommended default parser\n")
			return
		}
		fmt.Printf("  ✗ Generation error: %v\n", err)
		return
	}

	if s.shouldSkip {
		fmt.Printf("  ✗ Should have recommended default parser, but generated rules\n")
		return
	}

	// Show rule
	fmt.Printf("  Version: %d, Verified: %v\n", rule.Version, rule.Verified)
	for name, pt := range rule.PageTypes {
		fmt.Printf("  Page type: %s\n", name)
		if *verbose {
			fmt.Printf("    Selectors:\n")
			for k, v := range pt.Selectors {
				fmt.Printf("      %s: %s\n", k, v)
			}
			fmt.Printf("    Template: %.100s...\n", pt.Template)
		}
	}

	// Apply V2 rules
	result, err := rules.ApplyV2(rule, s.url, html)
	if err != nil {
		fmt.Printf("  ✗ Apply error: %v\n", err)
		return
	}
	if result == nil {
		fmt.Printf("  ✗ Apply returned nil\n")
		return
	}

	// Evaluate V2 results
	fmt.Printf("  Extracted %d links\n", len(result.Links))

	// Check link count
	if len(result.Links) < s.minItems {
		fmt.Printf("  ✗ Too few links (expected >= %d)\n", s.minItems)
	} else if s.maxItems > 0 && len(result.Links) > s.maxItems {
		fmt.Printf("  ⚠ Many links (expected <= %d)\n", s.maxItems)
	} else {
		fmt.Printf("  ✓ Link count OK\n")
	}

	// Check title lengths
	totalTitleLen := 0
	for _, link := range result.Links {
		totalTitleLen += len(link.Text)
	}

	if len(result.Links) > 0 {
		avgTitleLen := float64(totalTitleLen) / float64(len(result.Links))
		if avgTitleLen < float64(s.minTitleLen) {
			fmt.Printf("  ✗ Short titles: avg %.1f chars (expected >= %d)\n", avgTitleLen, s.minTitleLen)
		} else {
			fmt.Printf("  ✓ Avg title length: %.1f chars\n", avgTitleLen)
		}
	}

	// Show sample links
	fmt.Println("  Sample items:")
	for i, link := range result.Links {
		if i >= 5 {
			break
		}
		title := link.Text
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		fmt.Printf("    %d. %s\n       → %s\n", i+1, title, link.Href)
	}

	// Show rendered content preview
	if *verbose && len(result.Content) > 0 {
		fmt.Println("\n  Rendered content preview:")
		preview := result.Content
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		for _, line := range strings.Split(preview, "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
}

func fetchHTML(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}
