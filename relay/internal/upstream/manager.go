// Package upstream manages connections to HL Gateway.
package upstream

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go_hyperliquid/relay/internal/config"
	"go_hyperliquid/relay/pkg/types"

	"github.com/panjf2000/ants/v2"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Manager manages upstream connections to HL Gateway.
type Manager struct {
	cfg             *config.UpstreamConfig
	gateways        []config.GatewayConfig
	activeGateway   *GatewayConnection
	streams         map[string]*Stream
	streamsMu       sync.RWMutex
	pool            *ants.Pool
	logger          *zap.Logger
	onData          func(symbol string, data *types.MarketDataUpdate)
	reconnecting    atomic.Bool
	ctx             context.Context
	cancel          context.CancelFunc
}

// GatewayConnection represents a connection to a gateway.
type GatewayConnection struct {
	Endpoint     string
	Priority     int
	Region       string
	Connected    bool
	LastPing     time.Time
	ReconnectCount int
}

// Stream represents an upstream stream for a symbol.
type Stream struct {
	Symbol         string
	Status         types.StreamStatus
	LastUpdate     time.Time
	ReconnectCount int
	cancel         context.CancelFunc
}

// NewManager creates a new upstream manager.
func NewManager(cfg *config.UpstreamConfig, logger *zap.Logger, onData func(string, *types.MarketDataUpdate)) (*Manager, error) {
	pool, err := ants.NewPool(100)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create worker pool")
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		cfg:      cfg,
		gateways: cfg.Gateways,
		streams:  make(map[string]*Stream),
		pool:     pool,
		logger:   logger,
		onData:   onData,
		ctx:      ctx,
		cancel:   cancel,
	}

	return m, nil
}

// Start starts the upstream manager.
func (m *Manager) Start() error {
	if len(m.gateways) == 0 {
		return errors.New("no gateways configured")
	}

	// Sort gateways by priority and connect to the first one
	m.sortGatewaysByPriority()
	if err := m.connectToGateway(0); err != nil {
		m.logger.Warn("Failed to connect to primary gateway, trying failover",
			zap.Error(err),
			zap.String("endpoint", m.gateways[0].Endpoint))
		return m.failover()
	}

	// Start health check
	go m.healthCheckLoop()

	return nil
}

// Stop stops the upstream manager.
func (m *Manager) Stop() {
	m.cancel()
	m.pool.Release()

	m.streamsMu.Lock()
	for _, stream := range m.streams {
		if stream.cancel != nil {
			stream.cancel()
		}
	}
	m.streamsMu.Unlock()
}

// Subscribe subscribes to a symbol's data stream.
func (m *Manager) Subscribe(symbol string) error {
	m.streamsMu.Lock()
	defer m.streamsMu.Unlock()

	if _, exists := m.streams[symbol]; exists {
		return nil // Already subscribed
	}

	ctx, cancel := context.WithCancel(m.ctx)
	stream := &Stream{
		Symbol:     symbol,
		Status:     types.StreamStatusConnecting,
		LastUpdate: time.Now(),
		cancel:     cancel,
	}
	m.streams[symbol] = stream

	// Start the stream in a worker
	m.pool.Submit(func() {
		m.runStream(ctx, stream)
	})

	return nil
}

// Unsubscribe unsubscribes from a symbol's data stream.
func (m *Manager) Unsubscribe(symbol string) {
	m.streamsMu.Lock()
	defer m.streamsMu.Unlock()

	if stream, exists := m.streams[symbol]; exists {
		if stream.cancel != nil {
			stream.cancel()
		}
		delete(m.streams, symbol)
	}
}

