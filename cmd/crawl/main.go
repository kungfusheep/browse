// Command crawl discovers and catalogues text-friendly websites.
// It crawls from seed URLs, scores pages based on how well they render,
// and builds a database of sites suitable for text-based browsing.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"browse/html"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/net/html/atom"
	htmlpkg "golang.org/x/net/html"
)

// Config holds crawler configuration
type Config struct {
	DBPath         string
	MaxDepth       int
	MaxDomains     int
	Concurrency    int
	RateLimit      time.Duration
	RequestTimeout time.Duration
	UserAgent      string
}

// Site holds information about a crawled site
type Site struct {
	Domain        string
	URL           string
	Name          string
	Score         int
	ContentNodes  int
	HasRSS        bool
	RSSURL        string
	BotProtected  bool
	NeedsJS       bool
	Category      string
	FirstSeen     time.Time
	LastCrawled   time.Time
	CrawlDepth    int
	ExternalLinks []string
}

// Crawler manages the crawling process
type Crawler struct {
	config     Config
	db         *sql.DB
	client     *http.Client
	visited    map[string]bool
	visitedMu  sync.RWMutex
	queue      chan CrawlJob
	results    chan Site
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	domainLast map[string]time.Time
	domainMu   sync.Mutex
}

// CrawlJob represents a URL to crawl
type CrawlJob struct {
	URL       string
	Depth     int
	FoundFrom string
}

func main() {
	var (
		dbPath      = flag.String("db", "crawler.db", "SQLite database path")
		maxDepth    = flag.Int("depth", 3, "Maximum crawl depth from seeds")
		maxDomains  = flag.Int("max", 10000, "Maximum domains to crawl")
		concurrency = flag.Int("c", 10, "Concurrent crawlers")
		rateLimit   = flag.Duration("rate", 2*time.Second, "Minimum time between requests to same domain")
		timeout     = flag.Duration("timeout", 30*time.Second, "Request timeout")
		seedFile    = flag.String("seeds", "", "File containing seed URLs (one per line)")
		addSeed     = flag.String("add", "", "Add a single seed URL")
		exportJSON  = flag.String("export", "", "Export high-scoring sites to JSON file")
		minScore    = flag.Int("min-score", 50, "Minimum score for export")
		stats       = flag.Bool("stats", false, "Show database statistics")
	)
	flag.Parse()

	config := Config{
		DBPath:         *dbPath,
		MaxDepth:       *maxDepth,
		MaxDomains:     *maxDomains,
		Concurrency:    *concurrency,
		RateLimit:      *rateLimit,
		RequestTimeout: *timeout,
		UserAgent:      "browse-crawler/1.0 (text-browser-catalogue; +https://github.com/anthropics/browse)",
	}

	// Initialize database
	db, err := initDB(config.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Handle different modes
	if *stats {
		showStats(db)
		return
	}

	if *exportJSON != "" {
		if err := exportSites(db, *exportJSON, *minScore); err != nil {
			log.Fatalf("Export failed: %v", err)
		}
		fmt.Printf("Exported sites with score >= %d to %s\n", *minScore, *exportJSON)
		return
	}

	if *addSeed != "" {
		if err := addSeedURL(db, *addSeed); err != nil {
			log.Fatalf("Failed to add seed: %v", err)
		}
		fmt.Printf("Added seed: %s\n", *addSeed)
		return
	}

	if *seedFile != "" {
		count, err := loadSeeds(db, *seedFile)
		if err != nil {
			log.Fatalf("Failed to load seeds: %v", err)
		}
		fmt.Printf("Loaded %d seeds from %s\n", count, *seedFile)
	}

	// Start crawling
	crawler := NewCrawler(config, db)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down gracefully...")
		crawler.Stop()
	}()

	if err := crawler.Run(); err != nil {
		log.Fatalf("Crawler error: %v", err)
	}
}

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	// Performance settings
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=10000",
		"PRAGMA temp_store=MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("pragma %s: %w", p, err)
		}
	}

	// Create tables
	schema := `
	CREATE TABLE IF NOT EXISTS sites (
		domain TEXT PRIMARY KEY,
		url TEXT,
		name TEXT,
		score INTEGER DEFAULT 0,
		content_nodes INTEGER DEFAULT 0,
		has_rss BOOLEAN DEFAULT FALSE,
		rss_url TEXT,
		bot_protected BOOLEAN DEFAULT FALSE,
		needs_js BOOLEAN DEFAULT FALSE,
		category TEXT,
		first_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_crawled TIMESTAMP,
		crawl_depth INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS links (
		from_domain TEXT,
		to_domain TEXT,
		count INTEGER DEFAULT 1,
		PRIMARY KEY (from_domain, to_domain)
	);

	CREATE TABLE IF NOT EXISTS queue (
		url TEXT PRIMARY KEY,
		priority INTEGER DEFAULT 0,
		depth INTEGER DEFAULT 0,
		found_from TEXT,
		added TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_sites_score ON sites(score DESC);
	CREATE INDEX IF NOT EXISTS idx_sites_category ON sites(category);
	CREATE INDEX IF NOT EXISTS idx_queue_priority ON queue(priority DESC);
	CREATE INDEX IF NOT EXISTS idx_links_to ON links(to_domain);
	`

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}

	return db, nil
}

