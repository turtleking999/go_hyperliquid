// Package cache provides in-memory caching for market data.
package cache

import (
	"sync"
	"time"

	"go_hyperliquid/relay/pkg/types"

	"github.com/valyala/bytebufferpool"
)

// Layer provides thread-safe caching for orderbook snapshots and trades.
type Layer struct {
	orderbooks  map[string]*types.OrderbookSnapshot
	trades      map[string]*TradeRingBuffer
	mu          sync.RWMutex
	maxDepth    int
	tradeSize   int
	bufferPool  *bytebufferpool.Pool
}

// TradeRingBuffer is a ring buffer for storing recent trades.
type TradeRingBuffer struct {
	trades []types.Trade
	head   int
	count  int
	mu     sync.RWMutex
}

// NewTradeRingBuffer creates a new trade ring buffer.
func NewTradeRingBuffer(size int) *TradeRingBuffer {
	return &TradeRingBuffer{
		trades: make([]types.Trade, size),
	}
}

// Add adds a trade to the ring buffer.
func (rb *TradeRingBuffer) Add(trade types.Trade) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.trades[rb.head] = trade
	rb.head = (rb.head + 1) % len(rb.trades)
	if rb.count < len(rb.trades) {
		rb.count++
	}
}

// GetAll returns all trades in the buffer (oldest first).
func (rb *TradeRingBuffer) GetAll() []types.Trade {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]types.Trade, rb.count)
	if rb.count < len(rb.trades) {
		copy(result, rb.trades[:rb.count])
	} else {
		// Buffer is full, need to handle wrap-around
		start := rb.head
		for i := 0; i < rb.count; i++ {
			result[i] = rb.trades[(start+i)%len(rb.trades)]
		}
	}
	return result
}

// GetRecent returns the N most recent trades.
func (rb *TradeRingBuffer) GetRecent(n int) []types.Trade {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n > rb.count {
		n = rb.count
	}
	result := make([]types.Trade, n)
	for i := 0; i < n; i++ {
		idx := (rb.head - n + i + len(rb.trades)) % len(rb.trades)
		result[i] = rb.trades[idx]
	}
	return result
}

// NewLayer creates a new cache layer.
func NewLayer(maxDepth, tradeSize int) *Layer {
	return &Layer{
		orderbooks: make(map[string]*types.OrderbookSnapshot),
		trades:     make(map[string]*TradeRingBuffer),
		maxDepth:   maxDepth,
		tradeSize:  tradeSize,
		bufferPool: &bytebufferpool.Pool{},
	}
}

// UpdateOrderbook updates the cached orderbook for a symbol.
func (l *Layer) UpdateOrderbook(snapshot *types.OrderbookSnapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Trim to max depth
	if len(snapshot.Asks) > l.maxDepth {
		snapshot.Asks = snapshot.Asks[:l.maxDepth]
	}
	if len(snapshot.Bids) > l.maxDepth {
		snapshot.Bids = snapshot.Bids[:l.maxDepth]
	}

	l.orderbooks[snapshot.Symbol] = snapshot
}

// GetOrderbook retrieves the cached orderbook for a symbol.
func (l *Layer) GetOrderbook(symbol string) (*types.OrderbookSnapshot, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	snapshot, ok := l.orderbooks[symbol]
	if !ok {
		return nil, false
	}
	// Return a copy to prevent concurrent modification
	return l.copyOrderbook(snapshot), true
}

func (l *Layer) copyOrderbook(src *types.OrderbookSnapshot) *types.OrderbookSnapshot {
	dst := &types.OrderbookSnapshot{
		Symbol:    src.Symbol,
		Timestamp: src.Timestamp,
		Sequence:  src.Sequence,
		Asks:      make([]types.PriceLevel, len(src.Asks)),
		Bids:      make([]types.PriceLevel, len(src.Bids)),
	}
	copy(dst.Asks, src.Asks)
	copy(dst.Bids, src.Bids)
	return dst
}

// AddTrade adds a trade to the cache.
func (l *Layer) AddTrade(trade types.Trade) {
	l.mu.Lock()
	rb, ok := l.trades[trade.Symbol]
	if !ok {
		rb = NewTradeRingBuffer(l.tradeSize)
		l.trades[trade.Symbol] = rb
	}
	l.mu.Unlock()

	rb.Add(trade)
}

// GetRecentTrades retrieves recent trades for a symbol.
func (l *Layer) GetRecentTrades(symbol string, count int) []types.Trade {
	l.mu.RLock()
	rb, ok := l.trades[symbol]
	l.mu.RUnlock()

	if !ok {
		return nil
	}
	return rb.GetRecent(count)
}

// GetSymbols returns all cached symbols.
func (l *Layer) GetSymbols() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	symbols := make([]string, 0, len(l.orderbooks))
	for s := range l.orderbooks {
		symbols = append(symbols, s)
	}
	return symbols
}

// Cleanup removes stale entries older than the given duration.
func (l *Layer) Cleanup(staleThreshold time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for symbol, snapshot := range l.orderbooks {
		if now.Sub(snapshot.Timestamp) > staleThreshold {
			delete(l.orderbooks, symbol)
			delete(l.trades, symbol)
		}
	}
}

// GetBuffer gets a buffer from the pool.
func (l *Layer) GetBuffer() *bytebufferpool.ByteBuffer {
	return l.bufferPool.Get()
}

// PutBuffer returns a buffer to the pool.
func (l *Layer) PutBuffer(buf *bytebufferpool.ByteBuffer) {
	l.bufferPool.Put(buf)
}

// Stats returns cache statistics.
type Stats struct {
	OrderbookCount int
	TradeSymbols   int
}

// GetStats returns current cache statistics.
func (l *Layer) GetStats() Stats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return Stats{
		OrderbookCount: len(l.orderbooks),
		TradeSymbols:   len(l.trades),
	}
}
