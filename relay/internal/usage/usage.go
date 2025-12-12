// Package usage provides usage tracking and statistics collection.
package usage

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"time"

	"go_hyperliquid/relay/internal/models"

	"github.com/pkg/errors"
)

// Collector collects and aggregates usage statistics.
type Collector struct {
	db            *sql.DB
	buffer        map[string]*UsageBuffer
	mu            sync.RWMutex
	flushInterval time.Duration
	ctx           context.Context
	cancel        context.CancelFunc
}

// UsageBuffer holds buffered usage data for a single API key.
type UsageBuffer struct {
	TenantID              int64
	APIKeyID              int64
	Requests              atomic.Int64
	Messages              atomic.Int64
	Errors                atomic.Int64
	PeakConcurrentStreams atomic.Int32
	LatencySum            atomic.Int64
	LatencyCount          atomic.Int64
}

// Config holds usage collector configuration.
type Config struct {
	FlushInterval time.Duration
}

// NewCollector creates a new usage collector.
func NewCollector(db *sql.DB, cfg *Config) *Collector {
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &Collector{
		db:            db,
		buffer:        make(map[string]*UsageBuffer),
		flushInterval: cfg.FlushInterval,
		ctx:           ctx,
		cancel:        cancel,
	}

	go c.flushLoop()

	return c
}

// RecordRequest records a request.
func (c *Collector) RecordRequest(tenantID, apiKeyID int64) {
	buf := c.getOrCreateBuffer(tenantID, apiKeyID)
	buf.Requests.Add(1)
}

// RecordMessage records a message sent.
func (c *Collector) RecordMessage(tenantID, apiKeyID int64) {
	buf := c.getOrCreateBuffer(tenantID, apiKeyID)
	buf.Messages.Add(1)
}

// RecordError records an error.
func (c *Collector) RecordError(tenantID, apiKeyID int64) {
	buf := c.getOrCreateBuffer(tenantID, apiKeyID)
	buf.Errors.Add(1)
}

// RecordLatency records a request latency in milliseconds.
func (c *Collector) RecordLatency(tenantID, apiKeyID int64, latencyMs int64) {
	buf := c.getOrCreateBuffer(tenantID, apiKeyID)
	buf.LatencySum.Add(latencyMs)
	buf.LatencyCount.Add(1)
}

// UpdatePeakStreams updates peak concurrent streams if current is higher.
func (c *Collector) UpdatePeakStreams(tenantID, apiKeyID int64, currentStreams int32) {
	buf := c.getOrCreateBuffer(tenantID, apiKeyID)
	for {
		current := buf.PeakConcurrentStreams.Load()
		if currentStreams <= current {
			break
		}
		if buf.PeakConcurrentStreams.CompareAndSwap(current, currentStreams) {
			break
		}
	}
}

func (c *Collector) getOrCreateBuffer(tenantID, apiKeyID int64) *UsageBuffer {
	key := c.bufferKey(tenantID, apiKeyID)

	c.mu.RLock()
	buf, ok := c.buffer[key]
	c.mu.RUnlock()

	if ok {
		return buf
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check
	if buf, ok = c.buffer[key]; ok {
		return buf
	}

	buf = &UsageBuffer{
		TenantID: tenantID,
		APIKeyID: apiKeyID,
	}
	c.buffer[key] = buf
	return buf
}

func (c *Collector) bufferKey(tenantID, apiKeyID int64) string {
	return string(rune(tenantID)) + ":" + string(rune(apiKeyID))
}

func (c *Collector) flushLoop() {
	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			c.Flush()
			return
		case <-ticker.C:
			c.Flush()
		}
	}
}

// Flush writes buffered data to the database.
func (c *Collector) Flush() {
	c.mu.Lock()
	buffers := c.buffer
	c.buffer = make(map[string]*UsageBuffer)
	c.mu.Unlock()

	if len(buffers) == 0 {
		return
	}

	ctx := context.Background()
	today := time.Now().Format("2006-01-02")

	for _, buf := range buffers {
		requests := buf.Requests.Swap(0)
		messages := buf.Messages.Swap(0)
		errorsCount := buf.Errors.Swap(0)
		peakStreams := buf.PeakConcurrentStreams.Swap(0)
		latencySum := buf.LatencySum.Swap(0)
		latencyCount := buf.LatencyCount.Swap(0)

		var avgLatency *float64
		if latencyCount > 0 {
			avg := float64(latencySum) / float64(latencyCount)
			avgLatency = &avg
		}

		c.upsertUsage(ctx, buf.TenantID, buf.APIKeyID, today,
			requests, messages, errorsCount, int(peakStreams), avgLatency)
	}
}

func (c *Collector) upsertUsage(ctx context.Context, tenantID, apiKeyID int64, date string,
	requests, messages, errorsCount int64, peakStreams int, avgLatency *float64) {

	query := `
		INSERT INTO usage_daily 
		(tenant_id, api_key_id, usage_date, total_requests, total_messages, 
		 peak_concurrent_streams, avg_latency_ms, error_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		total_requests = total_requests + VALUES(total_requests),
		total_messages = total_messages + VALUES(total_messages),
		error_count = error_count + VALUES(error_count),
		peak_concurrent_streams = GREATEST(peak_concurrent_streams, VALUES(peak_concurrent_streams)),
		avg_latency_ms = COALESCE(
			(avg_latency_ms * total_requests + VALUES(avg_latency_ms) * VALUES(total_requests)) / 
			(total_requests + VALUES(total_requests)),
			VALUES(avg_latency_ms)
		),
		updated_at = NOW()
	`

	_, _ = c.db.ExecContext(ctx, query,
		tenantID, apiKeyID, date, requests, messages,
		peakStreams, avgLatency, errorsCount,
	)
}

