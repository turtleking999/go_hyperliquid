// Package ratelimit provides rate limiting using token bucket algorithm.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter provides per-key rate limiting.
type Limiter struct {
	limiters    map[string]*ClientLimiter
	mu          sync.RWMutex
	defaultRPS  int
	defaultBurst int
	cleanupInterval time.Duration
}

// ClientLimiter holds rate limiters for a single client/API key.
type ClientLimiter struct {
	RPS         *rate.Limiter // Requests per second
	Streams     *StreamLimiter
	LastAccess  time.Time
}

// StreamLimiter limits concurrent streams per client.
type StreamLimiter struct {
	maxStreams    int
	activeStreams int
	mu            sync.Mutex
}

// NewStreamLimiter creates a new stream limiter.
func NewStreamLimiter(maxStreams int) *StreamLimiter {
	return &StreamLimiter{
		maxStreams: maxStreams,
	}
}

// Acquire tries to acquire a stream slot.
func (sl *StreamLimiter) Acquire() bool {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.activeStreams >= sl.maxStreams {
		return false
	}
	sl.activeStreams++
	return true
}

// Release releases a stream slot.
func (sl *StreamLimiter) Release() {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.activeStreams > 0 {
		sl.activeStreams--
	}
}

// ActiveCount returns the number of active streams.
func (sl *StreamLimiter) ActiveCount() int {
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.activeStreams
}

// Config holds rate limiter configuration.
type Config struct {
	DefaultRPS      int           `mapstructure:"default_rps"`
	DefaultMaxStreams int         `mapstructure:"default_max_streams"`
	BurstMultiplier float64       `mapstructure:"burst_multiplier"`
	CleanupInterval time.Duration `mapstructure:"cleanup_interval"`
}

// NewLimiter creates a new rate limiter.
func NewLimiter(cfg *Config) *Limiter {
	if cfg.BurstMultiplier < 1 {
		cfg.BurstMultiplier = 2.0
	}
	if cfg.CleanupInterval == 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}

	l := &Limiter{
		limiters:        make(map[string]*ClientLimiter),
		defaultRPS:      cfg.DefaultRPS,
		defaultBurst:    int(float64(cfg.DefaultRPS) * cfg.BurstMultiplier),
		cleanupInterval: cfg.CleanupInterval,
	}

	// Start cleanup goroutine
	go l.cleanupLoop()

	return l
}

// Allow checks if a request is allowed for the given key.
func (l *Limiter) Allow(key string) bool {
	limiter := l.getOrCreate(key, l.defaultRPS, l.defaultRPS)
	limiter.LastAccess = time.Now()
	return limiter.RPS.Allow()
}

// AllowN checks if N requests are allowed for the given key.
func (l *Limiter) AllowN(key string, n int) bool {
	limiter := l.getOrCreate(key, l.defaultRPS, l.defaultRPS)
	limiter.LastAccess = time.Now()
	return limiter.RPS.AllowN(time.Now(), n)
}

// Wait blocks until a request is allowed or context is cancelled.
func (l *Limiter) Wait(ctx context.Context, key string) error {
	limiter := l.getOrCreate(key, l.defaultRPS, l.defaultRPS)
	limiter.LastAccess = time.Now()
	return limiter.RPS.Wait(ctx)
}

// AcquireStream tries to acquire a stream slot for the given key.
func (l *Limiter) AcquireStream(key string, maxStreams int) bool {
	limiter := l.getOrCreate(key, l.defaultRPS, maxStreams)
	limiter.LastAccess = time.Now()
	return limiter.Streams.Acquire()
}

// ReleaseStream releases a stream slot for the given key.
func (l *Limiter) ReleaseStream(key string) {
	l.mu.RLock()
	limiter, ok := l.limiters[key]
	l.mu.RUnlock()

	if ok && limiter.Streams != nil {
		limiter.Streams.Release()
	}
}

// SetLimit sets custom limits for a key.
func (l *Limiter) SetLimit(key string, rps int, maxStreams int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, ok := l.limiters[key]
	if ok {
		limiter.RPS.SetLimit(rate.Limit(rps))
		limiter.RPS.SetBurst(int(float64(rps) * 2))
		limiter.Streams = NewStreamLimiter(maxStreams)
	} else {
		l.limiters[key] = &ClientLimiter{
			RPS:        rate.NewLimiter(rate.Limit(rps), int(float64(rps)*2)),
			Streams:    NewStreamLimiter(maxStreams),
			LastAccess: time.Now(),
		}
	}
}

// GetStats returns rate limiter statistics for a key.
type KeyStats struct {
	RPS           float64
	Burst         int
	ActiveStreams int
	MaxStreams    int
}

// GetKeyStats returns statistics for a specific key.
func (l *Limiter) GetKeyStats(key string) (KeyStats, bool) {
	l.mu.RLock()
	limiter, ok := l.limiters[key]
	l.mu.RUnlock()

	if !ok {
		return KeyStats{}, false
	}

	return KeyStats{
		RPS:           float64(limiter.RPS.Limit()),
		Burst:         limiter.RPS.Burst(),
		ActiveStreams: limiter.Streams.ActiveCount(),
		MaxStreams:    limiter.Streams.maxStreams,
	}, true
}

// getOrCreate gets or creates a limiter for a key.
func (l *Limiter) getOrCreate(key string, rps, maxStreams int) *ClientLimiter {
	l.mu.RLock()
	limiter, ok := l.limiters[key]
	l.mu.RUnlock()

	if ok {
		return limiter
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, ok = l.limiters[key]; ok {
		return limiter
	}

	limiter = &ClientLimiter{
		RPS:        rate.NewLimiter(rate.Limit(rps), l.defaultBurst),
		Streams:    NewStreamLimiter(maxStreams),
		LastAccess: time.Now(),
	}
	l.limiters[key] = limiter
	return limiter
}

// cleanupLoop periodically removes inactive limiters.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		l.cleanup()
	}
}

// cleanup removes limiters that haven't been accessed recently.
func (l *Limiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	threshold := time.Now().Add(-l.cleanupInterval * 2)
	for key, limiter := range l.limiters {
		if limiter.LastAccess.Before(threshold) && limiter.Streams.ActiveCount() == 0 {
			delete(l.limiters, key)
		}
	}
}

// Stats returns overall rate limiter statistics.
type Stats struct {
	TotalKeys     int
	TotalStreams  int
}

// GetStats returns overall statistics.
func (l *Limiter) GetStats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	totalStreams := 0
	for _, limiter := range l.limiters {
		totalStreams += limiter.Streams.ActiveCount()
	}

	return Stats{
		TotalKeys:    len(l.limiters),
		TotalStreams: totalStreams,
	}
}
