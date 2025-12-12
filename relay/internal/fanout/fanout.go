// Package fanout provides a pub/sub fanout hub for distributing market data.
package fanout

import (
	"sync"
	"sync/atomic"
	"time"

	"go_hyperliquid/relay/pkg/types"

	"github.com/olebedev/emitter"
)

// Hub manages topics and subscribers for market data distribution.
type Hub struct {
	emitter           *emitter.Emitter
	topics            map[string]*Topic
	mu                sync.RWMutex
	bufferSize        int
	slowThreshold     int
	zombieTimeout     time.Duration
	activeSubscribers atomic.Int64
	droppedMessages   atomic.Int64
}

// Topic represents a single symbol's subscriber list.
type Topic struct {
	symbol      string
	subscribers map[string]*Subscriber
	mu          sync.RWMutex
	lastUpdate  time.Time
}

// Subscriber represents a downstream client.
type Subscriber struct {
	ID          string
	TenantID    int64
	APIKeyID    int64
	SendChan    chan *types.MarketDataUpdate
	ConnectTime time.Time
	LastSend    time.Time
	Dropped     atomic.Int64
	closed      atomic.Bool
}

// NewHub creates a new fanout hub.
func NewHub(bufferSize, slowThreshold int, zombieTimeout time.Duration) *Hub {
	return &Hub{
		emitter:       emitter.New(uint(bufferSize)),
		topics:        make(map[string]*Topic),
		bufferSize:    bufferSize,
		slowThreshold: slowThreshold,
		zombieTimeout: zombieTimeout,
	}
}

// Subscribe adds a subscriber to a symbol topic.
func (h *Hub) Subscribe(symbol string, sub *Subscriber) {
	h.mu.Lock()
	topic, ok := h.topics[symbol]
	if !ok {
		topic = &Topic{
			symbol:      symbol,
			subscribers: make(map[string]*Subscriber),
		}
		h.topics[symbol] = topic
	}
	h.mu.Unlock()

	topic.mu.Lock()
	topic.subscribers[sub.ID] = sub
	topic.mu.Unlock()

	h.activeSubscribers.Add(1)
}

// Unsubscribe removes a subscriber from a symbol topic.
func (h *Hub) Unsubscribe(symbol, subID string) {
	h.mu.RLock()
	topic, ok := h.topics[symbol]
	h.mu.RUnlock()

	if !ok {
		return
	}

	topic.mu.Lock()
	if sub, exists := topic.subscribers[subID]; exists {
		sub.closed.Store(true)
		close(sub.SendChan)
		delete(topic.subscribers, subID)
		h.activeSubscribers.Add(-1)
	}
	topic.mu.Unlock()
}

// Publish sends an update to all subscribers of a symbol.
func (h *Hub) Publish(symbol string, update *types.MarketDataUpdate) {
	h.mu.RLock()
	topic, ok := h.topics[symbol]
	h.mu.RUnlock()

	if !ok {
		return
	}

	topic.mu.RLock()
	topic.lastUpdate = time.Now()
	subscribers := make([]*Subscriber, 0, len(topic.subscribers))
	for _, sub := range topic.subscribers {
		subscribers = append(subscribers, sub)
	}
	topic.mu.RUnlock()

	// Publish to all subscribers non-blocking
	for _, sub := range subscribers {
		if sub.closed.Load() {
			continue
		}
		select {
		case sub.SendChan <- update:
			sub.LastSend = time.Now()
		default:
			// Buffer full, drop message
			sub.Dropped.Add(1)
			h.droppedMessages.Add(1)

			// Check if subscriber is too slow
			if sub.Dropped.Load() > int64(h.slowThreshold) {
				h.handleSlowConsumer(symbol, sub.ID)
			}
		}
	}

	// Also emit via emitter for additional handlers
	h.emitter.Emit(symbol, update)
}

// handleSlowConsumer handles a slow consumer by disconnecting them.
func (h *Hub) handleSlowConsumer(symbol, subID string) {
	h.Unsubscribe(symbol, subID)
}

// GetTopicStats returns statistics for a topic.
func (h *Hub) GetTopicStats(symbol string) (subscriberCount int, lastUpdate time.Time) {
	h.mu.RLock()
	topic, ok := h.topics[symbol]
	h.mu.RUnlock()

	if !ok {
		return 0, time.Time{}
	}

	topic.mu.RLock()
	defer topic.mu.RUnlock()

	return len(topic.subscribers), topic.lastUpdate
}

// GetActiveSymbols returns all symbols with active subscribers.
func (h *Hub) GetActiveSymbols() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	symbols := make([]string, 0, len(h.topics))
	for symbol, topic := range h.topics {
		topic.mu.RLock()
		if len(topic.subscribers) > 0 {
			symbols = append(symbols, symbol)
		}
		topic.mu.RUnlock()
	}
	return symbols
}

// CleanupZombies removes subscribers that haven't received data recently.
func (h *Hub) CleanupZombies() {
	h.mu.RLock()
	topics := make([]*Topic, 0, len(h.topics))
	for _, topic := range h.topics {
		topics = append(topics, topic)
	}
	h.mu.RUnlock()

	now := time.Now()
	for _, topic := range topics {
		topic.mu.Lock()
		for subID, sub := range topic.subscribers {
			if now.Sub(sub.LastSend) > h.zombieTimeout && !sub.closed.Load() {
				sub.closed.Store(true)
				close(sub.SendChan)
				delete(topic.subscribers, subID)
				h.activeSubscribers.Add(-1)
			}
		}
		topic.mu.Unlock()
	}
}

// Stats returns hub statistics.
type Stats struct {
	ActiveTopics      int
	ActiveSubscribers int64
	DroppedMessages   int64
}

// GetStats returns current hub statistics.
func (h *Hub) GetStats() Stats {
	h.mu.RLock()
	topicCount := len(h.topics)
	h.mu.RUnlock()

	return Stats{
		ActiveTopics:      topicCount,
		ActiveSubscribers: h.activeSubscribers.Load(),
		DroppedMessages:   h.droppedMessages.Load(),
	}
}

// On registers a handler for events on a pattern (supports wildcards).
// Returns a channel that receives events matching the pattern.
func (h *Hub) On(pattern string) <-chan emitter.Event {
	return h.emitter.On(pattern)
}

// Off removes a handler for events on a pattern.
func (h *Hub) Off(pattern string, ch <-chan emitter.Event) {
	h.emitter.Off(pattern, ch)
}

// CreateSubscriber creates a new subscriber instance.
func (h *Hub) CreateSubscriber(id string, tenantID, apiKeyID int64) *Subscriber {
	return &Subscriber{
		ID:          id,
		TenantID:    tenantID,
		APIKeyID:    apiKeyID,
		SendChan:    make(chan *types.MarketDataUpdate, h.bufferSize),
		ConnectTime: time.Now(),
		LastSend:    time.Now(),
	}
}