// Stop stops the collector.
func (c *Collector) Stop() {
	c.cancel()
}

// GetDailyUsage retrieves daily usage for a tenant.
func (c *Collector) GetDailyUsage(ctx context.Context, tenantID int64, date string) ([]*models.UsageDaily, error) {
	query := `SELECT id, tenant_id, api_key_id, usage_date, total_requests, 
	                 total_messages, peak_concurrent_streams, avg_latency_ms, 
	                 error_count, created_at, updated_at
	          FROM usage_daily 
	          WHERE tenant_id = ? AND usage_date = ?
	          ORDER BY api_key_id`

	rows, err := c.db.QueryContext(ctx, query, tenantID, date)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get daily usage")
	}
	defer rows.Close()

	var usages []*models.UsageDaily
	for rows.Next() {
		var usage models.UsageDaily
		var avgLatency sql.NullFloat64

		if err := rows.Scan(
			&usage.ID, &usage.TenantID, &usage.APIKeyID, &usage.UsageDate,
			&usage.TotalRequests, &usage.TotalMessages, &usage.PeakConcurrentStreams,
			&avgLatency, &usage.ErrorCount, &usage.CreatedAt, &usage.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan usage")
		}

		if avgLatency.Valid {
			usage.AvgLatencyMs = &avgLatency.Float64
		}

		usages = append(usages, &usage)
	}

	return usages, nil
}

// GetUsageRange retrieves usage for a date range.
func (c *Collector) GetUsageRange(ctx context.Context, tenantID int64, startDate, endDate string) ([]*models.UsageDaily, error) {
	query := `SELECT id, tenant_id, api_key_id, usage_date, total_requests, 
	                 total_messages, peak_concurrent_streams, avg_latency_ms, 
	                 error_count, created_at, updated_at
	          FROM usage_daily 
	          WHERE tenant_id = ? AND usage_date BETWEEN ? AND ?
	          ORDER BY usage_date, api_key_id`

	rows, err := c.db.QueryContext(ctx, query, tenantID, startDate, endDate)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get usage range")
	}
	defer rows.Close()

	var usages []*models.UsageDaily
	for rows.Next() {
		var usage models.UsageDaily
		var avgLatency sql.NullFloat64

		if err := rows.Scan(
			&usage.ID, &usage.TenantID, &usage.APIKeyID, &usage.UsageDate,
			&usage.TotalRequests, &usage.TotalMessages, &usage.PeakConcurrentStreams,
			&avgLatency, &usage.ErrorCount, &usage.CreatedAt, &usage.UpdatedAt,
		); err != nil {
			return nil, errors.Wrap(err, "failed to scan usage")
		}

		if avgLatency.Valid {
			usage.AvgLatencyMs = &avgLatency.Float64
		}

		usages = append(usages, &usage)
	}

	return usages, nil
}

// GetTotalUsage gets aggregated usage for a tenant.
type TotalUsage struct {
	TenantID        int64
	TotalRequests   int64
	TotalMessages   int64
	TotalErrors     int64
	AvgLatencyMs    float64
	PeakStreams     int
	FirstUsageDate  string
	LastUsageDate   string
}

// GetTotalUsage retrieves total aggregated usage for a tenant.
func (c *Collector) GetTotalUsage(ctx context.Context, tenantID int64) (*TotalUsage, error) {
	query := `SELECT 
	          SUM(total_requests), SUM(total_messages), SUM(error_count),
	          AVG(avg_latency_ms), MAX(peak_concurrent_streams),
	          MIN(usage_date), MAX(usage_date)
	          FROM usage_daily WHERE tenant_id = ?`

	var usage TotalUsage
	usage.TenantID = tenantID

	var totalReq, totalMsg, totalErr sql.NullInt64
	var avgLat sql.NullFloat64
	var peakStr sql.NullInt32
	var firstDate, lastDate sql.NullString

	err := c.db.QueryRowContext(ctx, query, tenantID).Scan(
		&totalReq, &totalMsg, &totalErr, &avgLat, &peakStr, &firstDate, &lastDate,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get total usage")
	}

	if totalReq.Valid {
		usage.TotalRequests = totalReq.Int64
	}
	if totalMsg.Valid {
		usage.TotalMessages = totalMsg.Int64
	}
	if totalErr.Valid {
		usage.TotalErrors = totalErr.Int64
	}
	if avgLat.Valid {
		usage.AvgLatencyMs = avgLat.Float64
	}
	if peakStr.Valid {
		usage.PeakStreams = int(peakStr.Int32)
	}
	if firstDate.Valid {
		usage.FirstUsageDate = firstDate.String
	}
	if lastDate.Valid {
		usage.LastUsageDate = lastDate.String
	}

	return &usage, nil
}
