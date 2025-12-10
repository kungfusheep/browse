package rss

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreSubscribe(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	// First subscribe should succeed
	err := store.Subscribe("https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if store.Len() != 1 {
		t.Errorf("expected 1 feed, got %d", store.Len())
	}

	// Duplicate subscribe should fail
	err = store.Subscribe("https://example.com/feed.xml")
	if err == nil {
		t.Error("expected error for duplicate subscribe")
	}
	if store.Len() != 1 {
		t.Errorf("expected 1 feed after duplicate, got %d", store.Len())
	}
}

func TestStoreUnsubscribe(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	store.Subscribe("https://example.com/feed.xml")

	// Unsubscribe should succeed
	if !store.Unsubscribe("https://example.com/feed.xml") {
		t.Error("Unsubscribe should return true")
	}
	if store.Len() != 0 {
		t.Errorf("expected 0 feeds, got %d", store.Len())
	}

	// Unsubscribe non-existent should fail
	if store.Unsubscribe("https://example.com/nonexistent.xml") {
		t.Error("Unsubscribe should return false for non-existent")
	}
}

func TestStoreReadTracking(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	guid := "test-guid-123"

	// Initially unread
	if store.IsRead(guid) {
		t.Error("expected initially unread")
	}

	// Mark read
	store.MarkRead(guid)
	if !store.IsRead(guid) {
		t.Error("expected read after MarkRead")
	}

	// Mark unread
	store.MarkUnread(guid)
	if store.IsRead(guid) {
		t.Error("expected unread after MarkUnread")
	}
}

func TestStoreUnreadCount(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	feedURL := "https://example.com/feed.xml"
	store.Subscribe(feedURL)

	// Add items
	store.Items[feedURL] = []FeedItem{
		{GUID: "item-1", FeedURL: feedURL, Title: "Item 1"},
		{GUID: "item-2", FeedURL: feedURL, Title: "Item 2"},
		{GUID: "item-3", FeedURL: feedURL, Title: "Item 3"},
	}

	// All unread initially
	if count := store.UnreadCount(feedURL); count != 3 {
		t.Errorf("expected 3 unread, got %d", count)
	}
	if count := store.AllUnreadCount(); count != 3 {
		t.Errorf("expected 3 total unread, got %d", count)
	}

	// Mark one read
	store.MarkRead("item-1")
	if count := store.UnreadCount(feedURL); count != 2 {
		t.Errorf("expected 2 unread, got %d", count)
	}

	// Mark all read
	store.MarkFeedRead(feedURL)
	if count := store.UnreadCount(feedURL); count != 0 {
		t.Errorf("expected 0 unread, got %d", count)
	}
}

func TestStoreUpdateFeed(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	feedURL := "https://example.com/feed.xml"
	store.Subscribe(feedURL)

	// First update
	parsed := &ParsedFeed{
		Title: "Example Feed",
		Link:  "https://example.com",
		Items: []ParsedItem{
			{GUID: "item-1", Title: "Item 1", Published: time.Now()},
			{GUID: "item-2", Title: "Item 2", Published: time.Now().Add(-time.Hour)},
		},
	}

	newCount := store.UpdateFeed(feedURL, parsed, 100)
	if newCount != 2 {
		t.Errorf("expected 2 new items, got %d", newCount)
	}

	// Verify feed metadata updated
	feed := store.GetFeed(feedURL)
	if feed.Title != "Example Feed" {
		t.Errorf("expected title 'Example Feed', got %q", feed.Title)
	}

	// Second update with one new item
	parsed.Items = []ParsedItem{
		{GUID: "item-1", Title: "Item 1", Published: time.Now()},
		{GUID: "item-2", Title: "Item 2", Published: time.Now().Add(-time.Hour)},
		{GUID: "item-3", Title: "Item 3", Published: time.Now().Add(-2 * time.Hour)},
	}

	newCount = store.UpdateFeed(feedURL, parsed, 100)
	if newCount != 1 {
		t.Errorf("expected 1 new item, got %d", newCount)
	}

	items := store.GetItems(feedURL)
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestStoreUpdateFeedMaxItems(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	feedURL := "https://example.com/feed.xml"
	store.Subscribe(feedURL)

	// Add 5 items with limit of 3
	parsed := &ParsedFeed{
		Title: "Example Feed",
		Items: []ParsedItem{
			{GUID: "item-1", Title: "Item 1", Published: time.Now()},
			{GUID: "item-2", Title: "Item 2", Published: time.Now().Add(-time.Hour)},
			{GUID: "item-3", Title: "Item 3", Published: time.Now().Add(-2 * time.Hour)},
			{GUID: "item-4", Title: "Item 4", Published: time.Now().Add(-3 * time.Hour)},
			{GUID: "item-5", Title: "Item 5", Published: time.Now().Add(-4 * time.Hour)},
		},
	}

	store.UpdateFeed(feedURL, parsed, 3)

	items := store.GetItems(feedURL)
	if len(items) != 3 {
		t.Errorf("expected 3 items (limited), got %d", len(items))
	}

	// Should keep the newest items
	if items[0].GUID != "item-1" {
		t.Errorf("expected newest item first, got %q", items[0].GUID)
	}
}

func TestStorePersistence(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "rss-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "rss.json")

	// Create and populate store
	store := &Store{
		path:      path,
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	store.Subscribe("https://example.com/feed.xml")
	store.Items["https://example.com/feed.xml"] = []FeedItem{
		{GUID: "item-1", Title: "Test Item"},
	}
	store.MarkRead("item-1")

	// Save
	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load into new store
	data, _ := os.ReadFile(path)
	store2 := &Store{
		path:      path,
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	if err := store2.load(data); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if store2.Len() != 1 {
		t.Errorf("expected 1 feed after load, got %d", store2.Len())
	}
	if !store2.IsRead("item-1") {
		t.Error("expected item-1 to be read after load")
	}
}

// Helper for testing persistence
func (s *Store) load(data []byte) error {
	return json.Unmarshal(data, s)
}

func TestGetAllItems(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	// Add items from two feeds
	store.Items["feed1"] = []FeedItem{
		{GUID: "f1-1", Title: "Feed1 Item1", Published: time.Now()},
		{GUID: "f1-2", Title: "Feed1 Item2", Published: time.Now().Add(-2 * time.Hour)},
	}
	store.Items["feed2"] = []FeedItem{
		{GUID: "f2-1", Title: "Feed2 Item1", Published: time.Now().Add(-time.Hour)},
	}

	all := store.GetAllItems()
	if len(all) != 3 {
		t.Errorf("expected 3 items, got %d", len(all))
	}

	// Should be sorted by date descending
	if all[0].GUID != "f1-1" {
		t.Errorf("expected newest item first, got %q", all[0].GUID)
	}
	if all[1].GUID != "f2-1" {
		t.Errorf("expected second newest item second, got %q", all[1].GUID)
	}
}

func TestGetUnreadItems(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	store.Items["feed1"] = []FeedItem{
		{GUID: "item-1", Title: "Item 1", Published: time.Now()},
		{GUID: "item-2", Title: "Item 2", Published: time.Now().Add(-time.Hour)},
		{GUID: "item-3", Title: "Item 3", Published: time.Now().Add(-2 * time.Hour)},
	}

	store.MarkRead("item-2")

	unread := store.GetUnreadItems()
	if len(unread) != 2 {
		t.Errorf("expected 2 unread items, got %d", len(unread))
	}

	// Check that item-2 is not in the list
	for _, item := range unread {
		if item.GUID == "item-2" {
			t.Error("item-2 should not be in unread list")
		}
	}
}
