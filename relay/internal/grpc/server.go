// Package grpc provides the gRPC API server.
package grpc

import (
	"context"
	"net"
	"time"

	"go_hyperliquid/relay/internal/auth"
	"go_hyperliquid/relay/internal/cache"
	"go_hyperliquid/relay/internal/fanout"
	"go_hyperliquid/relay/internal/metrics"
	"go_hyperliquid/relay/internal/ratelimit"
	"go_hyperliquid/relay/internal/upstream"
	"go_hyperliquid/relay/pkg/types"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Server is the gRPC API server.
type Server struct {
	server    *grpc.Server
	auth      *auth.Service
	cache     *cache.Layer
	fanout    *fanout.Hub
	upstream  *upstream.Manager
	limiter   *ratelimit.Limiter
	metrics   *metrics.Metrics
	logger    *zap.Logger
	port      int
}

// Config holds gRPC server configuration.
type Config struct {
	Port int
}

// NewServer creates a new gRPC server.
func NewServer(
	cfg *Config,
	authSvc *auth.Service,
	cacheLyr *cache.Layer,
	fanoutHub *fanout.Hub,
	upstreamMgr *upstream.Manager,
	limiter *ratelimit.Limiter,
	metricsInst *metrics.Metrics,
	logger *zap.Logger,
) *Server {
	s := &Server{
		auth:     authSvc,
		cache:    cacheLyr,
		fanout:   fanoutHub,
		upstream: upstreamMgr,
		limiter:  limiter,
		metrics:  metricsInst,
		logger:   logger,
		port:     cfg.Port,
	}

	// Create gRPC server with interceptors
	s.server = grpc.NewServer(
		grpc.UnaryInterceptor(s.unaryAuthInterceptor),
		grpc.StreamInterceptor(s.streamAuthInterceptor),
	)

	return s
}

// Start starts the gRPC server.
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", ":"+string(rune(s.port+'0')))
	if err != nil {
		return errors.Wrap(err, "failed to listen")
	}

	s.logger.Info("Starting gRPC server", zap.Int("port", s.port))
	return s.server.Serve(lis)
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
	s.server.GracefulStop()
}

// unaryAuthInterceptor is the unary auth interceptor.
func (s *Server) unaryAuthInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	// Extract API key from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	apiKeys := md.Get("x-api-key")
	if len(apiKeys) == 0 {
		return nil, status.Error(codes.Unauthenticated, "API key is required")
	}

	// Authenticate
	authCtx, err := s.auth.Authenticate(ctx, apiKeys[0])
	if err != nil {
		s.metrics.RecordAuthFailure(err.Error())
		if errors.Is(err, auth.ErrInvalidAPIKey) {
			return nil, status.Error(codes.Unauthenticated, "invalid API key")
		}
		if errors.Is(err, auth.ErrExpiredAPIKey) {
			return nil, status.Error(codes.Unauthenticated, "API key has expired")
		}
		if errors.Is(err, auth.ErrRevokedAPIKey) {
			return nil, status.Error(codes.Unauthenticated, "API key has been revoked")
		}
		if errors.Is(err, auth.ErrSuspendedTenant) {
			return nil, status.Error(codes.PermissionDenied, "tenant account is suspended")
		}
		return nil, status.Error(codes.Internal, "authentication failed")
	}

	s.metrics.RecordAuthSuccess()

	// Rate limit check
	keyID := string(rune(authCtx.APIKeyID))
	if !s.limiter.Allow(keyID) {
		s.metrics.RecordRateLimitHit("rps")
		return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
	}

	// Add auth context to context
	ctx = context.WithValue(ctx, authContextKey, authCtx)

	return handler(ctx, req)
}

// streamAuthInterceptor is the stream auth interceptor.
func (s *Server) streamAuthInterceptor(
	srv interface{},
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	// Extract API key from metadata
	md, ok := metadata.FromIncomingContext(ss.Context())
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}

	apiKeys := md.Get("x-api-key")
	if len(apiKeys) == 0 {
		return status.Error(codes.Unauthenticated, "API key is required")
	}

	// Authenticate
	authCtx, err := s.auth.Authenticate(ss.Context(), apiKeys[0])
	if err != nil {
		s.metrics.RecordAuthFailure(err.Error())
		if errors.Is(err, auth.ErrInvalidAPIKey) {
			return status.Error(codes.Unauthenticated, "invalid API key")
		}
		return status.Error(codes.Internal, "authentication failed")
	}

	s.metrics.RecordAuthSuccess()

	// Check stream limit
	keyID := string(rune(authCtx.APIKeyID))
	if !s.limiter.AcquireStream(keyID, authCtx.MaxConcurrentStreams) {
		s.metrics.RecordRateLimitHit("streams")
		return status.Error(codes.ResourceExhausted, "maximum concurrent streams exceeded")
	}
	defer s.limiter.ReleaseStream(keyID)

	// Wrap the stream with auth context
	wrappedStream := &authServerStream{
		ServerStream: ss,
		ctx:          context.WithValue(ss.Context(), authContextKey, authCtx),
	}

	return handler(srv, wrappedStream)
}

