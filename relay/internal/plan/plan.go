// Package plan provides subscription plan management services.
package plan

import (
	"context"
	"database/sql"

	"go_hyperliquid/relay/internal/models"

	"github.com/pkg/errors"
)

// Service provides plan management operations.
type Service struct {
	db    *sql.DB
	cache map[int64]*models.Plan // Simple in-memory cache
}

// NewService creates a new plan service.
func NewService(db *sql.DB) *Service {
	return &Service{
		db:    db,
		cache: make(map[int64]*models.Plan),
	}
}

// Create creates a new plan.
func (s *Service) Create(ctx context.Context, plan *models.Plan) (*models.Plan, error) {
	query := `INSERT INTO plans (name, description, max_concurrent_streams, max_rps, 
	                             max_symbols, max_daily_requests, monthly_price, status)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	result, err := s.db.ExecContext(ctx, query,
		plan.Name, plan.Description, plan.MaxConcurrentStreams, plan.MaxRPS,
		plan.MaxSymbols, plan.MaxDailyRequests, plan.MonthlyPrice, plan.Status,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create plan")
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get last insert id")
	}

	return s.GetByID(ctx, id)
}

// GetByID retrieves a plan by ID.
func (s *Service) GetByID(ctx context.Context, id int64) (*models.Plan, error) {
	// Check cache first
	if plan, ok := s.cache[id]; ok {
		return plan, nil
	}

	query := `SELECT id, name, description, max_concurrent_streams, max_rps, 
	                 max_symbols, max_daily_requests, monthly_price, status, created_at
	          FROM plans WHERE id = ?`

	var plan models.Plan
	var description sql.NullString
	var maxDailyRequests sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&plan.ID, &plan.Name, &description, &plan.MaxConcurrentStreams, &plan.MaxRPS,
		&plan.MaxSymbols, &maxDailyRequests, &plan.MonthlyPrice, &plan.Status, &plan.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlanNotFound
		}
		return nil, errors.Wrap(err, "failed to get plan")
	}

	if description.Valid {
		plan.Description = description.String
	}
	if maxDailyRequests.Valid {
		plan.MaxDailyRequests = &maxDailyRequests.Int64
	}

	// Update cache
	s.cache[id] = &plan

	return &plan, nil
}

// GetByName retrieves a plan by name.
func (s *Service) GetByName(ctx context.Context, name string) (*models.Plan, error) {
	query := `SELECT id, name, description, max_concurrent_streams, max_rps, 
	                 max_symbols, max_daily_requests, monthly_price, status, created_at
	          FROM plans WHERE name = ?`

	var plan models.Plan
	var description sql.NullString
	var maxDailyRequests sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, name).Scan(
		&plan.ID, &plan.Name, &description, &plan.MaxConcurrentStreams, &plan.MaxRPS,
		&plan.MaxSymbols, &maxDailyRequests, &plan.MonthlyPrice, &plan.Status, &plan.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPlanNotFound
		}
		return nil, errors.Wrap(err, "failed to get plan")
	}

	if description.Valid {
		plan.Description = description.String
	}
	if maxDailyRequests.Valid {
		plan.MaxDailyRequests = &maxDailyRequests.Int64
	}

	return &plan, nil
}

// List retrieves all active plans.
func (s *Service) List(ctx context.Context) ([]*models.Plan, error) {
	query := `SELECT id, name, description, max_concurrent_streams, max_rps, 
	                 max_symbols, max_daily_requests, monthly_price, status, created_at
	          FROM plans WHERE status = 'active' ORDER BY monthly_price ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list plans")
	}
	defer rows.Close()

	var plans []*models.Plan
	for rows.Next() {
		var plan models.Plan
		var description sql.NullString
		var maxDailyRequests sql.NullInt64

		if err := rows.Scan(
			&plan.ID, &plan.Name, &description, &plan.MaxConcurrentStreams, &plan.MaxRPS,
			&plan.MaxSymbols, &maxDailyRequests, &plan.MonthlyPrice, &plan.Status, &plan.CreatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan plan")
		}

		if description.Valid {
			plan.Description = description.String
		}
		if maxDailyRequests.Valid {
			plan.MaxDailyRequests = &maxDailyRequests.Int64
		}

		plans = append(plans, &plan)
	}

	return plans, nil
}

// Update updates a plan.
func (s *Service) Update(ctx context.Context, plan *models.Plan) error {
	query := `UPDATE plans SET 
	          name = ?, description = ?, max_concurrent_streams = ?, max_rps = ?,
	          max_symbols = ?, max_daily_requests = ?, monthly_price = ?, status = ?
	          WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query,
		plan.Name, plan.Description, plan.MaxConcurrentStreams, plan.MaxRPS,
		plan.MaxSymbols, plan.MaxDailyRequests, plan.MonthlyPrice, plan.Status, plan.ID,
	)
	if err != nil {
		return errors.Wrap(err, "failed to update plan")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return ErrPlanNotFound
	}

	// Invalidate cache
	delete(s.cache, plan.ID)

	return nil
}

// Deprecate marks a plan as deprecated.
func (s *Service) Deprecate(ctx context.Context, id int64) error {
	query := `UPDATE plans SET status = 'deprecated' WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "failed to deprecate plan")
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "failed to get rows affected")
	}
	if rows == 0 {
		return ErrPlanNotFound
	}

	// Invalidate cache
	delete(s.cache, id)

	return nil
}

// GetDefaultPlan returns the default (free) plan.
func (s *Service) GetDefaultPlan(ctx context.Context) (*models.Plan, error) {
	return s.GetByName(ctx, "free")
}

// ClearCache clears the plan cache.
func (s *Service) ClearCache() {
	s.cache = make(map[int64]*models.Plan)
}

// Error definitions
var (
	ErrPlanNotFound = errors.New("plan not found")
)
