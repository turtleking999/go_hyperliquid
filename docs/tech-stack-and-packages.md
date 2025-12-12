# 技術開發規範與套件選型文件

> 本文件定義 HL Relay Service 專案的技術開發規範與第三方套件選型標準。
> 目標是選擇**穩定、高效、維護良好**的套件，加速開發並確保生產環境穩定性。

---

## 1. 套件選型原則

| 原則 | 說明 |
|------|------|
| **穩定性** | 優先選擇已被大規模生產環境驗證的套件 |
| **效能** | 針對高頻交易場景，效能是首要考量 |
| **維護狀態** | 活躍維護、有明確的 issue 回應 |
| **簡潔性** | API 設計清晰、易於使用和測試 |

---

## 2. 核心套件清單

### 2.1 日誌（Logging）

**選用**：`go.uber.org/zap`

- **效能**：比標準 log 快 10-100 倍
- **零記憶體分配**：在 hot path 上不產生 GC 壓力
- **結構化日誌**：支援 JSON 輸出，便於 ELK/Loki 整合

```go
import "go.uber.org/zap"

// Production 配置
logger, _ := zap.NewProduction()

// 使用 SugaredLogger
sugar := logger.Sugar()
sugar.Infow("Order placed",
    "symbol", "BTC-PERP",
    "size", 0.1,
    "latency_ms", 5.2,
)
```

---

### 2.2 內部訊息傳遞（PubSub）

**選用**：`github.com/olebedev/emitter`

- **事件發射器模式**：簡潔的 pub/sub 實作
- **支援萬用字元**：可訂閱 `order.*` 等模式
- **非同步支援**：支援 goroutine 發送

```go
import "github.com/olebedev/emitter"

e := emitter.New(10)  // buffer size

// 訂閱
go func() {
    for event := range e.On("order:placed") {
        order := event.Args[0].(*Order)
        // 處理訂單
    }
}()

// 發送
e.Emit("order:placed", order)
```

---

### 2.3 記憶體管理（Memory / Buffer / Pool）

#### Buffer Pool

**選用**：`github.com/valyala/bytebufferpool`

- **高效**：專為高頻場景優化
- **零分配**：重複使用 buffer

```go
import "github.com/valyala/bytebufferpool"

// 獲取 buffer
buf := bytebufferpool.Get()

// 使用
buf.WriteString("data")

// 歸還
bytebufferpool.Put(buf)
```

#### 物件池（計算暫存）

**選用**：`sync.Pool`（標準庫）

```go
var orderPool = sync.Pool{
    New: func() interface{} {
        return &Order{}
    },
}

// 獲取
order := orderPool.Get().(*Order)

// 歸還（記得重置）
order.Reset()
orderPool.Put(order)
```

#### Goroutine Pool（任務併發控制）

**選用**：`github.com/panjf2000/ants/v2`

```go
import "github.com/panjf2000/ants/v2"

pool, _ := ants.NewPool(1000)
defer pool.Release()

pool.Submit(func() {
    // 處理任務
})
```

---

### 2.4 序列化

#### 內部跨服務通訊

**選用**：`FlatBuffers`（Google）

- **零拷貝**：直接讀取 buffer，無需反序列化
- **極致效能**：比 Protobuf 更快
- **記憶體效率**：無額外記憶體分配

> **規則**：內部服務間通訊**全程使用 FlatBuffers**，不經過 JSON。

```go
// 使用 flatc 產生 Go 程式碼
// flatc --go schema.fbs

import flatbuffers "github.com/google/flatbuffers/go"

// 建立 builder
builder := flatbuffers.NewBuilder(256)

// 建立物件...
```

#### 外部 API / 人類可讀

**選用**：`github.com/json-iterator/go`

- **效能**：比標準庫快 6 倍
- **100% 相容**：可直接替換 encoding/json

> **規則**：JSON 僅用於**對外 API** 和**需要人類閱讀**的場景。

```go
import jsoniter "github.com/json-iterator/go"

var json = jsoniter.ConfigCompatibleWithStandardLibrary

data, _ := json.Marshal(obj)
json.Unmarshal(data, &obj)
```

---

### 2.5 MySQL 資料庫

**選用**：`github.com/go-sql-driver/mysql`

- **官方推薦**：MySQL 官方推薦 driver
- **穩定可靠**：大規模生產環境驗證
- **效能優異**：原生 SQL 控制

```go
import (
    "database/sql"
    _ "github.com/go-sql-driver/mysql"
)

dsn := "user:password@tcp(localhost:3306)/hl_relay?parseTime=true"
db, _ := sql.Open("mysql", dsn)

// 連線池設定
db.SetMaxOpenConns(100)
db.SetMaxIdleConns(10)
db.SetConnMaxLifetime(time.Hour)

// 查詢
rows, _ := db.Query("SELECT * FROM tenants WHERE status = ?", "active")
```

---

### 2.6 Redis 快取

**選用**：`github.com/redis/go-redis/v9`

- **官方維護**：Redis 官方團隊維護
- **功能完整**：支援 Cluster、Sentinel、Pipeline、Pub/Sub
- **內建連線池**：高效連線管理

```go
import "github.com/redis/go-redis/v9"

rdb := redis.NewClient(&redis.Options{
    Addr:         "localhost:6379",
    PoolSize:     100,
    MinIdleConns: 10,
})

ctx := context.Background()
rdb.Set(ctx, "key", "value", time.Hour)
val, _ := rdb.Get(ctx, "key").Result()
```

---

### 2.7 HTTP 框架