func NewCrawler(config Config, db *sql.DB) *Crawler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Crawler{
		config:     config,
		db:         db,
		client:     &http.Client{Timeout: config.RequestTimeout},
		visited:    make(map[string]bool),
		queue:      make(chan CrawlJob, 10000),
		results:    make(chan Site, 1000),
		ctx:        ctx,
		cancel:     cancel,
		domainLast: make(map[string]time.Time),
	}
}

func (c *Crawler) Run() error {
	// Load visited domains from DB
	if err := c.loadVisited(); err != nil {
		return fmt.Errorf("loading visited: %w", err)
	}

	// Load queue from DB
	jobs, err := c.loadQueue()
	if err != nil {
		return fmt.Errorf("loading queue: %w", err)
	}

	if len(jobs) == 0 {
		fmt.Println("Queue is empty. Add seeds with -seeds or -add")
		return nil
	}

	fmt.Printf("Starting crawl: %d in queue, %d already visited\n", len(jobs), len(c.visited))

	// Start workers
	for i := 0; i < c.config.Concurrency; i++ {
		c.wg.Add(1)
		go c.worker(i)
	}

	// Start result processor
	go c.processResults()

	// Feed initial jobs
	go func() {
		for _, job := range jobs {
			select {
			case c.queue <- job:
			case <-c.ctx.Done():
				return
			}
		}
	}()

	// Wait for completion or shutdown
	c.wg.Wait()
	close(c.results)

	// Save remaining queue
	c.saveQueue()

	return nil
}

func (c *Crawler) Stop() {
	c.cancel()
}

