// Package session handles saving and restoring browser session state.
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// PageState represents a single page in history.
type PageState struct {
	URL     string `json:"url"`
	ScrollY int    `json:"scrollY"`
}

// Buffer represents a browser tab with its history.
type Buffer struct {
	History []PageState `json:"history"` // back stack
	Current PageState   `json:"current"`
	Forward []PageState `json:"forward"` // forward stack
}

// Session represents the complete browser session state.
type Session struct {
	Buffers          []Buffer `json:"buffers"`
	CurrentBufferIdx int      `json:"currentBufferIdx"`
	SearchHistory    []string `json:"searchHistory"`
}

// Path returns the session file path.
func Path() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "browse", "session.json"), nil
}

// Load reads the session from disk.
func Load() (*Session, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	return &s, nil
}

// Save writes the session to disk.
func Save(s *Session) error {
	path, err := Path()
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// Clear removes the session file.
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return os.Remove(path)
}