// authServerStream wraps ServerStream with a custom context.
type authServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authServerStream) Context() context.Context {
	return s.ctx
}

// authContextKey is the context key for auth context.
type authContextKeyType struct{}

var authContextKey = authContextKeyType{}

// GetAuthContext retrieves auth context from context.
func GetAuthContext(ctx context.Context) (*auth.Service, bool) {
	v := ctx.Value(authContextKey)
	if v == nil {
		return nil, false
	}
	authSvc, ok := v.(*auth.Service)
	return authSvc, ok
}

// MarketDataServiceServer implements the MarketDataService.
type MarketDataServiceServer struct {
	cache    *cache.Layer
	fanout   *fanout.Hub
	upstream *upstream.Manager
	metrics  *metrics.Metrics
	logger   *zap.Logger
}

// NewMarketDataServiceServer creates a new MarketDataServiceServer.
func NewMarketDataServiceServer(
	cache *cache.Layer,
	fanout *fanout.Hub,
	upstream *upstream.Manager,
	metricsInst *metrics.Metrics,
	logger *zap.Logger,
) *MarketDataServiceServer {
	return &MarketDataServiceServer{
		cache:    cache,
		fanout:   fanout,
		upstream: upstream,
		metrics:  metricsInst,
		logger:   logger,
	}
}

// StreamOrderBook streams orderbook updates.
func (s *MarketDataServiceServer) StreamOrderBook(symbol string, depth int, sendFunc func(*types.OrderbookSnapshot) error) error {
	// Subscribe to upstream if needed
	if err := s.upstream.Subscribe(symbol); err != nil {
		return status.Error(codes.Internal, "failed to subscribe to symbol")
	}

	s.metrics.RecordStreamSubscribe(symbol)
	defer s.metrics.RecordStreamUnsubscribe(symbol)

	// Create subscriber
	subID := generateID()
	sub := s.fanout.CreateSubscriber(subID, 0, 0)
	s.fanout.Subscribe(symbol, sub)
	defer s.fanout.Unsubscribe(symbol, subID)

	// Send initial snapshot
	if snapshot, ok := s.cache.GetOrderbook(symbol); ok {
		if err := sendFunc(snapshot); err != nil {
			return err
		}
	}

	// Stream updates
	for update := range sub.SendChan {
		if update.Orderbook != nil {
			s.metrics.RecordMessageSent(symbol, "orderbook")
			if err := sendFunc(update.Orderbook); err != nil {
				return err
			}
		}
	}

	return nil
}

// GetOrderBookSnapshot returns the current orderbook snapshot.
func (s *MarketDataServiceServer) GetOrderBookSnapshot(ctx context.Context, symbol string, depth int) (*types.OrderbookSnapshot, error) {
	snapshot, ok := s.cache.GetOrderbook(symbol)
	if !ok {
		// Try to subscribe
		if err := s.upstream.Subscribe(symbol); err != nil {
			return nil, status.Error(codes.Internal, "failed to subscribe to symbol")
		}
		s.metrics.RecordCacheMiss()
		return nil, status.Error(codes.NotFound, "orderbook not available yet")
	}

	s.metrics.RecordCacheHit()
	return snapshot, nil
}

// GetRecentTrades returns recent trades for a symbol.
func (s *MarketDataServiceServer) GetRecentTrades(ctx context.Context, symbol string, count int) ([]types.Trade, error) {
	trades := s.cache.GetRecentTrades(symbol, count)
	return trades, nil
}

// GetSymbols returns available symbols.
func (s *MarketDataServiceServer) GetSymbols(ctx context.Context) ([]string, error) {
	return s.cache.GetSymbols(), nil
}

// generateID generates a random subscriber ID.
func generateID() string {
	return time.Now().Format("20060102150405.000000")
}
