// Package api provides the HTTP API using Fiber.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"go_hyperliquid/relay/internal/auth"
	"go_hyperliquid/relay/internal/cache"
	"go_hyperliquid/relay/internal/config"
	"go_hyperliquid/relay/internal/fanout"
	"go_hyperliquid/relay/internal/models"
	"go_hyperliquid/relay/internal/ratelimit"
	"go_hyperliquid/relay/internal/upstream"
	"go_hyperliquid/relay/pkg/types"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Server is the HTTP API server.
type Server struct {
	app       *fiber.App
	cfg       *config.ServerConfig
	auth      *auth.Service
	cache     *cache.Layer
	fanout    *fanout.Hub
	upstream  *upstream.Manager
	limiter   *ratelimit.Limiter
	logger    *zap.Logger
}

// NewServer creates a new API server.
func NewServer(
	cfg *config.ServerConfig,
	authSvc *auth.Service,
	cacheLyr *cache.Layer,
	fanoutHub *fanout.Hub,
	upstreamMgr *upstream.Manager,
	limiter *ratelimit.Limiter,
	logger *zap.Logger,
) *Server {
	app := fiber.New(fiber.Config{
		AppName:      "HL Relay Service",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	})

	s := &Server{
		app:      app,
		cfg:      cfg,
		auth:     authSvc,
		cache:    cacheLyr,
		fanout:   fanoutHub,
		upstream: upstreamMgr,
		limiter:  limiter,
		logger:   logger,
	}

	s.setupMiddleware()
	s.setupRoutes()

	return s
}

// setupMiddleware sets up middleware.
func (s *Server) setupMiddleware() {
	s.app.Use(recover.New())
	s.app.Use(logger.New())
	s.app.Use(cors.New())
}

// setupRoutes sets up routes.
func (s *Server) setupRoutes() {
	// Health check
	s.app.Get("/health", s.handleHealth)

	// API v1
	v1 := s.app.Group("/v1")

	// Market data endpoints
	v1.Get("/orderbook", s.authMiddleware, s.handleGetOrderbook)
	v1.Get("/trades", s.authMiddleware, s.handleGetTrades)
	v1.Get("/symbols", s.authMiddleware, s.handleGetSymbols)

	// WebSocket endpoint
	s.app.Get("/ws", s.handleWebSocket)

	// Stats endpoint (internal)
	s.app.Get("/stats", s.handleStats)
}

// authMiddleware validates API key and rate limits.
func (s *Server) authMiddleware(c *fiber.Ctx) error {
	apiKey := c.Get("X-API-Key")
	if apiKey == "" {
		apiKey = c.Query("api_key")
	}

	if apiKey == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "API key is required",
			"code":  "AUTH_MISSING_KEY",
		})
	}

	// Authenticate
	authCtx, err := s.auth.Authenticate(c.Context(), apiKey)
	if err != nil {
		status := fiber.StatusUnauthorized
		code := "AUTH_INVALID_KEY"

		if errors.Is(err, auth.ErrSuspendedTenant) {
			status = fiber.StatusForbidden
			code = "AUTH_SUSPENDED_TENANT"
		} else if errors.Is(err, auth.ErrRevokedAPIKey) {
			code = "AUTH_REVOKED_KEY"
		} else if errors.Is(err, auth.ErrExpiredAPIKey) {
			code = "AUTH_EXPIRED_KEY"
		}

		return c.Status(status).JSON(fiber.Map{
			"error": err.Error(),
			"code":  code,
		})
	}

	// Rate limit
	keyID := string(rune(authCtx.APIKeyID))
	if !s.limiter.Allow(keyID) {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
			"error": "Rate limit exceeded",
			"code":  "QUOTA_EXCEEDED_RPS",
		})
	}

	// Store auth context
	c.Locals("auth", authCtx)
	return c.Next()
}

// handleHealth returns health status.
func (s *Server) handleHealth(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

// handleGetOrderbook returns orderbook snapshot.
func (s *Server) handleGetOrderbook(c *fiber.Ctx) error {
	symbol := c.Query("symbol")
	if symbol == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "symbol is required",
		})
	}

	snapshot, ok := s.cache.GetOrderbook(symbol)
	if !ok {
		// Try to subscribe to the symbol
		if err := s.upstream.Subscribe(symbol); err != nil {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "Failed to subscribe to symbol",
			})
		}

		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Orderbook not available yet, please retry",
		})
	}

	return c.JSON(snapshot)
}

// handleGetTrades returns recent trades.
func (s *Server) handleGetTrades(c *fiber.Ctx) error {
	symbol := c.Query("symbol")
	if symbol == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "symbol is required",
		})
	}

	count := c.QueryInt("count", 100)
	if count > 1000 {
		count = 1000
	}

	trades := s.cache.GetRecentTrades(symbol, count)
	return c.JSON(fiber.Map{
		"symbol": symbol,
		"trades": trades,
	})
}

// handleGetSymbols returns available symbols.
func (s *Server) handleGetSymbols(c *fiber.Ctx) error {
	symbols := s.cache.GetSymbols()
	return c.JSON(fiber.Map{
		"symbols": symbols,
	})
}

