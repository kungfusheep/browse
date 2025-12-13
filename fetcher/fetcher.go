// Package fetcher provides HTTP fetching with optional browser rendering fallback.
package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// FetchResult contains the fetched HTML and metadata.
type FetchResult struct {
	HTML        string
	FinalURL    string        // URL after following redirects
	UsedBrowser bool
	FetchTime   time.Duration
}

// Options configures the fetcher behavior.
type Options struct {
	UserAgent      string
	TimeoutSeconds int
	ChromePath     string // Path to Chrome binary (empty = auto-detect)
}

// DefaultOptions returns sensible defaults.
func DefaultOptions() Options {
	return Options{
		UserAgent:      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		TimeoutSeconds: 30,
		ChromePath:     "",
	}
}

// Package-level options (set via Configure)
var opts = DefaultOptions()

// Configure sets the package-level options.
func Configure(o Options) {
	if o.UserAgent != "" {
		opts.UserAgent = o.UserAgent
	}
	if o.TimeoutSeconds > 0 {
		opts.TimeoutSeconds = o.TimeoutSeconds
	}
	opts.ChromePath = o.ChromePath // Can be empty
}

// UserAgent returns the currently configured user agent string.
func UserAgent() string {
	return opts.UserAgent
}

// Timeout returns the currently configured timeout duration.
func Timeout() time.Duration {
	return time.Duration(opts.TimeoutSeconds) * time.Second
}

// Realistic Chrome user agent
const chromeUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// userDataDir returns a persistent directory for Chrome user data.
// This allows cookies and other session data to persist between fetches.
func userDataDir() string {
	dir, _ := os.UserCacheDir()
	return filepath.Join(dir, "browse-chrome-profile")
}

// IsGoogleSearch returns true if the URL is a Google search URL.
func IsGoogleSearch(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	return strings.Contains(u.Host, "google.") && strings.HasPrefix(u.Path, "/search")
}

// OptimizeGoogleURL adds parameters to Google URLs that help avoid bot detection.
func OptimizeGoogleURL(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return urlStr
	}
	if !IsGoogleSearch(urlStr) {
		return urlStr
	}
	q := u.Query()
	// Use basic HTML version (fewer JS requirements)
	q.Set("gbv", "1")
	// Set locale to avoid consent pages
	q.Set("hl", "en")
	q.Set("gl", "us")
	// Disable personalization
	q.Set("pws", "0")
	u.RawQuery = q.Encode()
	return u.String()
}

