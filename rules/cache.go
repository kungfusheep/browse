package rules

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Cache manages rule storage and retrieval.
type Cache struct {
	localDir    string
	memoryCache map[string]*Rule
}

// NewCache creates a new rule cache.
// If localDir is empty, uses ~/.config/browse/rules/
func NewCache(localDir string) (*Cache, error) {
	if localDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting home dir: %w", err)
		}
		localDir = filepath.Join(home, ".config", "browse", "rules")
	}

	// Ensure directory exists
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return nil, fmt.Errorf("creating rules dir: %w", err)
	}

	return &Cache{
		localDir:    localDir,
		memoryCache: make(map[string]*Rule),
	}, nil
}

// Get retrieves a rule for a domain.
// Checks: memory cache → local file → returns nil if not found
func (c *Cache) Get(domain string) *Rule {
	domain = normalizeDomain(domain)

	// Check memory cache first
	if rule, ok := c.memoryCache[domain]; ok {
		return rule
	}

	// Try to load from local file
	rule, err := c.loadFromFile(domain)
	if err == nil && rule != nil {
		c.memoryCache[domain] = rule
		return rule
	}

	return nil
}

// GetForURL extracts the domain from a URL and retrieves rules.
func (c *Cache) GetForURL(rawURL string) *Rule {
	domain := extractDomain(rawURL)
	if domain == "" {
		return nil
	}
	return c.Get(domain)
}

// Put stores a rule in memory and optionally to disk.
func (c *Cache) Put(rule *Rule, persist bool) error {
	domain := normalizeDomain(rule.Domain)
	c.memoryCache[domain] = rule

	if persist {
		return c.saveToFile(rule)
	}
	return nil
}

// Has checks if a rule exists for a domain.
func (c *Cache) Has(domain string) bool {
	return c.Get(domain) != nil
}

// Delete removes a rule from cache and disk.
func (c *Cache) Delete(domain string) error {
	domain = normalizeDomain(domain)
	delete(c.memoryCache, domain)

	path := c.filePath(domain)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// List returns all cached domains.
func (c *Cache) List() ([]string, error) {
	var domains []string

	// Add memory-only entries
	for domain := range c.memoryCache {
		domains = append(domains, domain)
	}

	// Scan local directory
	entries, err := os.ReadDir(c.localDir)
	if err != nil {
		if os.IsNotExist(err) {
			return domains, nil
		}
		return nil, err
	}

	seen := make(map[string]bool)
	for _, d := range domains {
		seen[d] = true
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".json") {
			domain := strings.TrimSuffix(name, ".json")
			if !seen[domain] {
				domains = append(domains, domain)
			}
		}
	}

	return domains, nil
}

// LocalDir returns the local cache directory path.
func (c *Cache) LocalDir() string {
	return c.localDir
}

func (c *Cache) filePath(domain string) string {
	// Sanitize domain for use as filename
	safe := strings.ReplaceAll(domain, "/", "_")
	safe = strings.ReplaceAll(safe, ":", "_")
	return filepath.Join(c.localDir, safe+".json")
}

func (c *Cache) loadFromFile(domain string) (*Rule, error) {
	path := c.filePath(domain)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var rule Rule
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("parsing rule file: %w", err)
	}

	return &rule, nil
}

func (c *Cache) saveToFile(rule *Rule) error {
	path := c.filePath(rule.Domain)

	data, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling rule: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing rule file: %w", err)
	}

	return nil
}

// normalizeDomain ensures consistent domain format.
func normalizeDomain(domain string) string {
	domain = strings.ToLower(domain)
	domain = strings.TrimPrefix(domain, "www.")
	return domain
}

// extractDomain gets the domain from a URL.
func extractDomain(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return normalizeDomain(parsed.Host)
}
