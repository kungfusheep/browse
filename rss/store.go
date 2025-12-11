package rss

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Feed represents an RSS/Atom feed subscription.
type Feed struct {
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	SiteURL     string    `json:"site_url,omitempty"`
	AddedAt     time.Time `json:"added_at"`
	LastFetch   time.Time `json:"last_fetch,omitempty"`
	FetchError  string    `json:"fetch_error,omitempty"`
	Category    string    `json:"category,omitempty"`
}

// FeedItem represents a single item from a feed.
type FeedItem struct {
	GUID        string    `json:"guid"`
	FeedURL     string    `json:"feed_url"`
	Title       string    `json:"title"`
	Link        string    `json:"link"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Published   time.Time `json:"published"`
}

// Store manages RSS subscriptions and item state.
type Store struct {
	mu        sync.RWMutex
	path      string
	Feeds     []Feed                `json:"feeds"`
	Items     map[string][]FeedItem `json:"items"`      // keyed by feed URL
	ReadGUIDs map[string]time.Time  `json:"read_guids"` // GUID -> read timestamp
}

// configDir returns the configuration directory path.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "browse"), nil
}

// Load reads RSS state from disk, creating the file if it doesn't exist.
func Load() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}

	// Ensure config directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "rss.json")
	store := &Store{
		path:      path,
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// No RSS state yet, return empty store
		return store, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}

	// Initialize maps if nil after unmarshal
	if store.Items == nil {
		store.Items = make(map[string][]FeedItem)
	}
	if store.ReadGUIDs == nil {
		store.ReadGUIDs = make(map[string]time.Time)
	}

	return store, nil
}

// Save writes RSS state to disk.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Prune old read markers (> 30 days)
	s.pruneOldReadMarkers()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// pruneOldReadMarkers removes read markers older than 30 days.
func (s *Store) pruneOldReadMarkers() {
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for guid, readAt := range s.ReadGUIDs {
		if readAt.Before(cutoff) {
			delete(s.ReadGUIDs, guid)
		}
	}
}

// Subscribe adds a new feed subscription.
func (s *Store) Subscribe(feedURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate
	for _, f := range s.Feeds {
		if f.URL == feedURL {
			return errors.New("already subscribed")
		}
	}

	s.Feeds = append(s.Feeds, Feed{
		URL:     feedURL,
		AddedAt: time.Now(),
	})
	return nil
}

// Unsubscribe removes a feed subscription and its items.
func (s *Store) Unsubscribe(feedURL string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, f := range s.Feeds {
		if f.URL == feedURL {
			s.Feeds = append(s.Feeds[:i], s.Feeds[i+1:]...)
			delete(s.Items, feedURL)
			return true
		}
	}
	return false
}

// MarkRead marks an item as read.
func (s *Store) MarkRead(guid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ReadGUIDs[guid] = time.Now()
}

// MarkUnread marks an item as unread.
func (s *Store) MarkUnread(guid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ReadGUIDs, guid)
}

// IsRead checks if an item has been read.
func (s *Store) IsRead(guid string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.ReadGUIDs[guid]
	return ok
}

// UnreadCount returns the number of unread items for a feed.
func (s *Store) UnreadCount(feedURL string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, item := range s.Items[feedURL] {
		if _, read := s.ReadGUIDs[item.GUID]; !read {
			count++
		}
	}
	return count
}

// AllUnreadCount returns the total number of unread items across all feeds.
func (s *Store) AllUnreadCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, items := range s.Items {
		for _, item := range items {
			if _, read := s.ReadGUIDs[item.GUID]; !read {
				count++
			}
		}
	}
	return count
}

// GetFeed returns a feed by URL.
func (s *Store) GetFeed(feedURL string) *Feed {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i := range s.Feeds {
		if s.Feeds[i].URL == feedURL {
			return &s.Feeds[i]
		}
	}
	return nil
}

// GetItems returns items for a feed, sorted by date descending.
func (s *Store) GetItems(feedURL string) []FeedItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]FeedItem, len(s.Items[feedURL]))
	copy(items, s.Items[feedURL])
	return items
}

// GetAllItems returns all items across all feeds, sorted by date descending.
func (s *Store) GetAllItems() []FeedItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var all []FeedItem
	for _, items := range s.Items {
		all = append(all, items...)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Published.After(all[j].Published)
	})

	return all
}

// GetUnreadItems returns unread items across all feeds, sorted by date descending.
func (s *Store) GetUnreadItems() []FeedItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var unread []FeedItem
	for _, items := range s.Items {
		for _, item := range items {
			if _, read := s.ReadGUIDs[item.GUID]; !read {
				unread = append(unread, item)
			}
		}
	}

	sort.Slice(unread, func(i, j int) bool {
		return unread[i].Published.After(unread[j].Published)
	})

	return unread
}

// MarkReadByLink marks an item as read by its link URL.
// Returns true if an item was found and marked, false otherwise.
func (s *Store) MarkReadByLink(linkURL string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, items := range s.Items {
		for _, item := range items {
			if item.Link == linkURL {
				s.ReadGUIDs[item.GUID] = time.Now()
				return true
			}
		}
	}
	return false
}

// UpdateFeed stores fetched items for a feed.
func (s *Store) UpdateFeed(feedURL string, parsed *ParsedFeed, maxItems int) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update feed metadata
	for i := range s.Feeds {
		if s.Feeds[i].URL == feedURL {
			if parsed.Title != "" {
				s.Feeds[i].Title = parsed.Title
			}
			if parsed.Description != "" {
				s.Feeds[i].Description = parsed.Description
			}
			if parsed.Link != "" {
				s.Feeds[i].SiteURL = parsed.Link
			}
			s.Feeds[i].LastFetch = time.Now()
			s.Feeds[i].FetchError = ""
			break
		}
	}

	// Build set of existing GUIDs
	existingGUIDs := make(map[string]bool)
	for _, item := range s.Items[feedURL] {
		existingGUIDs[item.GUID] = true
	}

	// Add new items
	newCount := 0
	for _, pi := range parsed.Items {
		if !existingGUIDs[pi.GUID] {
			s.Items[feedURL] = append(s.Items[feedURL], FeedItem{
				GUID:        pi.GUID,
				FeedURL:     feedURL,
				Title:       pi.Title,
				Link:        pi.Link,
				Description: pi.Description,
				Author:      pi.Author,
				Published:   pi.Published,
			})
			newCount++
		}
	}

	// Sort by date descending
	sort.Slice(s.Items[feedURL], func(i, j int) bool {
		return s.Items[feedURL][i].Published.After(s.Items[feedURL][j].Published)
	})

	// Limit to maxItems per feed
	if maxItems > 0 && len(s.Items[feedURL]) > maxItems {
		s.Items[feedURL] = s.Items[feedURL][:maxItems]
	}

	return newCount
}

// SetFeedError records a fetch error for a feed.
func (s *Store) SetFeedError(feedURL string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.Feeds {
		if s.Feeds[i].URL == feedURL {
			s.Feeds[i].FetchError = errMsg
			s.Feeds[i].LastFetch = time.Now()
			break
		}
	}
}

// Len returns the number of feed subscriptions.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Feeds)
}

// InitMaps initializes the internal maps (for testing).
func (s *Store) InitMaps() {
	s.Items = make(map[string][]FeedItem)
	s.ReadGUIDs = make(map[string]time.Time)
}

// SetPath sets the storage path (for testing).
func (s *Store) SetPath(path string) {
	s.path = path
}

// LoadFrom loads RSS state from a specific file path.
func LoadFrom(path string) (*Store, error) {
	store := &Store{
		path:      path,
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}

	if store.Items == nil {
		store.Items = make(map[string][]FeedItem)
	}
	if store.ReadGUIDs == nil {
		store.ReadGUIDs = make(map[string]time.Time)
	}

	return store, nil
}

// MarkAllRead marks all items as read.
func (s *Store) MarkAllRead() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, items := range s.Items {
		for _, item := range items {
			s.ReadGUIDs[item.GUID] = now
		}
	}
}

// MarkFeedRead marks all items in a feed as read.
func (s *Store) MarkFeedRead(feedURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for _, item := range s.Items[feedURL] {
		s.ReadGUIDs[item.GUID] = now
	}
}