// handleStats returns service statistics.
func (s *Server) handleStats(c *fiber.Ctx) error {
	cacheStats := s.cache.GetStats()
	fanoutStats := s.fanout.GetStats()
	upstreamStats := s.upstream.GetStats()
	limiterStats := s.limiter.GetStats()

	return c.JSON(fiber.Map{
		"cache": fiber.Map{
			"orderbooks":    cacheStats.OrderbookCount,
			"trade_symbols": cacheStats.TradeSymbols,
		},
		"fanout": fiber.Map{
			"active_topics":      fanoutStats.ActiveTopics,
			"active_subscribers": fanoutStats.ActiveSubscribers,
			"dropped_messages":   fanoutStats.DroppedMessages,
		},
		"upstream": fiber.Map{
			"active_gateway": upstreamStats.ActiveGateway,
			"active_streams": upstreamStats.ActiveStreams,
			"reconnects":     upstreamStats.TotalReconnects,
		},
		"ratelimit": fiber.Map{
			"total_keys":    limiterStats.TotalKeys,
			"total_streams": limiterStats.TotalStreams,
		},
	})
}

// handleWebSocket handles WebSocket connections.
func (s *Server) handleWebSocket(c *fiber.Ctx) error {
	// Upgrade to WebSocket
	return c.Status(fiber.StatusUpgradeRequired).JSON(fiber.Map{
		"error": "WebSocket upgrade required",
	})
}

// WebSocket handling with nhooyr.io/websocket
// This is called from a separate HTTP handler

// WSMessage represents a WebSocket message.
type WSMessage struct {
	Op      string `json:"op"`
	Channel string `json:"channel,omitempty"`
	Symbol  string `json:"symbol,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

// WSHandler handles WebSocket connections.
type WSHandler struct {
	auth     *auth.Service
	cache    *cache.Layer
	fanout   *fanout.Hub
	upstream *upstream.Manager
	limiter  *ratelimit.Limiter
	logger   *zap.Logger
}

// NewWSHandler creates a new WebSocket handler.
func NewWSHandler(
	authSvc *auth.Service,
	cacheLyr *cache.Layer,
	fanoutHub *fanout.Hub,
	upstreamMgr *upstream.Manager,
	limiter *ratelimit.Limiter,
	logger *zap.Logger,
) *WSHandler {
	return &WSHandler{
		auth:     authSvc,
		cache:    cacheLyr,
		fanout:   fanoutHub,
		upstream: upstreamMgr,
		limiter:  limiter,
		logger:   logger,
	}
}

// Handle handles a WebSocket connection.
func (h *WSHandler) Handle(ctx context.Context, conn *websocket.Conn, apiKey string) error {
	// Authenticate
	authCtx, err := h.auth.Authenticate(ctx, apiKey)
	if err != nil {
		wsjson.Write(ctx, conn, fiber.Map{"error": err.Error()})
		return err
	}

	// Generate subscriber ID
	subID := generateID()
	keyID := string(rune(authCtx.APIKeyID))

	// Check stream limit
	if !h.limiter.AcquireStream(keyID, authCtx.MaxConcurrentStreams) {
		wsjson.Write(ctx, conn, fiber.Map{
			"error": "Maximum concurrent streams exceeded",
			"code":  "QUOTA_EXCEEDED_STREAMS",
		})
		return errors.New("stream limit exceeded")
	}
	defer h.limiter.ReleaseStream(keyID)

	// Create subscriber
	sub := h.fanout.CreateSubscriber(subID, authCtx.TenantID, authCtx.APIKeyID)

	// Handle messages
	go h.handleIncoming(ctx, conn, sub, authCtx)

	// Send outgoing messages
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-sub.SendChan:
			if !ok {
				return nil
			}
			if err := wsjson.Write(ctx, conn, msg); err != nil {
				return err
			}
		}
	}
}

// handleIncoming handles incoming WebSocket messages.
func (h *WSHandler) handleIncoming(ctx context.Context, conn *websocket.Conn, sub *fanout.Subscriber, authCtx *models.AuthContext) {
	for {
		var msg WSMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}

		switch msg.Op {
		case "subscribe":
			if msg.Symbol == "" {
				wsjson.Write(ctx, conn, fiber.Map{"error": "symbol is required"})
				continue
			}

			// Subscribe to upstream if needed
			h.upstream.Subscribe(msg.Symbol)

			// Add to fanout
			h.fanout.Subscribe(msg.Symbol, sub)

			// Send snapshot first
			if snapshot, ok := h.cache.GetOrderbook(msg.Symbol); ok {
				update := &types.MarketDataUpdate{
					Type:       "orderbook",
					Symbol:     msg.Symbol,
					Timestamp:  snapshot.Timestamp,
					Sequence:   snapshot.Sequence,
					IsSnapshot: true,
					Orderbook:  snapshot,
				}
				wsjson.Write(ctx, conn, update)
			}

			wsjson.Write(ctx, conn, fiber.Map{
				"op":      "subscribed",
				"channel": msg.Channel,
				"symbol":  msg.Symbol,
			})

		case "unsubscribe":
			h.fanout.Unsubscribe(msg.Symbol, sub.ID)
			wsjson.Write(ctx, conn, fiber.Map{
				"op":      "unsubscribed",
				"channel": msg.Channel,
				"symbol":  msg.Symbol,
			})

		case "ping":
			wsjson.Write(ctx, conn, fiber.Map{"op": "pong"})
		}
	}
}

// Start starts the server.
func (s *Server) Start() error {
	addr := s.cfg.Host + ":" + string(rune(s.cfg.HTTPPort))
	s.logger.Info("Starting HTTP server", zap.String("addr", addr))
	return s.app.Listen(addr)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown() error {
	return s.app.Shutdown()
}

// generateID generates a random ID.
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
