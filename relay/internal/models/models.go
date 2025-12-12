// Package models provides database models for the HL Relay Service.
package models

import (
	"time"
)

// TenantStatus represents the status of a tenant.
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDeleted   TenantStatus = "deleted"
)

// Tenant represents a tenant in the system.
type Tenant struct {
	ID        int64        `db:"id" json:"id"`
	Name      string       `db:"name" json:"name"`
	Email     string       `db:"email" json:"email"`
	Status    TenantStatus `db:"status" json:"status"`
	Metadata  []byte       `db:"metadata" json:"metadata,omitempty"`
	CreatedAt time.Time    `db:"created_at" json:"created_at"`
	UpdatedAt time.Time    `db:"updated_at" json:"updated_at"`
}

// PlanStatus represents the status of a plan.
type PlanStatus string

const (
	PlanStatusActive     PlanStatus = "active"
	PlanStatusDeprecated PlanStatus = "deprecated"
)

// Plan represents a subscription plan.
type Plan struct {
	ID                   int64      `db:"id" json:"id"`
	Name                 string     `db:"name" json:"name"`
	Description          string     `db:"description" json:"description"`
	MaxConcurrentStreams int        `db:"max_concurrent_streams" json:"max_concurrent_streams"`
	MaxRPS               int        `db:"max_rps" json:"max_rps"`
	MaxSymbols           int        `db:"max_symbols" json:"max_symbols"`
	MaxDailyRequests     *int64     `db:"max_daily_requests" json:"max_daily_requests,omitempty"`
	MonthlyPrice         float64    `db:"monthly_price" json:"monthly_price"`
	Status               PlanStatus `db:"status" json:"status"`
	CreatedAt            time.Time  `db:"created_at" json:"created_at"`
}

// APIKeyStatus represents the status of an API key.
type APIKeyStatus string

const (
	APIKeyStatusActive  APIKeyStatus = "active"
	APIKeyStatusRevoked APIKeyStatus = "revoked"
	APIKeyStatusExpired APIKeyStatus = "expired"
)

// APIKey represents an API key for authentication.
type APIKey struct {
	ID         int64        `db:"id" json:"id"`
	TenantID   int64        `db:"tenant_id" json:"tenant_id"`
	PlanID     int64        `db:"plan_id" json:"plan_id"`
	KeyPrefix  string       `db:"key_prefix" json:"key_prefix"`
	KeyHash    string       `db:"key_hash" json:"-"` // Never expose hash
	Name       string       `db:"name" json:"name,omitempty"`
	Status     APIKeyStatus `db:"status" json:"status"`
	Permissions []byte      `db:"permissions" json:"permissions,omitempty"`
	ExpiresAt  *time.Time   `db:"expires_at" json:"expires_at,omitempty"`
	CreatedAt  time.Time    `db:"created_at" json:"created_at"`
	LastUsedAt *time.Time   `db:"last_used_at" json:"last_used_at,omitempty"`
}

// UsageDaily represents daily usage statistics.
type UsageDaily struct {
	ID                    int64     `db:"id" json:"id"`
	TenantID              int64     `db:"tenant_id" json:"tenant_id"`
	APIKeyID              int64     `db:"api_key_id" json:"api_key_id"`
	UsageDate             time.Time `db:"usage_date" json:"usage_date"`
	TotalRequests         int64     `db:"total_requests" json:"total_requests"`
	TotalMessages         int64     `db:"total_messages" json:"total_messages"`
	PeakConcurrentStreams int       `db:"peak_concurrent_streams" json:"peak_concurrent_streams"`
	AvgLatencyMs          *float64  `db:"avg_latency_ms" json:"avg_latency_ms,omitempty"`
	ErrorCount            int64     `db:"error_count" json:"error_count"`
	CreatedAt             time.Time `db:"created_at" json:"created_at"`
	UpdatedAt             time.Time `db:"updated_at" json:"updated_at"`
}

// AuditLog represents an audit log entry.
type AuditLog struct {
	ID           int64     `db:"id" json:"id"`
	TenantID     *int64    `db:"tenant_id" json:"tenant_id,omitempty"`
	APIKeyID     *int64    `db:"api_key_id" json:"api_key_id,omitempty"`
	Action       string    `db:"action" json:"action"`
	ResourceType string    `db:"resource_type" json:"resource_type,omitempty"`
	ResourceID   string    `db:"resource_id" json:"resource_id,omitempty"`
	Details      []byte    `db:"details" json:"details,omitempty"`
	IPAddress    string    `db:"ip_address" json:"ip_address,omitempty"`
	UserAgent    string    `db:"user_agent" json:"user_agent,omitempty"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// AuthContext contains authenticated user information.
type AuthContext struct {
	TenantID             int64
	TenantName           string
	TenantStatus         TenantStatus
	APIKeyID             int64
	APIKeyStatus         APIKeyStatus
	PlanID               int64
	PlanName             string
	MaxConcurrentStreams int
	MaxRPS               int
	MaxSymbols           int
	MaxDailyRequests     int64
	Permissions          []string
	ExpiresAt            *time.Time
	CachedAt             time.Time
}

// IsValid checks if the auth context is valid for use.
func (ac *AuthContext) IsValid() bool {
	if ac.TenantStatus != TenantStatusActive {
		return false
	}
	if ac.APIKeyStatus != APIKeyStatusActive {
		return false
	}
	if ac.ExpiresAt != nil && ac.ExpiresAt.Before(time.Now()) {
		return false
	}
	return true
}
