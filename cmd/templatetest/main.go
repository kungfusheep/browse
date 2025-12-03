// Command templatetest tests the new v2 template-based extraction system.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"browse/llm"
	"browse/rules"
)

var (
	url     = flag.String("url", "https://news.ycombinator.com", "URL to test")
	verbose = flag.Bool("v", false, "Verbose output (show selectors)")
)

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

	fmt.Printf("TEMPLATE TEST\n")
	fmt.Printf("URL: %s\n", *url)
	fmt.Printf("LLM: %s\n", client.Provider().Name())
	fmt.Println("════════════════════════════════════════")

	// Fetch HTML
	fmt.Println("\nFetching page...")
	html, err := fetchHTML(*url)
	if err != nil {
		fmt.Printf("Fetch error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Got %d bytes\n", len(html))

	// Generate rules
	fmt.Println("\nGenerating template-based rules...")
	generator := rules.NewGeneratorV2(client)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	domain := extractDomain(*url)
	rule, err := generator.GeneratePageType(ctx, domain, *url, html)
	if err != nil {
		fmt.Printf("Generation error: %v\n", err)
		os.Exit(1)
	}

	// Show what we got
	fmt.Printf("\nGenerated rule (version %d, verified: %v)\n", rule.Version, rule.Verified)

	if *verbose {
		for name, pt := range rule.PageTypes {
			fmt.Printf("\n─── Page Type: %s ───\n", name)
			fmt.Printf("Matcher: %+v\n", pt.Matcher)
			fmt.Println("\nSelectors:")
			for selName, sel := range pt.Selectors {
				fmt.Printf("  %s: %s\n", selName, sel)
			}
			fmt.Println("\nTemplate:")
			fmt.Println(pt.Template)
		}
	}

	// Apply and render
	fmt.Println("\n════════════════════════════════════════")
	fmt.Println("RENDERED OUTPUT:")
	fmt.Println("════════════════════════════════════════")
	fmt.Println()

	result, err := rules.ApplyV2(rule, *url, html)
	if err != nil {
		fmt.Printf("Apply error: %v\n", err)
		os.Exit(1)
	}

	if result == nil {
		fmt.Println("No result from ApplyV2")
		os.Exit(1)
	}

	fmt.Println(result.Content)

	fmt.Printf("\n════════════════════════════════════════\n")
	fmt.Printf("Page type: %s\n", result.PageTypeName)
	fmt.Printf("Links extracted: %d\n", len(result.Links))

	if *verbose && len(result.Links) > 0 {
		fmt.Println("\nFirst 5 links:")
		for i, link := range result.Links {
			if i >= 5 {
				break
			}
			text := link.Text
			if len(text) > 40 {
				text = text[:37] + "..."
			}
			fmt.Printf("  %d. %s → %s\n", i+1, text, link.Href)
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

func extractDomain(rawURL string) string {
	// Simple domain extraction
	url := rawURL
	url = removePrefix(url, "https://")
	url = removePrefix(url, "http://")
	if idx := indexOf(url, "/"); idx != -1 {
		url = url[:idx]
	}
	return url
}

func removePrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func indexOf(s string, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
