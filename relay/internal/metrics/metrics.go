// Package metrics provides Prometheus metrics for observability.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics.
type Metrics struct {
	// Request metrics
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight prometheus.Gauge

	// Stream metrics
	ActiveStreams     prometheus.Gauge
	StreamSubscribes  *prometheus.CounterVec
	DroppedMessages   *prometheus.CounterVec
	MessagesSent      *prometheus.CounterVec

	// Upstream metrics
	UpstreamConnected prometheus.Gauge
	UpstreamReconnects prometheus.Counter
	UpstreamLatency   prometheus.Histogram
	UpstreamErrors    *prometheus.CounterVec

	// Cache metrics
	CacheHits   prometheus.Counter
	CacheMisses prometheus.Counter
	CacheSize   prometheus.Gauge

	// Rate limit metrics
	RateLimitHits *prometheus.CounterVec

	// Auth metrics
	AuthSuccesses prometheus.Counter
	AuthFailures  *prometheus.CounterVec
}

// namespace is the metrics namespace.
const namespace = "hl_relay"

// NewMetrics creates a new Metrics instance with registered metrics.
func NewMetrics() *Metrics {
	return &Metrics{
		// Request metrics
		RequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "requests_total",
				Help:      "Total number of requests",
			},
			[]string{"method", "endpoint", "status"},
		),
		RequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "request_duration_seconds",
				Help:      "Request duration in seconds",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"method", "endpoint"},
		),
		RequestsInFlight: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "requests_in_flight",
				Help:      "Current number of requests being processed",
			},
		),

		// Stream metrics
		ActiveStreams: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "active_streams",
				Help:      "Current number of active streams",
			},
		),
		StreamSubscribes: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "stream_subscribes_total",
				Help:      "Total number of stream subscriptions",
			},
			[]string{"symbol", "action"}, // action: subscribe, unsubscribe
		),
		DroppedMessages: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "dropped_messages_total",
				Help:      "Total number of dropped messages due to slow consumers",
			},
			[]string{"symbol"},
		),
		MessagesSent: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "messages_sent_total",
				Help:      "Total number of messages sent to subscribers",
			},
			[]string{"symbol", "type"}, // type: orderbook, trade, etc.
		),

		// Upstream metrics
		UpstreamConnected: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "upstream_connected",
				Help:      "Whether connected to upstream gateway (1=yes, 0=no)",
			},
		),
		UpstreamReconnects: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "upstream_reconnects_total",
				Help:      "Total number of upstream reconnections",
			},
		),
		UpstreamLatency: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "upstream_latency_seconds",
				Help:      "Latency from upstream gateway in seconds",
				Buckets:   []float64{.0001, .0005, .001, .005, .01, .05, .1},
			},
		),
		UpstreamErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "upstream_errors_total",
				Help:      "Total number of upstream errors",
			},
			[]string{"type"}, // type: connection, parse, timeout
		),

		// Cache metrics
		CacheHits: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "cache_hits_total",
				Help:      "Total number of cache hits",
			},
		),
		CacheMisses: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "cache_misses_total",
				Help:      "Total number of cache misses",
			},
		),
		CacheSize: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "cache_size",
				Help:      "Current number of items in cache",
			},
		),

		// Rate limit metrics
		RateLimitHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "ratelimit_hits_total",
				Help:      "Total number of rate limit hits",
			},
			[]string{"type"}, // type: rps, streams
		),

		// Auth metrics
		AuthSuccesses: promauto.NewCounter(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "auth_successes_total",
				Help:      "Total number of successful authentications",
			},
		),
		AuthFailures: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "auth_failures_total",
				Help:      "Total number of failed authentications",
			},
			[]string{"reason"}, // reason: invalid_key, expired, revoked, suspended
		),
	}
}

// Handler returns the Prometheus HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// RecordRequest records a request metric.
func (m *Metrics) RecordRequest(method, endpoint string, status int, duration float64) {
	m.RequestsTotal.WithLabelValues(method, endpoint, string(rune(status))).Inc()
	m.RequestDuration.WithLabelValues(method, endpoint).Observe(duration)
}

// RecordStreamSubscribe records a stream subscription.
func (m *Metrics) RecordStreamSubscribe(symbol string) {
	m.StreamSubscribes.WithLabelValues(symbol, "subscribe").Inc()
	m.ActiveStreams.Inc()
}

// RecordStreamUnsubscribe records a stream unsubscription.
func (m *Metrics) RecordStreamUnsubscribe(symbol string) {
	m.StreamSubscribes.WithLabelValues(symbol, "unsubscribe").Inc()
	m.ActiveStreams.Dec()
}

// RecordDroppedMessage records a dropped message.
func (m *Metrics) RecordDroppedMessage(symbol string) {
	m.DroppedMessages.WithLabelValues(symbol).Inc()
}

// RecordMessageSent records a message sent.
func (m *Metrics) RecordMessageSent(symbol, msgType string) {
	m.MessagesSent.WithLabelValues(symbol, msgType).Inc()
}

// RecordUpstreamConnected records upstream connection status.
func (m *Metrics) RecordUpstreamConnected(connected bool) {
	if connected {
		m.UpstreamConnected.Set(1)
	} else {
		m.UpstreamConnected.Set(0)
	}
}

// RecordUpstreamReconnect records an upstream reconnection.
func (m *Metrics) RecordUpstreamReconnect() {
	m.UpstreamReconnects.Inc()
}

// RecordUpstreamLatency records upstream latency.
func (m *Metrics) RecordUpstreamLatency(seconds float64) {
	m.UpstreamLatency.Observe(seconds)
}

// RecordUpstreamError records an upstream error.
func (m *Metrics) RecordUpstreamError(errorType string) {
	m.UpstreamErrors.WithLabelValues(errorType).Inc()
}

// RecordCacheHit records a cache hit.
func (m *Metrics) RecordCacheHit() {
	m.CacheHits.Inc()
}

// RecordCacheMiss records a cache miss.
func (m *Metrics) RecordCacheMiss() {
	m.CacheMisses.Inc()
}

// SetCacheSize sets the current cache size.
func (m *Metrics) SetCacheSize(size int) {
	m.CacheSize.Set(float64(size))
}

// RecordRateLimitHit records a rate limit hit.
func (m *Metrics) RecordRateLimitHit(limitType string) {
	m.RateLimitHits.WithLabelValues(limitType).Inc()
}

// RecordAuthSuccess records a successful authentication.
func (m *Metrics) RecordAuthSuccess() {
	m.AuthSuccesses.Inc()
}

// RecordAuthFailure records a failed authentication.
func (m *Metrics) RecordAuthFailure(reason string) {
	m.AuthFailures.WithLabelValues(reason).Inc()
}
