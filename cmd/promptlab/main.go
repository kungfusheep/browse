// Command promptlab uses Claude to iteratively refine extraction prompts.
// It's a meta-tool: Claude helps Claude get better at extraction!
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
	site      = flag.String("site", "hn", "Test site (hn, bbc, lobsters, npr)")
	maxRounds = flag.Int("rounds", 5, "Maximum optimization rounds")
)

type testCase struct {
	name        string
	domain      string
	url         string
	minItems    int
	wantLinks   bool
	wantTitleLen int
}

var testCases = map[string]testCase{
	"hn": {
		name:        "Hacker News",
		domain:      "news.ycombinator.com",
		url:         "https://news.ycombinator.com",
		minItems:    20,
		wantLinks:   true,
		wantTitleLen: 20,
	},
	"bbc": {
		name:        "BBC News",
		domain:      "www.bbc.com",
		url:         "https://www.bbc.com/news",
		minItems:    10,
		wantLinks:   true,
		wantTitleLen: 20,
	},
	"lobsters": {
		name:        "Lobsters",
		domain:      "lobste.rs",
		url:         "https://lobste.rs",
		minItems:    15,
		wantLinks:   true,
		wantTitleLen: 15,
	},
	"npr": {
		name:        "NPR Text",
		domain:      "text.npr.org",
		url:         "https://text.npr.org",
		minItems:    10,
		wantLinks:   true,
		wantTitleLen: 15,
	},
}

