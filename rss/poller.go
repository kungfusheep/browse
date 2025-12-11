package rss

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// Poller handles background feed refresh.
type Poller struct {
	store       *Store
	interval    time.Duration
	maxItems    int
	concurrency int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Channel for triggering immediate refresh
	refreshCh chan struct{}
}

// NewPoller creates a new background feed poller.
func NewPoller(store *Store, interval time.Duration) *Poller {
	ctx, cancel := context.WithCancel(context.Background())
	return &Poller{
		store:       store,
		interval:    interval,
		maxItems:    100, // Default max items per feed
		concurrency: 3,   // Max concurrent fetches
		ctx:         ctx,
		cancel:      cancel,
		refreshCh:   make(chan struct{}, 1),
	}
}

// SetMaxItems sets the maximum items to keep per feed.
func (p *Poller) SetMaxItems(max int) {
	p.maxItems = max
}

// Start begins the background polling loop.
func (p *Poller) Start() {
	p.wg.Add(1)
	go p.loop()
}

// Stop gracefully shuts down the poller.
func (p *Poller) Stop() {
	p.cancel()
	p.wg.Wait()
}

// RefreshNow triggers an immediate refresh of all feeds.
func (p *Poller) RefreshNow() {
	select {
	case p.refreshCh <- struct{}{}:
	default:
		// Already a refresh pending
	}
}

// loop is the main polling goroutine.
func (p *Poller) loop() {
	defer p.wg.Done()

	// Initial refresh on startup
	p.refreshAll()

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.refreshAll()
		case <-p.refreshCh:
			p.refreshAll()
		}
	}
}

// refreshAll refreshes all subscribed feeds with rate limiting.
func (p *Poller) refreshAll() {
	feeds := p.store.Feeds
	if len(feeds) == 0 {
		return
	}

	// Use semaphore for concurrency limiting
	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup

	for _, feed := range feeds {
		wg.Add(1)
		go func(feedURL string) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Check context before fetching
			select {
			case <-p.ctx.Done():
				return
			default:
			}

			p.refreshFeed(feedURL)
		}(feed.URL)
	}

	wg.Wait()
	p.store.Save()
}

// refreshFeed fetches and updates a single feed.
func (p *Poller) refreshFeed(feedURL string) {
	// Create request with timeout
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", feedURL, nil)
	if err != nil {
		p.store.SetFeedError(feedURL, err.Error())
		return
	}

	// Set a reasonable User-Agent
	req.Header.Set("User-Agent", "Browse-RSS/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.store.SetFeedError(feedURL, err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		p.store.SetFeedError(feedURL, resp.Status)
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		p.store.SetFeedError(feedURL, err.Error())
		return
	}

	parsed, err := Parse(data)
	if err != nil {
		p.store.SetFeedError(feedURL, err.Error())
		return
	}

	// Update feed metadata
	p.store.mu.Lock()
	for i := range p.store.Feeds {
		if p.store.Feeds[i].URL == feedURL {
			p.store.Feeds[i].LastFetch = time.Now()
			p.store.Feeds[i].FetchError = ""
			if parsed.Title != "" && p.store.Feeds[i].Title == "" {
				p.store.Feeds[i].Title = parsed.Title
			}
			if parsed.Description != "" && p.store.Feeds[i].Description == "" {
				p.store.Feeds[i].Description = parsed.Description
			}
			if parsed.Link != "" && p.store.Feeds[i].SiteURL == "" {
				p.store.Feeds[i].SiteURL = parsed.Link
			}
			break
		}
	}
	p.store.mu.Unlock()

	// Update items
	p.store.UpdateFeed(feedURL, parsed, p.maxItems)
}

// RefreshFeed refreshes a single feed immediately.
func (p *Poller) RefreshFeed(feedURL string) {
	go func() {
		p.refreshFeed(feedURL)
		p.store.Save()
	}()
}