// Simple fetches a URL using standard HTTP (fast, low bandwidth).
func Simple(url string) (*FetchResult, error) {
	start := time.Now()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", opts.UserAgent)

	client := &http.Client{
		Timeout: time.Duration(opts.TimeoutSeconds) * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Capture final URL after redirects
	finalURL := resp.Request.URL.String()

	return &FetchResult{
		HTML:        string(body),
		FinalURL:    finalURL,
		UsedBrowser: false,
		FetchTime:   time.Since(start),
	}, nil
}

// stealthScript contains JavaScript to mask automation detection.
// Based on puppeteer-extra-plugin-stealth techniques.
const stealthScript = `
// Mask webdriver property
Object.defineProperty(navigator, 'webdriver', {
    get: () => undefined,
});

// Add Chrome runtime object
window.chrome = {
    runtime: {},
    loadTimes: function() {},
    csi: function() {},
    app: {},
};

// Mask automation-controlled flag
Object.defineProperty(navigator, 'plugins', {
    get: () => [
        { name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
        { name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
        { name: 'Native Client', filename: 'internal-nacl-plugin', description: '' },
    ],
});

// Set realistic languages
Object.defineProperty(navigator, 'languages', {
    get: () => ['en-US', 'en'],
});

// Mask permissions query
const originalQuery = window.navigator.permissions.query;
window.navigator.permissions.query = (parameters) => (
    parameters.name === 'notifications' ?
        Promise.resolve({ state: Notification.permission }) :
        originalQuery(parameters)
);

// Add realistic screen properties
Object.defineProperty(screen, 'availWidth', { get: () => window.innerWidth });
Object.defineProperty(screen, 'availHeight', { get: () => window.innerHeight });

// Mask headless indicators in WebGL
const getParameter = WebGLRenderingContext.prototype.getParameter;
WebGLRenderingContext.prototype.getParameter = function(parameter) {
    if (parameter === 37445) {
        return 'Intel Inc.';
    }
    if (parameter === 37446) {
        return 'Intel Iris OpenGL Engine';
    }
    return getParameter.apply(this, arguments);
};

// Prevent detection via toString
const originalFunction = Function.prototype.toString;
Function.prototype.toString = function() {
    if (this === window.navigator.permissions.query) {
        return 'function query() { [native code] }';
    }
    return originalFunction.apply(this, arguments);
};
`

// WithBrowser fetches a URL using headless Chrome to execute JavaScript.
// This is slower but handles JS-rendered content and includes anti-detection measures.
func WithBrowser(targetURL string) (*FetchResult, error) {
	// For Google searches, try to handle consent pages
	if IsGoogleSearch(targetURL) {
		return withBrowserGoogle(targetURL)
	}
	return withBrowserInternal(targetURL, true)
}

// WithBrowserVisible fetches using a visible (non-headless) Chrome window.
// This is useful for sites that detect and block headless browsers.
func WithBrowserVisible(targetURL string) (*FetchResult, error) {
	return withBrowserInternal(targetURL, false)
}

// withBrowserGoogle fetches Google search results with consent handling.
func withBrowserGoogle(targetURL string) (*FetchResult, error) {
	start := time.Now()

	allocOpts := []chromedp.ExecAllocatorOption{
		chromedp.NoDefaultBrowserCheck,
		chromedp.NoFirstRun,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-service-autorun", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.UserAgent(opts.UserAgent),
		chromedp.WindowSize(1920, 1080),
		chromedp.UserDataDir(userDataDir()),
		chromedp.Flag("headless", "new"),
	}

	// Add custom Chrome path if specified
	if opts.ChromePath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(opts.ChromePath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer allocCancel()

	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if timeout < 30*time.Second {
		timeout = 60 * time.Second // Browser fetches need more time
	} else {
		timeout = timeout * 2 // Double the timeout for browser fetches
	}
	ctx, cancel := context.WithTimeout(allocCtx, timeout)
	defer cancel()

	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	var html string
	var finalURL string
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(stealthScript).Do(ctx)
			return err
		}),
		network.SetExtraHTTPHeaders(network.Headers(map[string]interface{}{
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.9",
			"Sec-Ch-Ua":       `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-Ch-Ua-Mobile": "?0",
			"Sec-Ch-Ua-Platform": `"macOS"`,
		})),
		// First navigate to google.com to set region cookie
		chromedp.Navigate("https://www.google.com/?hl=en&gl=us"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Check for and handle consent page
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Try clicking "Accept all" or "I agree" buttons if present
			var exists bool
			// Try multiple selectors for consent buttons
			selectors := []string{
				`button[id*="accept"]`,
				`button:contains("Accept all")`,
				`button:contains("I agree")`,
				`#L2AGLb`, // Google's "Accept all" button ID
				`button[aria-label*="Accept"]`,
			}
			for _, sel := range selectors {
				err := chromedp.Run(ctx,
					chromedp.Evaluate(`document.querySelector('`+sel+`') !== null`, &exists),
				)
				if err == nil && exists {
					chromedp.Click(sel, chromedp.ByQuery).Do(ctx)
					chromedp.Sleep(1 * time.Second).Do(ctx)
					break
				}
			}
			return nil
		}),
		chromedp.Sleep(1*time.Second),
		// Now navigate to actual search
		chromedp.Navigate(targetURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		// Handle consent again if it appears
		chromedp.ActionFunc(func(ctx context.Context) error {
			var title string
			chromedp.Title(&title).Do(ctx)
			if contains(title, "Before you continue") || contains(title, "consent") {
				// Try clicking consent button
				chromedp.Click(`#L2AGLb`, chromedp.ByQuery).Do(ctx)
				chromedp.Sleep(2 * time.Second).Do(ctx)
			}
			return nil
		}),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
		// Capture final URL after any redirects
		chromedp.Location(&finalURL),
	)
	if err != nil {
		return nil, fmt.Errorf("browser fetch: %w", err)
	}

	return &FetchResult{
		HTML:        html,
		FinalURL:    finalURL,
		UsedBrowser: true,
		FetchTime:   time.Since(start),
	}, nil
}

