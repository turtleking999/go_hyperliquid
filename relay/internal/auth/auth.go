// Package auth provides authentication and authorization services.
package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"sync"
	"time"

	"go_hyperliquid/relay/internal/models"

	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"github.com/redis/go-redis/v9"
)

// Service provides authentication and authorization.
type Service struct {
	db          *sql.DB
	redis       *redis.Client
	cache       map[string]*models.AuthContext
	cacheMu     sync.RWMutex
	cacheTTL    time.Duration
}

// Config holds auth service configuration.
type Config struct {
	CacheTTL time.Duration
}

// NewService creates a new auth service.
func NewService(db *sql.DB, redisClient *redis.Client, cfg *Config) *Service {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}

	s := &Service{
		db:       db,
		redis:    redisClient,
		cache:    make(map[string]*models.AuthContext),
		cacheTTL: cfg.CacheTTL,
	}

	// Start cache cleanup goroutine
	go s.cleanupLoop()

	return s
}

// Authenticate validates an API key and returns the auth context.
func (s *Service) Authenticate(ctx context.Context, apiKey string) (*models.AuthContext, error) {
	if apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	// Hash the API key
	keyHash := hashAPIKey(apiKey)

	// Check in-memory cache first
	if authCtx := s.getFromCache(keyHash); authCtx != nil {
		if authCtx.IsValid() {
			return authCtx, nil
		}
		// Invalid, remove from cache
		s.removeFromCache(keyHash)
	}

	// Check Redis cache if available
	if s.redis != nil {
		if authCtx, err := s.getFromRedis(ctx, keyHash); err == nil && authCtx != nil {
			if authCtx.IsValid() {
				s.putToCache(keyHash, authCtx)
				return authCtx, nil
			}
		}
	}

	// Query database
	authCtx, err := s.queryAuthContext(ctx, keyHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidAPIKey
		}
		return nil, errors.Wrap(err, "failed to query auth context")
	}

	// Validate the auth context
	if !authCtx.IsValid() {
		if authCtx.TenantStatus == models.TenantStatusSuspended {
			return nil, ErrSuspendedTenant
		}
		if authCtx.APIKeyStatus == models.APIKeyStatusRevoked {
			return nil, ErrRevokedAPIKey
		}
		if authCtx.APIKeyStatus == models.APIKeyStatusExpired || 
		   (authCtx.ExpiresAt != nil && authCtx.ExpiresAt.Before(time.Now())) {
			return nil, ErrExpiredAPIKey
		}
		return nil, ErrInvalidAPIKey
	}

	// Cache the result
	authCtx.CachedAt = time.Now()
	s.putToCache(keyHash, authCtx)
	if s.redis != nil {
		s.putToRedis(ctx, keyHash, authCtx)
	}

	// Update last_used_at asynchronously
	go s.updateLastUsed(context.Background(), authCtx.APIKeyID)

	return authCtx, nil
}

// InvalidateCache removes an API key from all caches.
func (s *Service) InvalidateCache(apiKey string) {
	keyHash := hashAPIKey(apiKey)
	s.removeFromCache(keyHash)
	if s.redis != nil {
		s.redis.Del(context.Background(), "auth:"+keyHash)
	}
}

// queryAuthContext queries the database for auth context.
func (s *Service) queryAuthContext(ctx context.Context, keyHash string) (*models.AuthContext, error) {
	query := `
		SELECT 
			t.id, t.name, t.status,
			ak.id, ak.status, ak.expires_at,
			p.id, p.name, p.max_concurrent_streams, p.max_rps, p.max_symbols, p.max_daily_requests
		FROM api_keys ak
		JOIN tenants t ON ak.tenant_id = t.id
		JOIN plans p ON ak.plan_id = p.id
		WHERE ak.key_hash = ?
	`

	var authCtx models.AuthContext
	var tenantStatus, apiKeyStatus string
	var expiresAt sql.NullTime
	var maxDailyRequests sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, keyHash).Scan(
		&authCtx.TenantID, &authCtx.TenantName, &tenantStatus,
		&authCtx.APIKeyID, &apiKeyStatus, &expiresAt,
		&authCtx.PlanID, &authCtx.PlanName, &authCtx.MaxConcurrentStreams,
		&authCtx.MaxRPS, &authCtx.MaxSymbols, &maxDailyRequests,
	)
	if err != nil {
		return nil, err
	}

	authCtx.TenantStatus = models.TenantStatus(tenantStatus)
	authCtx.APIKeyStatus = models.APIKeyStatus(apiKeyStatus)
	if expiresAt.Valid {
		authCtx.ExpiresAt = &expiresAt.Time
	}
	if maxDailyRequests.Valid {
		authCtx.MaxDailyRequests = maxDailyRequests.Int64
	}

	return &authCtx, nil
}

// updateLastUsed updates the last_used_at timestamp.
func (s *Service) updateLastUsed(ctx context.Context, apiKeyID int64) {
	query := "UPDATE api_keys SET last_used_at = NOW() WHERE id = ?"
	s.db.ExecContext(ctx, query, apiKeyID)
}

// Cache methods
func (s *Service) getFromCache(keyHash string) *models.AuthContext {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	authCtx, ok := s.cache[keyHash]
	if !ok {
		return nil
	}
	if time.Since(authCtx.CachedAt) > s.cacheTTL {
		return nil // Expired
	}
	return authCtx
}

func (s *Service) putToCache(keyHash string, authCtx *models.AuthContext) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache[keyHash] = authCtx
}

func (s *Service) removeFromCache(keyHash string) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	delete(s.cache, keyHash)
}

func (s *Service) cleanupLoop() {
	ticker := time.NewTicker(s.cacheTTL)
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupCache()
	}
}

func (s *Service) cleanupCache() {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	now := time.Now()
	for key, authCtx := range s.cache {
		if now.Sub(authCtx.CachedAt) > s.cacheTTL {
			delete(s.cache, key)
		}
	}
}

// Redis methods
func (s *Service) getFromRedis(ctx context.Context, keyHash string) (*models.AuthContext, error) {
	// Simplified: In production, use proper serialization
	return nil, nil
}

func (s *Service) putToRedis(ctx context.Context, keyHash string, authCtx *models.AuthContext) {
	// Simplified: In production, use proper serialization
}

// hashAPIKey creates a SHA-256 hash of the API key.
func hashAPIKey(apiKey string) string {
	hash := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(hash[:])
}

// Error definitions
var (
	ErrMissingAPIKey   = errors.New("API key is required")
	ErrInvalidAPIKey   = errors.New("invalid API key")
	ErrExpiredAPIKey   = errors.New("API key has expired")
	ErrRevokedAPIKey   = errors.New("API key has been revoked")
	ErrSuspendedTenant = errors.New("tenant account is suspended")
)
