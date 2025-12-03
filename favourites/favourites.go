// Package favourites provides persistent bookmark storage for the browse browser.
package favourites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Favourite represents a saved bookmark.
type Favourite struct {
	URL     string    `json:"url"`
	Title   string    `json:"title"`
	AddedAt time.Time `json:"added_at"`
}

// Store manages the favourites collection.
type Store struct {
	path       string
	Favourites []Favourite `json:"favourites"`
}

// configDir returns the configuration directory path.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "browse"), nil
}

// Load reads favourites from disk, creating the file if it doesn't exist.
func Load() (*Store, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}

	// Ensure config directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "favourites.json")
	store := &Store{path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// No favourites yet, return empty store
		return store, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}

	return store, nil
}

// Save writes favourites to disk.
func (s *Store) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

// Add adds a new favourite, avoiding duplicates by URL.
func (s *Store) Add(url, title string) bool {
	// Check for duplicate
	for _, f := range s.Favourites {
		if f.URL == url {
			return false // Already exists
		}
	}

	s.Favourites = append(s.Favourites, Favourite{
		URL:     url,
		Title:   title,
		AddedAt: time.Now(),
	})
	return true
}

// Remove removes a favourite by index.
func (s *Store) Remove(index int) bool {
	if index < 0 || index >= len(s.Favourites) {
		return false
	}
	s.Favourites = append(s.Favourites[:index], s.Favourites[index+1:]...)
	return true
}

// Len returns the number of favourites.
func (s *Store) Len() int {
	return len(s.Favourites)
}