func withBrowserInternal(targetURL string, headless bool) (*FetchResult, error) {
	start := time.Now()

	// Create allocator with anti-detection options
	allocOpts := []chromedp.ExecAllocatorOption{
		chromedp.NoDefaultBrowserCheck,
		chromedp.NoFirstRun,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("exclude-switches", "enable-automation"),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-service-autorun", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		chromedp.UserAgent(opts.UserAgent),
		chromedp.WindowSize(1920, 1080),
		// Use persistent user data directory for cookies
		chromedp.UserDataDir(userDataDir()),
	}

	// Add headless flag based on parameter
	if headless {
		// Use new headless mode (more like regular Chrome)
		allocOpts = append(allocOpts, chromedp.Flag("headless", "new"))
	}

	// Add custom Chrome path if specified
	if opts.ChromePath != "" {
		allocOpts = append(allocOpts, chromedp.ExecPath(opts.ChromePath))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer allocCancel()

	// Create context with timeout (browser fetches get extra time)
	timeout := time.Duration(opts.TimeoutSeconds) * time.Second
	if timeout < 30*time.Second {
		timeout = 45 * time.Second
	} else {
		timeout = timeout + 15*time.Second
	}
	ctx, cancel := context.WithTimeout(allocCtx, timeout)
	defer cancel()

	// Create browser context
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	var html string
	var finalURL string
	err := chromedp.Run(ctx,
		// Inject stealth script before page loads
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(stealthScript).Do(ctx)
			return err
		}),
		// Set extra headers
		network.SetExtraHTTPHeaders(network.Headers(map[string]interface{}{
			"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
			"Accept-Language": "en-US,en;q=0.9",
			"Cache-Control":   "no-cache",
			"Pragma":          "no-cache",
			"Sec-Ch-Ua":       `"Not_A Brand";v="8", "Chromium";v="120", "Google Chrome";v="120"`,
			"Sec-Ch-Ua-Mobile": "?0",
			"Sec-Ch-Ua-Platform": `"macOS"`,
			"Sec-Fetch-Dest":  "document",
			"Sec-Fetch-Mode":  "navigate",
			"Sec-Fetch-Site":  "none",
			"Sec-Fetch-User":  "?1",
			"Upgrade-Insecure-Requests": "1",
		})),
		chromedp.Navigate(targetURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		// Wait for potential JS rendering and challenges
		chromedp.Sleep(2*time.Second),
		// Check if we hit a challenge page and wait longer if needed
		chromedp.ActionFunc(func(ctx context.Context) error {
			var title string
			if err := chromedp.Title(&title).Do(ctx); err != nil {
				return nil // Ignore errors, continue
			}
			// If Cloudflare challenge detected, wait longer
			if title == "Just a moment..." {
				return chromedp.Sleep(5 * time.Second).Do(ctx)
			}
			return nil
		}),
		// Check for DataDome and wait for it to complete
		chromedp.ActionFunc(func(ctx context.Context) error {
			var bodyHTML string
			if err := chromedp.OuterHTML("body", &bodyHTML, chromedp.ByQuery).Do(ctx); err != nil {
				return nil
			}
			// DataDome signature - wait longer for JS challenge to complete
			if strings.Contains(bodyHTML, "captcha-delivery.com") ||
				strings.Contains(bodyHTML, "Please enable JS") {
				// Wait for DataDome to complete its check
				chromedp.Sleep(5 * time.Second).Do(ctx)
				// Check again - if still blocked, try waiting more
				chromedp.OuterHTML("body", &bodyHTML, chromedp.ByQuery).Do(ctx)
				if strings.Contains(bodyHTML, "captcha-delivery.com") {
					chromedp.Sleep(5 * time.Second).Do(ctx)
				}
			}
			return nil
		}),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
		// Capture final URL after any redirects
		chromedp.Location(&finalURL),
	)
	if err != nil {
		return nil, fmt.Errorf("browser fetch: %w", err)
	}

	return &FetchResult{
		HTML:        html,
		FinalURL:    finalURL,
		UsedBrowser: true,
		FetchTime:   time.Since(start),
	}, nil
}

