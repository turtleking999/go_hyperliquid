// Package types defines the core types for the HL Relay Service.
package types

import (
	"time"
)

// StreamStatus represents the status of an upstream stream.
type StreamStatus int

const (
	StreamStatusConnecting StreamStatus = iota
	StreamStatusActive
	StreamStatusReconnecting
	StreamStatusClosed
)

func (s StreamStatus) String() string {
	switch s {
	case StreamStatusConnecting:
		return "CONNECTING"
	case StreamStatusActive:
		return "ACTIVE"
	case StreamStatusReconnecting:
		return "RECONNECTING"
	case StreamStatusClosed:
		return "CLOSED"
	default:
		return "UNKNOWN"
	}
}

// PriceLevel represents a single price level in the orderbook.
type PriceLevel struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

// OrderbookSnapshot represents a point-in-time snapshot of the orderbook.
type OrderbookSnapshot struct {
	Symbol    string       `json:"symbol"`
	Timestamp time.Time    `json:"timestamp"`
	Sequence  int64        `json:"sequence"`
	Asks      []PriceLevel `json:"asks"` // Sorted by price ascending
	Bids      []PriceLevel `json:"bids"` // Sorted by price descending
}

// Trade represents a single trade.
type Trade struct {
	Symbol    string    `json:"symbol"`
	TradeID   string    `json:"trade_id"`
	Price     float64   `json:"price"`
	Size      float64   `json:"size"`
	Side      string    `json:"side"` // "buy" or "sell"
	Timestamp time.Time `json:"timestamp"`
}

// MarketDataUpdate represents an update message for market data.
type MarketDataUpdate struct {
	Type       string             `json:"type"` // "orderbook", "trade", "ticker"
	Symbol     string             `json:"symbol"`
	Timestamp  time.Time          `json:"timestamp"`
	Sequence   int64              `json:"sequence"`
	IsSnapshot bool               `json:"is_snapshot"`
	Orderbook  *OrderbookSnapshot `json:"orderbook,omitempty"`
	Trade      *Trade             `json:"trade,omitempty"`
}

// Subscriber represents a downstream client subscription.
type Subscriber struct {
	ID          string
	TenantID    int64
	APIKeyID    int64
	Symbol      string
	SendChan    chan *MarketDataUpdate
	ConnectTime time.Time
	LastSend    time.Time
	Dropped     int64
}

// SubscriptionRequest represents a request to subscribe to market data.
type SubscriptionRequest struct {
	Symbol   string `json:"symbol"`
	Depth    int    `json:"depth,omitempty"` // Optional max orderbook depth
	APIKey   string `json:"api_key"`
}

// SubscriptionResponse represents a response to a subscription request.
type SubscriptionResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	SubID    string `json:"sub_id,omitempty"`
}
