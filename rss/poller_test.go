package rss

import (
	"testing"
	"time"
)

func TestPollerStartStop(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	poller := NewPoller(store, time.Hour) // Long interval, won't trigger during test

	// Start should not panic
	poller.Start()

	// Give it a moment
	time.Sleep(10 * time.Millisecond)

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		poller.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("poller.Stop() timed out")
	}
}

func TestPollerRefreshNow(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	poller := NewPoller(store, time.Hour)
	poller.Start()

	// RefreshNow should not panic even with no feeds
	poller.RefreshNow()

	// Give it time to process
	time.Sleep(50 * time.Millisecond)

	poller.Stop()
}

func TestPollerConcurrency(t *testing.T) {
	store := &Store{
		Items:     make(map[string][]FeedItem),
		ReadGUIDs: make(map[string]time.Time),
	}

	poller := NewPoller(store, time.Hour)

	// Test that concurrency is set
	if poller.concurrency != 3 {
		t.Errorf("expected concurrency 3, got %d", poller.concurrency)
	}

	// Test SetMaxItems
	poller.SetMaxItems(50)
	if poller.maxItems != 50 {
		t.Errorf("expected maxItems 50, got %d", poller.maxItems)
	}
}
