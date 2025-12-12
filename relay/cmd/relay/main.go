// Package main is the entry point for the HL Relay Service.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go_hyperliquid/relay/internal/api"
	"go_hyperliquid/relay/internal/auth"
	"go_hyperliquid/relay/internal/cache"
	"go_hyperliquid/relay/internal/config"
	"go_hyperliquid/relay/internal/fanout"
	"go_hyperliquid/relay/internal/logger"
	"go_hyperliquid/relay/internal/ratelimit"
	"go_hyperliquid/relay/internal/upstream"
	"go_hyperliquid/relay/pkg/types"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	if err := logger.Init(&logger.Config{
		Level:       cfg.Logger.Level,
		Development: cfg.Logger.Development,
		Encoding:    cfg.Logger.Encoding,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	log := logger.Log
	log.Info("Starting HL Relay Service",
		zap.Int("http_port", cfg.Server.HTTPPort),
		zap.Int("grpc_port", cfg.Server.GRPCPort))

	// Initialize MySQL connection
	db, err := initMySQL(&cfg.Database)
	if err != nil {
		log.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	defer db.Close()
	log.Info("Connected to MySQL")

	// Initialize Redis connection (optional)
	var redisClient *redis.Client
	if cfg.Redis.Addr != "" {
		redisClient = initRedis(&cfg.Redis)
		if err := redisClient.Ping(context.Background()).Err(); err != nil {
			log.Warn("Failed to connect to Redis, continuing without cache", zap.Error(err))
			redisClient = nil
		} else {
			log.Info("Connected to Redis")
			defer redisClient.Close()
		}
	}

	// Initialize components
	cacheLayer := cache.NewLayer(cfg.Cache.MaxOrderbookDepth, cfg.Cache.TradeHistorySize)
	log.Info("Cache layer initialized")

	fanoutHub := fanout.NewHub(
		cfg.Fanout.SubscriberBufferSize,
		cfg.Fanout.SlowConsumerThreshold,
		cfg.Fanout.ZombieTimeout,
	)
	log.Info("Fanout hub initialized")

	rateLimiter := ratelimit.NewLimiter(&ratelimit.Config{
		DefaultRPS:        cfg.Rate.DefaultRPS,
		DefaultMaxStreams: cfg.Rate.DefaultMaxStreams,
		BurstMultiplier:   cfg.Rate.BurstMultiplier,
	})
	log.Info("Rate limiter initialized")

	authService := auth.NewService(db, redisClient, &auth.Config{
		CacheTTL: 5 * time.Minute,
	})
	log.Info("Auth service initialized")

	// Initialize upstream manager with data callback
	onData := func(symbol string, data *types.MarketDataUpdate) {
		// Update cache
		if data.Orderbook != nil {
			cacheLayer.UpdateOrderbook(data.Orderbook)
		}
		if data.Trade != nil {
			cacheLayer.AddTrade(*data.Trade)
		}
		// Fanout to subscribers
		fanoutHub.Publish(symbol, data)
	}

	upstreamMgr, err := upstream.NewManager(&cfg.Upstream, log, onData)
	if err != nil {
		log.Fatal("Failed to create upstream manager", zap.Error(err))
	}
	log.Info("Upstream manager initialized")

	// Start upstream manager
	if err := upstreamMgr.Start(); err != nil {
		log.Fatal("Failed to start upstream manager", zap.Error(err))
	}
	log.Info("Upstream manager started")

	// Initialize API server
	server := api.NewServer(
		&cfg.Server,
		authService,
		cacheLayer,
		fanoutHub,
		upstreamMgr,
		rateLimiter,
		log,
	)
	log.Info("API server initialized")

	// Start server in goroutine
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	upstreamMgr.Stop()
	if err := server.Shutdown(); err != nil {
		log.Error("Server shutdown error", zap.Error(err))
	}

	select {
	case <-ctx.Done():
		log.Warn("Shutdown timed out")
	default:
		log.Info("Shutdown complete")
	}
}

// initMySQL initializes MySQL connection.
func initMySQL(cfg *config.DatabaseConfig) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

// initRedis initializes Redis connection.
func initRedis(cfg *config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})
}
