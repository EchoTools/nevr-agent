package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// PlayerInfo represents player information from the echovrce API
type PlayerInfo struct {
	ID          string    `json:"id"`
	DiscordID   string    `json:"discord_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url"`
	FetchedAt   time.Time `json:"-"`
}

// PlayerLookupService handles player information lookup with caching
type PlayerLookupService struct {
	baseURL     string
	httpClient  *http.Client
	cache       map[string]*PlayerInfo
	cacheMu     sync.RWMutex
	cacheTTL    time.Duration
	logger      Logger
	metrics     *Metrics
	rateLimiter *rateLimiter
}

// rateLimiter implements a simple token bucket rate limiter
type rateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

func newRateLimiter(maxTokens float64, refillRate float64) *rateLimiter {
	return &rateLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

func (r *rateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.tokens = min(r.maxTokens, r.tokens+elapsed*r.refillRate)
	r.lastRefill = now

	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

// PlayerLookupConfig holds configuration for the player lookup service
type PlayerLookupConfig struct {
	BaseURL        string        // Base URL for the player lookup API
	CacheTTL       time.Duration // How long to cache player info
	MaxRPS         float64       // Maximum requests per second
	BurstSize      float64       // Maximum burst size for rate limiting
	RequestTimeout time.Duration // Timeout for API requests
}

// DefaultPlayerLookupConfig returns a default configuration
func DefaultPlayerLookupConfig() *PlayerLookupConfig {
	return &PlayerLookupConfig{
		BaseURL:        "https://g.echovrce.com",
		CacheTTL:       1 * time.Hour,
		MaxRPS:         5,
		BurstSize:      10,
		RequestTimeout: 5 * time.Second,
	}
}

// NewPlayerLookupService creates a new player lookup service
func NewPlayerLookupService(config *PlayerLookupConfig, logger Logger, metrics *Metrics) *PlayerLookupService {
	if config == nil {
		config = DefaultPlayerLookupConfig()
	}

	return &PlayerLookupService{
		baseURL: config.BaseURL,
		httpClient: &http.Client{
			Timeout: config.RequestTimeout,
		},
		cache:       make(map[string]*PlayerInfo),
		cacheTTL:    config.CacheTTL,
		logger:      logger,
		metrics:     metrics,
		rateLimiter: newRateLimiter(config.BurstSize, config.MaxRPS),
	}
}

// Lookup looks up player information by XP ID
func (s *PlayerLookupService) Lookup(ctx context.Context, xpID string) (*PlayerInfo, error) {
	// Check cache first
	s.cacheMu.RLock()
	if cached, ok := s.cache[xpID]; ok && time.Since(cached.FetchedAt) < s.cacheTTL {
		s.cacheMu.RUnlock()
		return cached, nil
	}
	s.cacheMu.RUnlock()

	// Check rate limiter
	if !s.rateLimiter.Allow() {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// Perform lookup
	start := time.Now()
	info, err := s.fetchPlayerInfo(ctx, xpID)
	duration := time.Since(start)

	if s.metrics != nil {
		s.metrics.RecordPlayerLookup(duration, err)
	}

	if err != nil {
		return nil, err
	}

	// Cache the result
	s.cacheMu.Lock()
	s.cache[xpID] = info
	s.cacheMu.Unlock()

	return info, nil
}

// fetchPlayerInfo performs the actual API call
func (s *PlayerLookupService) fetchPlayerInfo(ctx context.Context, xpID string) (*PlayerInfo, error) {
	u, err := url.Parse(s.baseURL + "/account/lookup")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("xp_id", xpID)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "evrtelemetry/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("player not found: %s", xpID)
		}
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var info PlayerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	info.FetchedAt = time.Now()
	return &info, nil
}

// LookupBatch looks up multiple players concurrently
func (s *PlayerLookupService) LookupBatch(ctx context.Context, xpIDs []string) map[string]*PlayerInfo {
	results := make(map[string]*PlayerInfo)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, xpID := range xpIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			info, err := s.Lookup(ctx, id)
			if err != nil {
				s.logger.Debug("failed to lookup player", "xp_id", id, "error", err)
				return
			}

			mu.Lock()
			results[id] = info
			mu.Unlock()
		}(xpID)
	}

	wg.Wait()
	return results
}

// CleanupCache removes expired entries from the cache
func (s *PlayerLookupService) CleanupCache() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	now := time.Now()
	for xpID, info := range s.cache {
		if now.Sub(info.FetchedAt) > s.cacheTTL {
			delete(s.cache, xpID)
		}
	}
}

// StartCacheCleanup starts a background goroutine to clean up the cache periodically
func (s *PlayerLookupService) StartCacheCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CleanupCache()
			}
		}
	}()
}

// CacheStats returns cache statistics
func (s *PlayerLookupService) CacheStats() (size int, hitRate float64) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	return len(s.cache), 0 // TODO: track hit rate
}

// min returns the minimum of two float64 values
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
