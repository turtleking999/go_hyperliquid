// Package config provides configuration management using viper.
package config

import (
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration structure.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Upstream UpstreamConfig `mapstructure:"upstream"`
	Fanout   FanoutConfig   `mapstructure:"fanout"`
	Cache    CacheConfig    `mapstructure:"cache"`
	Rate     RateConfig     `mapstructure:"rate"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Logger   LoggerConfig   `mapstructure:"logger"`
}

// ServerConfig holds server settings.
type ServerConfig struct {
	HTTPPort int    `mapstructure:"http_port"`
	GRPCPort int    `mapstructure:"grpc_port"`
	Host     string `mapstructure:"host"`
}

// UpstreamConfig holds upstream gateway settings.
type UpstreamConfig struct {
	Gateways           []GatewayConfig `mapstructure:"gateways"`
	HealthCheckInterval time.Duration  `mapstructure:"health_check_interval"`
	ReconnectMaxDelay  time.Duration   `mapstructure:"reconnect_max_delay"`
	ReconnectBaseDelay time.Duration   `mapstructure:"reconnect_base_delay"`
}

// GatewayConfig holds individual gateway settings.
type GatewayConfig struct {
	Endpoint string `mapstructure:"endpoint"`
	Priority int    `mapstructure:"priority"`
	Region   string `mapstructure:"region"`
}

// FanoutConfig holds fanout hub settings.
type FanoutConfig struct {
	SubscriberBufferSize  int           `mapstructure:"subscriber_buffer_size"`
	SlowConsumerThreshold int           `mapstructure:"slow_consumer_threshold"`
	ZombieTimeout         time.Duration `mapstructure:"zombie_timeout"`
}

// CacheConfig holds cache layer settings.
type CacheConfig struct {
	MaxOrderbookDepth int           `mapstructure:"max_orderbook_depth"`
	TradeHistorySize  int           `mapstructure:"trade_history_size"`
	CleanupInterval   time.Duration `mapstructure:"cleanup_interval"`
}

// RateConfig holds rate limiter settings.
type RateConfig struct {
	DefaultRPS        int     `mapstructure:"default_rps"`
	DefaultMaxStreams int     `mapstructure:"default_max_streams"`
	BurstMultiplier   float64 `mapstructure:"burst_multiplier"`
}

// DatabaseConfig holds MySQL database settings.
type DatabaseConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	User            string        `mapstructure:"user"`
	Password        string        `mapstructure:"password"`
	Database        string        `mapstructure:"database"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// RedisConfig holds Redis settings.
type RedisConfig struct {
	Addr         string        `mapstructure:"addr"`
	Password     string        `mapstructure:"password"`
	DB           int           `mapstructure:"db"`
	PoolSize     int           `mapstructure:"pool_size"`
	MinIdleConns int           `mapstructure:"min_idle_conns"`
	DialTimeout  time.Duration `mapstructure:"dial_timeout"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// LoggerConfig holds logger settings.
type LoggerConfig struct {
	Level       string `mapstructure:"level"`
	Development bool   `mapstructure:"development"`
	Encoding    string `mapstructure:"encoding"`
}

// Load loads configuration from file and environment.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read config file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/hl-relay")
	}

	// Read environment variables
	v.AutomaticEnv()
	v.SetEnvPrefix("HL_RELAY")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file not found, use defaults
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.http_port", 8080)
	v.SetDefault("server.grpc_port", 50051)
	v.SetDefault("server.host", "0.0.0.0")

	// Upstream defaults
	v.SetDefault("upstream.health_check_interval", "5s")
	v.SetDefault("upstream.reconnect_max_delay", "30s")
	v.SetDefault("upstream.reconnect_base_delay", "100ms")

	// Fanout defaults
	v.SetDefault("fanout.subscriber_buffer_size", 500)
	v.SetDefault("fanout.slow_consumer_threshold", 1000)
	v.SetDefault("fanout.zombie_timeout", "60s")

	// Cache defaults
	v.SetDefault("cache.max_orderbook_depth", 100)
	v.SetDefault("cache.trade_history_size", 1000)
	v.SetDefault("cache.cleanup_interval", "5m")

	// Rate limiter defaults
	v.SetDefault("rate.default_rps", 100)
	v.SetDefault("rate.default_max_streams", 10)
	v.SetDefault("rate.burst_multiplier", 2.0)

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 3306)
	v.SetDefault("database.max_open_conns", 100)
	v.SetDefault("database.max_idle_conns", 10)
	v.SetDefault("database.conn_max_lifetime", "1h")

	// Redis defaults
	v.SetDefault("redis.addr", "localhost:6379")
	v.SetDefault("redis.pool_size", 100)
	v.SetDefault("redis.min_idle_conns", 10)
	v.SetDefault("redis.dial_timeout", "5s")
	v.SetDefault("redis.read_timeout", "3s")
	v.SetDefault("redis.write_timeout", "3s")

	// Logger defaults
	v.SetDefault("logger.level", "info")
	v.SetDefault("logger.development", false)
	v.SetDefault("logger.encoding", "json")
}
