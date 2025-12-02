// Package fetcher provides HTTP fetching with optional browser rendering fallback.
package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chromedp/chromedp"
)

// FetchResult contains the fetched HTML and metadata.
type FetchResult struct {
	HTML         string
	UsedBrowser  bool
	FetchTime    time.Duration
}

// Simple fetches a URL using standard HTTP (fast, low bandwidth).
func Simple(url string) (*FetchResult, error) {
	start := time.Now()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "Browse/1.0 (Terminal Browser)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	return &FetchResult{
		HTML:        string(body),
		UsedBrowser: false,
		FetchTime:   time.Since(start),
	}, nil
}

// WithBrowser fetches a URL using headless Chrome to execute JavaScript.
// This is slower but handles JS-rendered content.
func WithBrowser(url string) (*FetchResult, error) {
	start := time.Now()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create browser context
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	var html string
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		// Give JS a moment to render
		chromedp.Sleep(500*time.Millisecond),
		chromedp.OuterHTML("html", &html),
	)
	if err != nil {
		return nil, fmt.Errorf("browser fetch: %w", err)
	}

	return &FetchResult{
		HTML:        html,
		UsedBrowser: true,
		FetchTime:   time.Since(start),
	}, nil
}