func main() {
	flag.Parse()

	tc, ok := testCases[*site]
	if !ok {
		fmt.Printf("Unknown site: %s\n", *site)
		fmt.Println("Available: hn, bbc, lobsters, npr")
		os.Exit(1)
	}

	// Set up LLM client
	client := llm.NewClient(
		llm.NewClaudeCode(),
		llm.NewClaudeAPI(""),
	)

	if !client.Available() {
		fmt.Println("No LLM provider available!")
		os.Exit(1)
	}

	fmt.Printf("PROMPT LAB: Optimizing extraction for %s\n", tc.name)
	fmt.Printf("Using LLM: %s\n", client.Provider().Name())
	fmt.Println(strings.Repeat("=", 60))

	// Fetch HTML
	fmt.Printf("\nFetching %s...\n", tc.url)
	html, err := fetchHTML(tc.url)
	if err != nil {
		fmt.Printf("Fetch error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Got %d bytes\n\n", len(html))

	// Run optimization loop
	runOptimizationLoop(client, tc, html)
}

func runOptimizationLoop(client *llm.Client, tc testCase, html string) {
	ctx := context.Background()

	for round := 1; round <= *maxRounds; round++ {
		fmt.Printf("\n%s ROUND %d %s\n", strings.Repeat("─", 20), round, strings.Repeat("─", 20))

		// Generate rules with current prompts
		fmt.Println("Generating rules...")
		generator := rules.NewGenerator(client)
		genCtx, cancel := context.WithTimeout(ctx, 120*time.Second)

		rule, err := generator.Generate(genCtx, tc.domain, html)
		cancel()

		if err != nil {
			fmt.Printf("Generation error: %v\n", err)
			// Ask Claude how to fix the prompt for this failure
			suggestion := askForPromptImprovement(ctx, client, tc, nil, err.Error())
			fmt.Printf("\nClaude's suggestion:\n%s\n", suggestion)
			continue
		}

		// Apply rules and evaluate
		result := rules.Apply(rule, html)
		score := evaluateResult(result, tc)

		fmt.Printf("\nResults:\n")
		fmt.Printf("  Items extracted: %d (want >= %d)\n", len(result.Items), tc.minItems)
		fmt.Printf("  Link coverage: %.0f%% (want >= 50%%)\n", linkRatio(result)*100)
		fmt.Printf("  Avg title len: %.1f (want >= %d)\n", avgTitleLen(result), tc.wantTitleLen)
		fmt.Printf("  Quality score: %.0f/100\n", score)

		// Show sample items
		fmt.Println("\nSample items:")
		for i, item := range result.Items {
			if i >= 5 {
				break
			}
			title := item.Title
			if len(title) > 55 {
				title = title[:52] + "..."
			}
			link := "no link"
			if item.Href != "" {
				link = "linked"
			}
			fmt.Printf("  %d. [%s] %s\n", i+1, link, title)
		}

		// Check if we're happy
		if score >= 90 {
			fmt.Printf("\n SUCCESS! Extraction quality is excellent.\n")
			fmt.Println("The current prompts are working well for this site.")
			return
		}

		// Ask Claude for prompt improvements
		fmt.Println("\nAsking Claude to analyze and suggest improvements...")
		suggestion := askForPromptImprovement(ctx, client, tc, result, "")
		fmt.Printf("\nClaude's analysis:\n%s\n", suggestion)

		// Ask user whether to continue
		fmt.Print("\nContinue to next round? [Y/n]: ")
		var input string
		fmt.Scanln(&input)
		if strings.ToLower(strings.TrimSpace(input)) == "n" {
			break
		}
	}
}

func askForPromptImprovement(ctx context.Context, client *llm.Client, tc testCase, result *rules.ApplyResult, errorMsg string) string {
	var prompt strings.Builder

	prompt.WriteString(fmt.Sprintf(`You are helping optimize the extraction prompts for "browse", a terminal web browser.

SITE BEING TESTED: %s (%s)
GOAL: Extract a clean list of items (headlines/links) that renders beautifully in a terminal.

`, tc.name, tc.domain))

	if errorMsg != "" {
		prompt.WriteString(fmt.Sprintf("PROBLEM: Generation failed with error:\n%s\n\n", errorMsg))
	}

	if result != nil {
		prompt.WriteString(fmt.Sprintf(`CURRENT RESULTS:
- Items extracted: %d (want >= %d)
- Link coverage: %.0f%% (want >= 50%%)
- Avg title length: %.1f chars (want >= %d)

SAMPLE ITEMS:
`, len(result.Items), tc.minItems, linkRatio(result)*100, avgTitleLen(result), tc.wantTitleLen))

		for i, item := range result.Items {
			if i >= 8 {
				prompt.WriteString(fmt.Sprintf("... and %d more\n", len(result.Items)-8))
				break
			}
			linkStatus := "NO LINK"
			if item.Href != "" {
				linkStatus = "linked"
			}
			prompt.WriteString(fmt.Sprintf("%d. [%s] %q\n", i+1, linkStatus, truncate(item.Title, 60)))
		}
	}

	prompt.WriteString(`

WHAT I NEED FROM YOU:
1. Identify what's going wrong with the extraction
2. Suggest SPECIFIC changes to improve results
3. Focus on: selector strategies, prompt wording, guardrails

Be concise but specific. What would you change in the prompts to get better results?`)

	// Use single completion for analysis
	response, err := client.Provider().Complete(ctx, prompt.String())
	if err != nil {
		return fmt.Sprintf("Error getting analysis: %v", err)
	}
	return response
}

func evaluateResult(result *rules.ApplyResult, tc testCase) float64 {
	if result == nil || len(result.Items) == 0 {
		return 0
	}

	score := 0.0

	// Item count (0-30 points)
	if len(result.Items) >= tc.minItems {
		score += 30
	} else {
		score += 30 * float64(len(result.Items)) / float64(tc.minItems)
	}

	// Link coverage (0-30 points)
	lr := linkRatio(result)
	if tc.wantLinks {
		score += 30 * lr
	} else {
		score += 30 // Don't penalize if links not required
	}

	// Title quality (0-40 points)
	avgLen := avgTitleLen(result)
	if avgLen >= float64(tc.wantTitleLen) {
		score += 20
	} else {
		score += 20 * avgLen / float64(tc.wantTitleLen)
	}

	// Check for bad titles (section numbers, single words)
	goodTitles := 0
	for _, item := range result.Items {
		if !isBadTitle(item.Title) {
			goodTitles++
		}
	}
	titleQuality := float64(goodTitles) / float64(len(result.Items))
	score += 20 * titleQuality

	return score
}

func isBadTitle(title string) bool {
	title = strings.TrimSpace(title)
	// Empty
	if len(title) < 3 {
		return true
	}
	// Section numbers
	if strings.HasPrefix(title, "1.") || strings.HasPrefix(title, "2.") {
		return true
	}
	// Single words that are likely navigation
	singleWord := !strings.Contains(title, " ")
	navWords := []string{"home", "about", "contact", "menu", "search", "login", "register"}
	for _, nav := range navWords {
		if singleWord && strings.EqualFold(title, nav) {
			return true
		}
	}
	return false
}

func linkRatio(result *rules.ApplyResult) float64 {
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

func avgTitleLen(result *rules.ApplyResult) float64 {
	if result == nil || len(result.Items) == 0 {
		return 0
	}
	total := 0
	for _, item := range result.Items {
		total += len(item.Title)
	}
	return float64(total) / float64(len(result.Items))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
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