// WithBrowserRetry attempts to fetch with browser, retrying if Cloudflare is detected.
func WithBrowserRetry(url string, maxRetries int) (*FetchResult, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		result, err := WithBrowser(url)
		if err != nil {
			lastErr = err
			continue
		}
		// Check if we got a Cloudflare challenge page
		if len(result.HTML) < 5000 && (contains(result.HTML, "Just a moment...") ||
			contains(result.HTML, "Checking your browser") ||
			contains(result.HTML, "cf-browser-verification")) {
			lastErr = fmt.Errorf("cloudflare challenge detected")
			time.Sleep(time.Duration(i+1) * 2 * time.Second) // Exponential backoff
			continue
		}
		return result, nil
	}
	return nil, fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// IsBlockedResponse checks if the HTML indicates a blocked/challenged page.
func IsBlockedResponse(html string) (bool, string) {
	// Check for various blocking indicators
	if contains(html, "unusual traffic from your computer") {
		return true, "Google CAPTCHA"
	}
	if contains(html, "detected unusual traffic") {
		return true, "Google CAPTCHA"
	}
	if contains(html, "recaptcha") && len(html) < 10000 {
		return true, "reCAPTCHA challenge"
	}
	if contains(html, "Just a moment...") {
		return true, "Cloudflare challenge"
	}
	if contains(html, "Checking your browser") {
		return true, "Cloudflare challenge"
	}
	if contains(html, "cf-browser-verification") {
		return true, "Cloudflare challenge"
	}
	if contains(html, "Before you continue") && contains(html, "consent.google") {
		return true, "Google consent page"
	}
	// DataDome bot protection (used by Reuters, WSJ, etc.)
	if contains(html, "captcha-delivery.com") || contains(html, "DataDome") {
		return true, "DataDome bot protection"
	}
	// Akamai Bot Manager
	if contains(html, "akam/") && len(html) < 5000 {
		return true, "Akamai bot protection"
	}
	// PerimeterX
	if contains(html, "perimeterx") || contains(html, "px-captcha") {
		return true, "PerimeterX bot protection"
	}
	return false, ""
}

// Smart fetches a URL using the best available method.
// For regular sites, it tries simple HTTP first, then falls back to browser.
// For Google searches, it uses optimized parameters and browser fetch.
func Smart(targetURL string) (*FetchResult, error) {
	// Optimize Google URLs
	if IsGoogleSearch(targetURL) {
		targetURL = OptimizeGoogleURL(targetURL)
	}

	// First try simple HTTP
	result, err := Simple(targetURL)
	if err == nil {
		blocked, _ := IsBlockedResponse(result.HTML)
		if !blocked && len(result.HTML) > 5000 {
			return result, nil
		}
	}

	// Fall back to browser fetch
	result, err = WithBrowser(targetURL)
	if err != nil {
		return nil, err
	}

	// Check if we got blocked
	if blocked, reason := IsBlockedResponse(result.HTML); blocked {
		return result, fmt.Errorf("blocked: %s", reason)
	}

	return result, nil
}

// GoogleToDuckDuckGo converts a Google search URL to DuckDuckGo.
func GoogleToDuckDuckGo(googleURL string) string {
	u, err := url.Parse(googleURL)
	if err != nil || !IsGoogleSearch(googleURL) {
		return googleURL
	}
	q := u.Query().Get("q")
	if q == "" {
		return googleURL
	}
	return "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(q)
}

// IsDuckDuckGoSearch returns true if the URL is a DuckDuckGo search URL.
func IsDuckDuckGoSearch(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	return strings.Contains(u.Host, "duckduckgo.com")
}

// SmartSearch fetches search results, falling back to DuckDuckGo if Google blocks us.
func SmartSearch(targetURL string) (*FetchResult, error) {
	// If it's not a search, just use Smart
	if !IsGoogleSearch(targetURL) && !IsDuckDuckGoSearch(targetURL) {
		return Smart(targetURL)
	}

	// For DuckDuckGo, use simple HTTP directly (their HTML version works great)
	if IsDuckDuckGoSearch(targetURL) {
		return Simple(targetURL)
	}

	// For Google, try browser fetch first
	if IsGoogleSearch(targetURL) {
		optimized := OptimizeGoogleURL(targetURL)
		result, err := WithBrowser(optimized)
		if err == nil {
			blocked, _ := IsBlockedResponse(result.HTML)
			if !blocked {
				return result, nil
			}
		}
		// Google blocked us, fall back to DuckDuckGo
		ddgURL := GoogleToDuckDuckGo(targetURL)
		return Simple(ddgURL)
	}

	return Smart(targetURL)
}