// GetActiveSymbols returns all active symbol subscriptions.
func (m *Manager) GetActiveSymbols() []string {
	m.streamsMu.RLock()
	defer m.streamsMu.RUnlock()

	symbols := make([]string, 0, len(m.streams))
	for symbol := range m.streams {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// GetStreamStatus returns the status of a stream.
func (m *Manager) GetStreamStatus(symbol string) (types.StreamStatus, bool) {
	m.streamsMu.RLock()
	defer m.streamsMu.RUnlock()

	stream, exists := m.streams[symbol]
	if !exists {
		return types.StreamStatusClosed, false
	}
	return stream.Status, true
}

// runStream runs a stream for a symbol with reconnection logic.
func (m *Manager) runStream(ctx context.Context, stream *Stream) {
	for {
		select {
		case <-ctx.Done():
			stream.Status = types.StreamStatusClosed
			return
		default:
		}

		// Simulate connecting to upstream and receiving data
		// In production, this would be a gRPC or WebSocket connection
		stream.Status = types.StreamStatusActive
		m.logger.Info("Stream connected", zap.String("symbol", stream.Symbol))

		// Simulated data loop - in production this reads from actual connection
		if err := m.streamData(ctx, stream); err != nil {
			m.logger.Warn("Stream disconnected",
				zap.String("symbol", stream.Symbol),
				zap.Error(err))
			
			stream.Status = types.StreamStatusReconnecting
			stream.ReconnectCount++

			// Exponential backoff with jitter
			delay := m.calculateBackoff(stream.ReconnectCount)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
				continue
			}
		}
	}
}

// streamData simulates receiving data from upstream.
// In production, this would be replaced with actual gRPC/WS client code.
func (m *Manager) streamData(ctx context.Context, stream *Stream) error {
	// This is a placeholder - in production, implement actual streaming
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Simulate receiving orderbook update
			stream.LastUpdate = time.Now()

			// In production, parse actual data from connection
			update := &types.MarketDataUpdate{
				Type:      "orderbook",
				Symbol:    stream.Symbol,
				Timestamp: time.Now(),
				Sequence:  time.Now().UnixNano(),
			}

			if m.onData != nil {
				m.onData(stream.Symbol, update)
			}
		}
	}
}

// calculateBackoff calculates exponential backoff with jitter.
func (m *Manager) calculateBackoff(attempt int) time.Duration {
	base := m.cfg.ReconnectBaseDelay
	if base == 0 {
		base = 100 * time.Millisecond
	}

	max := m.cfg.ReconnectMaxDelay
	if max == 0 {
		max = 30 * time.Second
	}

	// Exponential backoff: base * 2^attempt
	delay := base * time.Duration(1<<uint(attempt-1))
	if delay > max {
		delay = max
	}

	// Add jitter (±10%)
	jitter := time.Duration(float64(delay) * 0.1 * (rand.Float64()*2 - 1))
	return delay + jitter
}

// connectToGateway connects to a gateway by index.
func (m *Manager) connectToGateway(index int) error {
	if index >= len(m.gateways) {
		return errors.New("gateway index out of range")
	}

	gw := m.gateways[index]
	m.logger.Info("Connecting to gateway",
		zap.String("endpoint", gw.Endpoint),
		zap.Int("priority", gw.Priority))

	// In production, establish actual connection here
	m.activeGateway = &GatewayConnection{
		Endpoint:  gw.Endpoint,
		Priority:  gw.Priority,
		Region:    gw.Region,
		Connected: true,
		LastPing:  time.Now(),
	}

	return nil
}

// failover attempts to connect to the next available gateway.
func (m *Manager) failover() error {
	if !m.reconnecting.CompareAndSwap(false, true) {
		return nil // Already reconnecting
	}
	defer m.reconnecting.Store(false)

	for i := range m.gateways {
		if err := m.connectToGateway(i); err == nil {
			return nil
		}
		m.logger.Warn("Gateway failover failed",
			zap.String("endpoint", m.gateways[i].Endpoint))
	}

	return errors.New("all gateways failed")
}

// healthCheckLoop periodically checks gateway health.
func (m *Manager) healthCheckLoop() {
	ticker := time.NewTicker(m.cfg.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.performHealthCheck()
		}
	}
}

// performHealthCheck checks the health of the current gateway.
func (m *Manager) performHealthCheck() {
	if m.activeGateway == nil {
		m.failover()
		return
	}

	// In production, send actual ping/health check
	// For now, just update the timestamp
	m.activeGateway.LastPing = time.Now()
}

// sortGatewaysByPriority sorts gateways by priority (ascending).
func (m *Manager) sortGatewaysByPriority() {
	// Simple bubble sort for small slice
	for i := 0; i < len(m.gateways)-1; i++ {
		for j := 0; j < len(m.gateways)-i-1; j++ {
			if m.gateways[j].Priority > m.gateways[j+1].Priority {
				m.gateways[j], m.gateways[j+1] = m.gateways[j+1], m.gateways[j]
			}
		}
	}
}

// Stats returns upstream manager statistics.
type Stats struct {
	ActiveGateway   string
	ActiveStreams   int
	TotalReconnects int
}

// GetStats returns current statistics.
func (m *Manager) GetStats() Stats {
	m.streamsMu.RLock()
	defer m.streamsMu.RUnlock()

	var totalReconnects int
	for _, stream := range m.streams {
		totalReconnects += stream.ReconnectCount
	}

	gatewayEndpoint := ""
	if m.activeGateway != nil {
		gatewayEndpoint = m.activeGateway.Endpoint
	}

	return Stats{
		ActiveGateway:   gatewayEndpoint,
		ActiveStreams:   len(m.streams),
		TotalReconnects: totalReconnects,
	}
}
