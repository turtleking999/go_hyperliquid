# HL Relay Service

A high-performance, multi-tenant market data relay service for Hyperliquid.

## Features

- **Low Latency**: Optimized for HFT scenarios
- **High Availability**: Automatic reconnection and failover
- **Multi-Tenant**: API key authentication with rate limiting
- **Fan-out Distribution**: Single upstream connection, multiple downstream subscribers
- **Caching**: Orderbook snapshots and trade history

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   HL Gateway (Upstream)                      │
└─────────────────────────────┬───────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    HL Relay Service                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐          │
│  │  Upstream   │  │   Fanout    │  │    Cache    │          │
│  │  Manager    │──│    Hub      │──│    Layer    │          │
│  └─────────────┘  └─────────────┘  └─────────────┘          │
│         │                │                │                  │
│         └────────────────┼────────────────┘                  │
│                          │                                   │
│  ┌─────────────┐  ┌──────┴──────┐  ┌─────────────┐          │
│  │    Auth     │  │    API      │  │ Rate Limit  │          │
│  │   Service   │──│   Server    │──│   Service   │          │
│  └─────────────┘  └─────────────┘  └─────────────┘          │
└─────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              │               │               │
              ▼               ▼               ▼
         ┌─────────┐    ┌─────────┐    ┌─────────┐
         │ Client  │    │ Client  │    │ Client  │
         └─────────┘    └─────────┘    └─────────┘
```

## Quick Start

### Prerequisites

- Go 1.21+
- MySQL 8.0+
- Redis 6.0+ (optional)

### Installation

```bash
# Clone the repository
git clone https://github.com/yourorg/go_hyperliquid.git
cd go_hyperliquid/relay

# Install dependencies
go mod download

# Run database migrations
mysql -u root -p < migrations/001_init.up.sql

# Copy and edit config
cp config.example.yaml config.yaml
vim config.yaml

# Run the service
go run cmd/relay/main.go
```

### Configuration

Configuration can be provided via:
- YAML file (`config.yaml`)
- Environment variables (prefixed with `HL_RELAY_`)

See `config.example.yaml` for all available options.

## API Reference

### Authentication

All API requests require an API key:

```bash
# Via header
curl -H "X-API-Key: hl_your_api_key" http://localhost:8080/v1/orderbook?symbol=BTC

# Via query parameter
curl "http://localhost:8080/v1/orderbook?symbol=BTC&api_key=hl_your_api_key"
```

### HTTP Endpoints

#### Get Orderbook Snapshot
```
GET /v1/orderbook?symbol=BTC
```

#### Get Recent Trades
```
GET /v1/trades?symbol=BTC&count=100
```

#### Get Available Symbols
```
GET /v1/symbols
```

#### Health Check
```
GET /health
```

### WebSocket

Connect to `/ws` with your API key:

```javascript
const ws = new WebSocket('wss://relay.example.com/ws');

// Authenticate
ws.send(JSON.stringify({
  op: 'auth',
  api_key: 'hl_your_api_key'
}));

// Subscribe to orderbook
ws.send(JSON.stringify({
  op: 'subscribe',
  channel: 'orderbook',
  symbol: 'BTC'
}));

// Receive updates
ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log(data);
};
```

## Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| AUTH_MISSING_KEY | 401 | API key not provided |
| AUTH_INVALID_KEY | 401 | Invalid API key |
| AUTH_EXPIRED_KEY | 401 | API key has expired |
| AUTH_REVOKED_KEY | 401 | API key has been revoked |
| AUTH_SUSPENDED_TENANT | 403 | Tenant account is suspended |
| QUOTA_EXCEEDED_RPS | 429 | Rate limit exceeded |
| QUOTA_EXCEEDED_STREAMS | 429 | Max concurrent streams exceeded |

## Development

### Project Structure

```
relay/
├── cmd/relay/          # Main entry point
├── internal/
│   ├── api/            # HTTP/WebSocket handlers
│   ├── auth/           # Authentication service
│   ├── cache/          # In-memory cache
│   ├── config/         # Configuration
│   ├── fanout/         # PubSub fanout
│   ├── logger/         # Logging wrapper
│   ├── models/         # Database models
│   ├── ratelimit/      # Rate limiting
│   └── upstream/       # Upstream connection manager
├── migrations/         # SQL migrations
├── pkg/types/          # Shared types
└── config.example.yaml # Example configuration
```

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o hl-relay ./cmd/relay
```

## License

Apache License, Version 2.0