**選用**：`github.com/gofiber/fiber/v2`

- **極致效能**：基於 fasthttp，比 net/http 快 10x
- **低記憶體**：記憶體使用極低
- **Express 風格**：熟悉的 API 設計

```go
import "github.com/gofiber/fiber/v2"

app := fiber.New()

app.Get("/api/orderbook", func(c *fiber.Ctx) error {
    symbol := c.Query("symbol")
    return c.JSON(fiber.Map{"symbol": symbol})
})

app.Listen(":3000")
```

---

### 2.8 gRPC

**選用**：`google.golang.org/grpc`

- **Google 官方**：穩定可靠
- **高效能**：二進位協議，低延遲
- **Streaming**：支援雙向流

```go
import "google.golang.org/grpc"

// Server
lis, _ := net.Listen("tcp", ":50051")
s := grpc.NewServer()
pb.RegisterMarketDataServer(s, &server{})
s.Serve(lis)

// Client
conn, _ := grpc.Dial("localhost:50051", grpc.WithInsecure())
client := pb.NewMarketDataClient(conn)
```

---

### 2.9 WebSocket

**選用**：`nhooyr.io/websocket`

- **現代 API**：原生 context 支援
- **輕量**：程式碼精簡
- **主動維護**：持續更新

```go
import "nhooyr.io/websocket"

// Server
http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
    conn, _ := websocket.Accept(w, r, nil)
    defer conn.Close(websocket.StatusNormalClosure, "")
    
    for {
        _, msg, _ := conn.Read(r.Context())
        conn.Write(r.Context(), websocket.MessageText, msg)
    }
})
```

---

### 2.10 設定管理

**選用**：`github.com/spf13/viper`

- **功能完整**：支援 YAML、JSON、TOML、環境變數
- **熱重載**：支援配置檔變更監控
- **廣泛使用**：社群支援良好

```go
import "github.com/spf13/viper"

viper.SetConfigName("config")
viper.SetConfigType("yaml")
viper.AddConfigPath(".")
viper.ReadInConfig()

port := viper.GetInt("server.port")
```

---

### 2.11 錯誤處理

**選用**：`github.com/pkg/errors`

- **Stack trace**：錯誤發生時記錄完整堆疊
- **錯誤包裝**：支援錯誤鏈

```go
import "github.com/pkg/errors"

// 包裝錯誤
err := errors.Wrap(err, "failed to connect")

// 帶格式
err := errors.Wrapf(err, "failed to process order %s", orderID)

// 獲取堆疊
fmt.Printf("%+v\n", err)
```

---

### 2.12 測試工具

| 套件 | 說明 |
|------|------|
| `github.com/stretchr/testify` | 斷言、Mock、Suite |
| `github.com/golang/mock` | 介面 Mock 產生 |
| `net/http/httptest` | 標準庫 HTTP 測試 |
| `testing.B` | 標準庫 benchmark |

---

### 2.13 可觀測性

| 套件 | 說明 |
|------|------|
| `github.com/prometheus/client_golang` | Prometheus 指標 |
| `go.opentelemetry.io/otel` | OpenTelemetry 追蹤 |
| `net/http/pprof` | 標準庫效能分析 |

---

## 3. 完整套件清單（go.mod）

```go
module github.com/yourorg/hl-relay

go 1.21

require (
    // Logging
    go.uber.org/zap v1.27.0
    
    // PubSub
    github.com/olebedev/emitter v0.0.0-20190110104742-e8d1457e6aee
    
    // Memory
    github.com/valyala/bytebufferpool v1.0.0
    github.com/panjf2000/ants/v2 v2.9.1
    
    // Serialization
    github.com/google/flatbuffers v24.3.25
    github.com/json-iterator/go v1.1.12
    
    // Database
    github.com/go-sql-driver/mysql v1.8.1
    github.com/redis/go-redis/v9 v9.5.1
    
    // HTTP/gRPC/WebSocket
    github.com/gofiber/fiber/v2 v2.52.4
    google.golang.org/grpc v1.64.0
    nhooyr.io/websocket v1.8.11
    
    // Config
    github.com/spf13/viper v1.18.2
    
    // Error Handling
    github.com/pkg/errors v0.9.1
    
    // Testing
    github.com/stretchr/testify v1.9.0
    
    // Metrics
    github.com/prometheus/client_golang v1.19.1
)
```

---

## 4. 序列化策略總結

```
┌─────────────────────────────────────────────────────────┐
│                   HL Relay Service                       │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  內部服務通訊                    對外 API               │
│  ┌─────────────┐                ┌─────────────┐        │
│  │ FlatBuffers │                │    JSON     │        │
│  │   (二進位)   │                │ (jsoniter)  │        │
│  └─────────────┘                └─────────────┘        │
│        │                              │                 │
│        ▼                              ▼                 │
│  ┌─────────────┐                ┌─────────────┐        │
│  │ Service A   │                │  外部客戶   │        │
│  │ Service B   │                │  Web UI     │        │
│  │ Service C   │                │  調試工具   │        │
│  └─────────────┘                └─────────────┘        │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

**規則**：
- ✅ 內部跨服務通訊：**FlatBuffers**
- ✅ 對外 REST API：**JSON (jsoniter)**
- ✅ 日誌輸出：**JSON (zap)**
- ❌ 禁止在內部服務間使用 JSON 序列化

---

## 附錄：相關文件

* [主架構文件](./hl-relay-architecture.md)
* [Relay Core 設計文件](./relay-core-design.md)
* [Control Plane 設計文件](./control-plane-design.md)