func (c *Crawler) worker(id int) {
	defer c.wg.Done()

	for {
		select {
		case job, ok := <-c.queue:
			if !ok {
				return
			}
			c.crawl(job)
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Crawler) crawl(job CrawlJob) {
	domain := extractDomain(job.URL)
	if domain == "" {
		return
	}

	// Check if already visited
	c.visitedMu.RLock()
	if c.visited[domain] {
		c.visitedMu.RUnlock()
		return
	}
	c.visitedMu.RUnlock()

	// Mark as visited
	c.visitedMu.Lock()
	if c.visited[domain] {
		c.visitedMu.Unlock()
		return
	}
	c.visited[domain] = true
	c.visitedMu.Unlock()

	// Rate limit per domain
	c.rateLimit(domain)

	// Fetch and analyze
	site, err := c.analyzeSite(job)
	if err != nil {
		log.Printf("[%s] Error: %v", domain, err)
		return
	}

	// Send result
	select {
	case c.results <- site:
	case <-c.ctx.Done():
		return
	}

	// Queue external links if within depth
	if job.Depth < c.config.MaxDepth {
		for _, link := range site.ExternalLinks {
			linkDomain := extractDomain(link)
			c.visitedMu.RLock()
			alreadyVisited := c.visited[linkDomain]
			c.visitedMu.RUnlock()

			if !alreadyVisited && !c.isBlacklisted(linkDomain) {
				select {
				case c.queue <- CrawlJob{URL: link, Depth: job.Depth + 1, FoundFrom: domain}:
				default:
					// Queue full, skip
				}
			}
		}
	}
}

func (c *Crawler) analyzeSite(job CrawlJob) (Site, error) {
	domain := extractDomain(job.URL)
	site := Site{
		Domain:      domain,
		URL:         job.URL,
		CrawlDepth:  job.Depth,
		FirstSeen:   time.Now(),
		LastCrawled: time.Now(),
	}

	// Fetch page
	req, err := http.NewRequestWithContext(c.ctx, "GET", job.URL, nil)
	if err != nil {
		return site, err
	}
	req.Header.Set("User-Agent", c.config.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.client.Do(req)
	if err != nil {
		return site, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return site, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB limit
	if err != nil {
		return site, err
	}

	htmlContent := string(body)

	// Check for bot protection
	if isBotProtected(htmlContent) {
		site.BotProtected = true
		site.Score = 10 // Low score but still catalogue it
		return site, nil
	}

	// Parse with our HTML parser
	doc, err := html.ParseString(htmlContent)
	if err != nil {
		return site, err
	}

	// Extract metadata
	site.Name = doc.Title
	if site.Name == "" {
		site.Name = domain
	}

	// Analyze content quality
	site.ContentNodes = countContentNodes(doc)
	site.Score = calculateScore(doc, htmlContent)

	// Find RSS feeds
	rssURL := findRSSFeed(htmlContent, job.URL)
	if rssURL != "" {
		site.HasRSS = true
		site.RSSURL = rssURL
		site.Score += 15 // Bonus for RSS
	}

	// Extract external links
	site.ExternalLinks = extractExternalLinks(htmlContent, domain)

	// Categorize
	site.Category = categorize(domain, doc)

	log.Printf("[%s] Score: %d, Nodes: %d, RSS: %v, Links: %d",
		domain, site.Score, site.ContentNodes, site.HasRSS, len(site.ExternalLinks))

	return site, nil
}

func (c *Crawler) rateLimit(domain string) {
	c.domainMu.Lock()
	last, ok := c.domainLast[domain]
	c.domainLast[domain] = time.Now()
	c.domainMu.Unlock()

	if ok {
		elapsed := time.Since(last)
		if elapsed < c.config.RateLimit {
			time.Sleep(c.config.RateLimit - elapsed)
		}
	}
}

func (c *Crawler) processResults() {
	batch := make([]Site, 0, 100)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := c.saveSites(batch); err != nil {
			log.Printf("Error saving batch: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case site, ok := <-c.results:
			if !ok {
				flush()
				return
			}
			batch = append(batch, site)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (c *Crawler) saveSites(sites []Site) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	siteStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO sites
		(domain, url, name, score, content_nodes, has_rss, rss_url, bot_protected, category, first_seen, last_crawled, crawl_depth)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer siteStmt.Close()

	linkStmt, err := tx.Prepare(`
		INSERT INTO links (from_domain, to_domain, count) VALUES (?, ?, 1)
		ON CONFLICT(from_domain, to_domain) DO UPDATE SET count = count + 1
	`)
	if err != nil {
		return err
	}
	defer linkStmt.Close()

	for _, site := range sites {
		_, err := siteStmt.Exec(
			site.Domain, site.URL, site.Name, site.Score, site.ContentNodes,
			site.HasRSS, site.RSSURL, site.BotProtected, site.Category,
			site.FirstSeen, site.LastCrawled, site.CrawlDepth,
		)
		if err != nil {
			return err
		}

		// Record links
		for _, link := range site.ExternalLinks {
			toDomain := extractDomain(link)
			if toDomain != "" && toDomain != site.Domain {
				linkStmt.Exec(site.Domain, toDomain)
			}
		}
	}

	return tx.Commit()
}

func (c *Crawler) loadVisited() error {
	rows, err := c.db.Query("SELECT domain FROM sites")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return err
		}
		c.visited[domain] = true
	}
	return rows.Err()
}

func (c *Crawler) loadQueue() ([]CrawlJob, error) {
	rows, err := c.db.Query("SELECT url, depth, found_from FROM queue ORDER BY priority DESC LIMIT ?", c.config.MaxDomains)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []CrawlJob
	for rows.Next() {
		var job CrawlJob
		var foundFrom sql.NullString
		if err := rows.Scan(&job.URL, &job.Depth, &foundFrom); err != nil {
			return nil, err
		}
		if foundFrom.Valid {
			job.FoundFrom = foundFrom.String
		}
		jobs = append(jobs, job)
	}

	// Clear loaded items from queue
	c.db.Exec("DELETE FROM queue")

	return jobs, rows.Err()
}

func (c *Crawler) saveQueue() {
	// Save unprocessed items back to queue
	tx, _ := c.db.Begin()
	stmt, _ := tx.Prepare("INSERT OR IGNORE INTO queue (url, depth, found_from) VALUES (?, ?, ?)")

	close(c.queue)
	for job := range c.queue {
		stmt.Exec(job.URL, job.Depth, job.FoundFrom)
	}

	stmt.Close()
	tx.Commit()
}

func (c *Crawler) isBlacklisted(domain string) bool {
	// Skip social media, CDNs, trackers, etc.
	blacklist := []string{
		"facebook.com", "twitter.com", "instagram.com", "tiktok.com",
		"linkedin.com", "pinterest.com", "snapchat.com",
		"google.com", "googleapis.com", "gstatic.com",
		"amazon.com", "amazonaws.com", "cloudfront.net",
		"cloudflare.com", "akamai.com",
		"youtube.com", "youtu.be", "vimeo.com",
		"apple.com", "microsoft.com",
		"doubleclick.net", "googlesyndication.com", "googleadservices.com",
		"t.co", "bit.ly", "tinyurl.com",
	}

	for _, b := range blacklist {
		if strings.HasSuffix(domain, b) || domain == b {
			return true
		}
	}
	return false
}

// Helper functions

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

func isBotProtected(html string) bool {
	if len(html) > 50000 {
		return false
	}
	return strings.Contains(html, "captcha-delivery.com") ||
		strings.Contains(html, "Please enable JS and disable any ad blocker") ||
		strings.Contains(html, "cf-browser-verification") ||
		strings.Contains(html, "Checking your browser")
}

func countContentNodes(doc *html.Document) int {
	if doc == nil || doc.Content == nil {
		return 0
	}
	return countNodes(doc.Content)
}

func countNodes(node *html.Node) int {
	if node == nil {
		return 0
	}
	count := 1
	for _, child := range node.Children {
		count += countNodes(child)
	}
	return count
}

func calculateScore(doc *html.Document, htmlContent string) int {
	score := 50 // Base score

	if doc == nil || doc.Content == nil {
		return 10
	}

	nodes := countContentNodes(doc)

	// Content nodes scoring
	if nodes < 5 {
		score -= 20
	} else if nodes > 20 {
		score += 10
	}
	if nodes > 50 {
		score += 10
	}

	// Has title
	if doc.Title != "" {
		score += 5
	}

	// Check for good structure
	if hasHeadings(doc.Content) {
		score += 10
	}
	if hasLists(doc.Content) {
		score += 5
	}
	if hasParagraphs(doc.Content) {
		score += 5
	}

	// Penalize very small or very large pages
	if len(htmlContent) < 1000 {
		score -= 10
	}
	if len(htmlContent) > 1000000 {
		score -= 10
	}

	// Clamp score
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

func hasHeadings(node *html.Node) bool {
	if node == nil {
		return false
	}
	switch node.Type {
	case html.NodeHeading1, html.NodeHeading2, html.NodeHeading3:
		return true
	}
	for _, child := range node.Children {
		if hasHeadings(child) {
			return true
		}
	}
	return false
}

func hasLists(node *html.Node) bool {
	if node == nil {
		return false
	}
	if node.Type == html.NodeList {
		return true
	}
	for _, child := range node.Children {
		if hasLists(child) {
			return true
		}
	}
	return false
}

func hasParagraphs(node *html.Node) bool {
	if node == nil {
		return false
	}
	if node.Type == html.NodeParagraph {
		return true
	}
	for _, child := range node.Children {
		if hasParagraphs(child) {
			return true
		}
	}
	return false
}

func findRSSFeed(htmlContent, baseURL string) string {
	// Look for RSS/Atom link tags
	doc, err := htmlpkg.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}

	var rssURL string
	var findLinks func(*htmlpkg.Node)
	findLinks = func(n *htmlpkg.Node) {
		if n.Type == htmlpkg.ElementNode && n.DataAtom == atom.Link {
			var rel, typ, href string
			for _, a := range n.Attr {
				switch a.Key {
				case "rel":
					rel = a.Val
				case "type":
					typ = a.Val
				case "href":
					href = a.Val
				}
			}
			if rel == "alternate" && (strings.Contains(typ, "rss") || strings.Contains(typ, "atom")) {
				rssURL = href
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if rssURL != "" {
				return
			}
			findLinks(c)
		}
	}
	findLinks(doc)

	// Resolve relative URL
	if rssURL != "" && !strings.HasPrefix(rssURL, "http") {
		base, err := url.Parse(baseURL)
		if err == nil {
			ref, err := url.Parse(rssURL)
			if err == nil {
				rssURL = base.ResolveReference(ref).String()
			}
		}
	}

	return rssURL
}

func extractExternalLinks(htmlContent, currentDomain string) []string {
	doc, err := htmlpkg.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var links []string

	var findLinks func(*htmlpkg.Node)
	findLinks = func(n *htmlpkg.Node) {
		if n.Type == htmlpkg.ElementNode && n.DataAtom == atom.A {
			for _, a := range n.Attr {
				if a.Key == "href" && strings.HasPrefix(a.Val, "http") {
					domain := extractDomain(a.Val)
					if domain != "" && domain != currentDomain && !seen[domain] {
						seen[domain] = true
						links = append(links, a.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findLinks(c)
		}
	}
	findLinks(doc)

	// Limit to avoid huge lists
	if len(links) > 50 {
		links = links[:50]
	}

	return links
}

func categorize(domain string, doc *html.Document) string {
	// Simple categorization based on domain patterns
	if strings.HasSuffix(domain, ".gov") {
		return "government"
	}
	if strings.HasSuffix(domain, ".edu") {
		return "education"
	}
	if strings.HasSuffix(domain, ".org") {
		return "organization"
	}
	if strings.Contains(domain, "news") || strings.Contains(domain, "times") ||
		strings.Contains(domain, "post") || strings.Contains(domain, "herald") {
		return "news"
	}
	if strings.Contains(domain, "blog") {
		return "blog"
	}
	if strings.Contains(domain, "docs") || strings.Contains(domain, "wiki") {
		return "documentation"
	}
	return "general"
}

func addSeedURL(db *sql.DB, rawURL string) error {
	// Normalize URL
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = "https://" + rawURL
	}

	_, err := db.Exec(
		"INSERT OR IGNORE INTO queue (url, priority, depth) VALUES (?, 100, 0)",
		rawURL,
	)
	return err
}

func loadSeeds(db *sql.DB, filename string) (int, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return 0, err
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("INSERT OR IGNORE INTO queue (url, priority, depth) VALUES (?, 100, 0)")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "http") {
			line = "https://" + line
		}
		if _, err := stmt.Exec(line); err == nil {
			count++
		}
	}

	return count, tx.Commit()
}

func showStats(db *sql.DB) {
	var total, withRSS, botProtected int
	var avgScore float64

	db.QueryRow("SELECT COUNT(*) FROM sites").Scan(&total)
	db.QueryRow("SELECT COUNT(*) FROM sites WHERE has_rss = 1").Scan(&withRSS)
	db.QueryRow("SELECT COUNT(*) FROM sites WHERE bot_protected = 1").Scan(&botProtected)
	db.QueryRow("SELECT AVG(score) FROM sites").Scan(&avgScore)

	fmt.Printf("Database Statistics:\n")
	fmt.Printf("  Total sites:     %d\n", total)
	fmt.Printf("  With RSS:        %d (%.1f%%)\n", withRSS, float64(withRSS)/float64(total)*100)
	fmt.Printf("  Bot protected:   %d (%.1f%%)\n", botProtected, float64(botProtected)/float64(total)*100)
	fmt.Printf("  Average score:   %.1f\n", avgScore)

	fmt.Printf("\nTop 10 by score:\n")
	rows, _ := db.Query("SELECT domain, score, has_rss FROM sites ORDER BY score DESC LIMIT 10")
	defer rows.Close()
	for rows.Next() {
		var domain string
		var score int
		var hasRSS bool
		rows.Scan(&domain, &score, &hasRSS)
		rss := ""
		if hasRSS {
			rss = " [RSS]"
		}
		fmt.Printf("  %3d  %s%s\n", score, domain, rss)
	}

	fmt.Printf("\nBy category:\n")
	rows2, _ := db.Query("SELECT category, COUNT(*), AVG(score) FROM sites GROUP BY category ORDER BY COUNT(*) DESC")
	defer rows2.Close()
	for rows2.Next() {
		var cat string
		var count int
		var avg float64
		rows2.Scan(&cat, &count, &avg)
		if cat == "" {
			cat = "uncategorized"
		}
		fmt.Printf("  %-15s %5d sites (avg: %.1f)\n", cat, count, avg)
	}

	var queueSize int
	db.QueryRow("SELECT COUNT(*) FROM queue").Scan(&queueSize)
	fmt.Printf("\nQueue size: %d\n", queueSize)
}

func exportSites(db *sql.DB, filename string, minScore int) error {
	rows, err := db.Query(`
		SELECT domain, url, name, score, content_nodes, has_rss, rss_url, bot_protected, category
		FROM sites
		WHERE score >= ? AND bot_protected = 0
		ORDER BY score DESC
	`, minScore)
	if err != nil {
		return err
	}
	defer rows.Close()

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString("[\n")
	first := true
	for rows.Next() {
		var domain, url, name, rssURL, category string
		var score, contentNodes int
		var hasRSS, botProtected bool

		if err := rows.Scan(&domain, &url, &name, &score, &contentNodes, &hasRSS, &rssURL, &botProtected, &category); err != nil {
			continue
		}

		if !first {
			f.WriteString(",\n")
		}
		first = false

		// Manual JSON to avoid importing encoding/json
		f.WriteString(fmt.Sprintf(`  {"domain": %q, "url": %q, "name": %q, "score": %d, "has_rss": %v, "rss_url": %q, "category": %q}`,
			domain, url, name, score, hasRSS, rssURL, category))
	}
	f.WriteString("\n]\n")

	return nil
}
