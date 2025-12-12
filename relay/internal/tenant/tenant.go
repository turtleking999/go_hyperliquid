// Package tenant provides tenant management services.
package tenant

import (
	"context"
	"database/sql"
	"time"

	"go_hyperliquid/relay/internal/models"

	"github.com/pkg/errors"
)

// Service provides tenant management operations.
type Service struct {
	db *sql.DB
}

// NewService creates a new tenant service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Create creates a new tenant.
func (s *Service) Create(ctx context.Context, name, email string) (*models.Tenant, error) {
	query := `INSERT INTO tenants (name, email, status) VALUES (?, ?, 'active')`
	result, err := s.db.ExecContext(ctx, query, name, email)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create tenant")
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get last insert id")
	}

	return s.GetByID(ctx, id)
}

// GetByID retrieves a tenant by ID.
func (s *Service) GetByID(ctx context.Context, id int64) (*models.Tenant, error) {
	query := `SELECT id, name, email, status, metadata, created_at, updated_at 
	          FROM tenants WHERE id = ?`

	var tenant models.Tenant
	var metadata sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&tenant.ID, &tenant.Name, &tenant.Email, &tenant.Status,
		&metadata, &tenant.CreatedAt, &tenant.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantNotFound
		}
		return nil, errors.Wrap(err, "failed to get tenant")
	}

	if metadata.Valid {
		tenant.Metadata = []byte(metadata.String)
	}

	return &tenant, nil
}

// GetByEmail retrieves a tenant by email.
func (s *Service) GetByEmail(ctx context.Context, email string) (*models.Tenant, error) {
	query := `SELECT id, name, email, status, metadata, created_at, updated_at 
	          FROM tenants WHERE email = ?`

	var tenant models.Tenant
	var metadata sql.NullString
	err := s.db.QueryRowContext(ctx, query, email).Scan(
		&tenant.ID, &tenant.Name, &tenant.Email, &tenant.Status,
		&metadata, &tenant.CreatedAt, &tenant.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantNotFound
		}
		return nil, errors.Wrap(err, "failed to get tenant")
	}

	if metadata.Valid {
		tenant.Metadata = []byte(metadata.String)
	}

	return &tenant, nil
}

// List retrieves all tenants with pagination.
func (s *Service) List(ctx context.Context, offset, limit int) ([]*models.Tenant, error) {
	query := `SELECT id, name, email, status, metadata, created_at, updated_at 
	          FROM tenants ORDER BY id LIMIT ? OFFSET ?`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list tenants")
	}
	defer rows.Close()

	var tenants []*models.Tenant
	for rows.Next() {
		var tenant models.Tenant
		var metadata sql.NullString
		if err := rows.Scan(
			&tenant.ID, &tenant.Name, &tenant.Email, &tenant.Status,
			&metadata, &tenant.CreatedAt, &tenant.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan tenant")
		}
		if metadata.Valid {
			tenant.Metadata = []byte(metadata.String)
		}
		tenants = append(tenants, &tenant)
	}

	return tenants, nil
}

// Update updates a tenant's information.
func (s *Service) Update(ctx context.Context, id int64, name, email string) error {
	query := `UPDATE tenants SET name = ?, email = ?, updated_at = NOW() WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, name, email, id)
	if err != nil {
		return errors.Wrap(err, "failed to update tenant")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return ErrTenantNotFound
	}

	return nil
}

// UpdateStatus updates a tenant's status.
func (s *Service) UpdateStatus(ctx context.Context, id int64, status models.TenantStatus) error {
	query := `UPDATE tenants SET status = ?, updated_at = NOW() WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return errors.Wrap(err, "failed to update tenant status")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return ErrTenantNotFound
	}

	return nil
}

// Suspend suspends a tenant.
func (s *Service) Suspend(ctx context.Context, id int64) error {
	return s.UpdateStatus(ctx, id, models.TenantStatusSuspended)
}

// Activate activates a tenant.
func (s *Service) Activate(ctx context.Context, id int64) error {
	return s.UpdateStatus(ctx, id, models.TenantStatusActive)
}

// Delete soft-deletes a tenant.
func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.UpdateStatus(ctx, id, models.TenantStatusDeleted)
}

// Count returns the total number of tenants.
func (s *Service) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tenants WHERE status != 'deleted'").Scan(&count)
	if err != nil {
		return 0, errors.Wrap(err, "failed to count tenants")
	}
	return count, nil
}

// GetAPIKeys retrieves all API keys for a tenant.
func (s *Service) GetAPIKeys(ctx context.Context, tenantID int64) ([]*models.APIKey, error) {
	query := `SELECT id, tenant_id, plan_id, key_prefix, key_hash, name, status, 
	                 permissions, expires_at, created_at, last_used_at
	          FROM api_keys WHERE tenant_id = ? ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get api keys")
	}
	defer rows.Close()

	var keys []*models.APIKey
	for rows.Next() {
		var key models.APIKey
		var name, permissions sql.NullString
		var expiresAt, lastUsedAt sql.NullTime

		if err := rows.Scan(
			&key.ID, &key.TenantID, &key.PlanID, &key.KeyPrefix, &key.KeyHash,
			&name, &key.Status, &permissions, &expiresAt, &key.CreatedAt, &lastUsedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan api key")
		}

		if name.Valid {
			key.Name = name.String
		}
		if permissions.Valid {
			key.Permissions = []byte(permissions.String)
		}
		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		if lastUsedAt.Valid {
			key.LastUsedAt = &lastUsedAt.Time
		}

		keys = append(keys, &key)
	}

	return keys, nil
}

// Stats returns tenant statistics.
type Stats struct {
	TotalTenants    int64
	ActiveTenants   int64
	SuspendedTenants int64
	LastCreatedAt   *time.Time
}

// GetStats returns tenant statistics.
func (s *Service) GetStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{}

	// Total count
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tenants WHERE status != 'deleted'").Scan(&stats.TotalTenants); err != nil {
		return nil, err
	}

	// Active count
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tenants WHERE status = 'active'").Scan(&stats.ActiveTenants); err != nil {
		return nil, err
	}

	// Suspended count
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tenants WHERE status = 'suspended'").Scan(&stats.SuspendedTenants); err != nil {
		return nil, err
	}

	// Last created
	var lastCreated sql.NullTime
	if err := s.db.QueryRowContext(ctx,
		"SELECT MAX(created_at) FROM tenants").Scan(&lastCreated); err != nil {
		return nil, err
	}
	if lastCreated.Valid {
		stats.LastCreatedAt = &lastCreated.Time
	}

	return stats, nil
}

// Error definitions
var (
	ErrTenantNotFound = errors.New("tenant not found")
)
