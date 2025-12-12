# 技術開發規範與套件選型文件

> 本文件定義 HL Relay Service 專案的技術開發規範與第三方套件選型標準。
> 目標是選擇**穩定、高效、維護良好**的套件，加速開發並確保生產環境穩定性。

---

## 1. 套件選型原則

在選擇第三方套件時，遵循以下原則：

| 原則 | 說明 |
|------|------|
| **穩定性** | 優先選擇已被大規模生產環境驗證的套件 |
| **效能** | 針對高頻交易場景，效能是首要考量 |
| **維護狀態** | 活躍維護、有明確的 issue 回應 |
| **社群規模** | GitHub stars、使用者數量、文件品質 |
| **相容性** | 與 Go 版本相容、無已知重大安全漏洞 |
| **簡潔性** | API 設計清晰、易於使用和測試 |

---

## 2. 核心套件清單

### 2.1 日誌（Logging）

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `go.uber.org/zap` | 21k+ stars | Uber 開源，極致效能，結構化日誌 |
| 備選 | `github.com/rs/zerolog` | 10k+ stars | Zero allocation，效能優異 |
| 不推薦 | `log/slog` (標準庫) | - | Go 1.21+ 內建，功能較基礎 |

#### 選擇理由：zap

- **效能**：比標準 log 快 10-100 倍
- **零記憶體分配**：在 hot path 上不產生 GC 壓力
- **結構化日誌**：支援 JSON 輸出，便於 ELK/Loki 整合
- **分級日誌**：支援 Debug/Info/Warn/Error/Fatal
- **採樣功能**：可配置採樣率，避免日誌風暴

#### 使用規範

```go
// 建議的初始化方式
import "go.uber.org/zap"

// Production 配置（JSON 格式）
logger, _ := zap.NewProduction()

// Development 配置（人類可讀）
logger, _ := zap.NewDevelopment()

// 使用 SugaredLogger 更方便
sugar := logger.Sugar()
sugar.Infow("Order placed",
    "symbol", "BTC-PERP",
    "size", 0.1,
    "latency_ms", 5.2,
)
```

---

### 2.2 內部訊息傳遞（Channel / PubSub）

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | 原生 `chan` + 自訂封裝 | - | 效能最佳，完全控制 |
| 推薦 | `github.com/coder/quartz` | 200+ stars | 時間相關測試工具 |
| 備選 | `github.com/asaskevich/EventBus` | 1.7k stars | 簡單 pub/sub 實作 |
| 備選 | `github.com/olebedev/emitter` | 500+ stars | 事件發射器模式 |

#### 選擇理由：原生 chan + 自訂封裝

對於 HFT 場景，直接使用 Go 原生 channel 是最佳選擇：

- **零額外開銷**：沒有第三方抽象層
- **完全控制**：可針對場景優化 buffer 大小
- **型別安全**：編譯期型別檢查

#### 建議的 PubSub 封裝模式

```go
// Topic-based fanout pattern
type Topic[T any] struct {
    mu          sync.RWMutex
    subscribers map[string]chan T
    bufferSize  int
}

func NewTopic[T any](bufferSize int) *Topic[T] {
    return &Topic[T]{
        subscribers: make(map[string]chan T),
        bufferSize:  bufferSize,
    }
}

func (t *Topic[T]) Subscribe(id string) <-chan T {
    t.mu.Lock()
    defer t.mu.Unlock()
    ch := make(chan T, t.bufferSize)
    t.subscribers[id] = ch
    return ch
}

func (t *Topic[T]) Publish(msg T) {
    t.mu.RLock()
    defer t.mu.RUnlock()
    for _, ch := range t.subscribers {
        select {
        case ch <- msg:
        default:
            // buffer full, drop or handle
        }
    }
}
```

---

### 2.3 記憶體管理（Memory / Object Pool）

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `sync.Pool` (標準庫) | - | Go 內建，最佳效能 |
| 備選 | `github.com/valyala/bytebufferpool` | 1k+ stars | 位元組緩衝池 |
| 備選 | `github.com/panjf2000/ants` | 12k+ stars | goroutine 池 |

#### 選擇理由：sync.Pool

- **標準庫**：無額外依賴
- **GC 友好**：與 Go runtime 深度整合
- **自動回收**：GC 時自動清理閒置物件

#### 使用規範

```go
import "sync"

// 定義物件池
var orderPool = sync.Pool{
    New: func() interface{} {
        return &Order{}
    },
}

// 獲取物件
func GetOrder() *Order {
    return orderPool.Get().(*Order)
}

// 歸還物件（記得重置狀態）
func PutOrder(o *Order) {
    o.Reset()  // 重要：清除舊資料
    orderPool.Put(o)
}
```

#### Goroutine Pool（可選）

對於大量短期 goroutine，考慮使用 `ants`：

```go
import "github.com/panjf2000/ants/v2"

// 建立固定大小的 goroutine 池
pool, _ := ants.NewPool(1000)
defer pool.Release()

// 提交任務
pool.Submit(func() {
    // 處理任務
})
```

---

### 2.4 MySQL 資料庫

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **Driver** | `github.com/go-sql-driver/mysql` | 14k+ stars | 官方推薦 driver |
| **ORM (推薦)** | `github.com/jmoiron/sqlx` | 16k+ stars | 輕量擴展，保留 SQL 控制 |
| ORM (備選) | `gorm.io/gorm` | 36k+ stars | 全功能 ORM，較重 |
| 遷移工具 | `github.com/golang-migrate/migrate` | 15k+ stars | 資料庫遷移 |

#### 選擇理由：sqlx

- **輕量**：只是 database/sql 的薄封裝
- **效能**：幾乎無額外開銷
- **靈活**：保留原生 SQL 的完整控制
- **便利**：自動 struct 映射

#### 使用規範

```go
import (
    "github.com/jmoiron/sqlx"
    _ "github.com/go-sql-driver/mysql"
)

// 連線配置
dsn := "user:password@tcp(localhost:3306)/hl_relay?parseTime=true&loc=Local"
db, err := sqlx.Connect("mysql", dsn)

// 連線池設定（重要！）
db.SetMaxOpenConns(100)           // 最大連線數
db.SetMaxIdleConns(10)            // 閒置連線數
db.SetConnMaxLifetime(time.Hour)  // 連線最大存活時間

// 查詢範例
type Tenant struct {
    ID        int64     `db:"id"`
    Name      string    `db:"name"`
    Status    string    `db:"status"`
    CreatedAt time.Time `db:"created_at"`
}

var tenant Tenant
err = db.Get(&tenant, "SELECT * FROM tenants WHERE id = ?", tenantID)

// 批量查詢
var tenants []Tenant
err = db.Select(&tenants, "SELECT * FROM tenants WHERE status = ?", "active")

// 命名參數
query := "INSERT INTO tenants (name, email) VALUES (:name, :email)"
result, err := db.NamedExec(query, map[string]interface{}{
    "name":  "Test Tenant",
    "email": "test@example.com",
})
```

#### 資料庫遷移

```go
import "github.com/golang-migrate/migrate/v4"

// 執行遷移
m, err := migrate.New(
    "file://migrations",
    "mysql://user:password@tcp(localhost:3306)/hl_relay",
)
m.Up()  // 升級到最新版本
```

---

### 2.5 Redis 快取

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `github.com/redis/go-redis/v9` | 20k+ stars | 官方維護，功能完整 |
| 備選 | `github.com/gomodule/redigo` | 10k+ stars | 較輕量，連線池需手動管理 |

#### 選擇理由：go-redis

- **官方維護**：Redis 官方團隊維護
- **功能完整**：支援 Cluster、Sentinel、Pipeline、Pub/Sub
- **Context 支援**：原生支援 context 取消
- **連線池**：內建高效連線池

#### 使用規範

```go
import "github.com/redis/go-redis/v9"

// 初始化客戶端
rdb := redis.NewClient(&redis.Options{
    Addr:         "localhost:6379",
    Password:     "",
    DB:           0,
    PoolSize:     100,              // 連線池大小
    MinIdleConns: 10,               // 最小閒置連線
    MaxRetries:   3,                // 重試次數
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
})

// 基本操作
ctx := context.Background()

// SET
err := rdb.Set(ctx, "key", "value", time.Hour).Err()

// GET
val, err := rdb.Get(ctx, "key").Result()

// Pipeline（批量操作）
pipe := rdb.Pipeline()
incr := pipe.Incr(ctx, "counter")
pipe.Expire(ctx, "counter", time.Hour)
_, err = pipe.Exec(ctx)

// Pub/Sub
pubsub := rdb.Subscribe(ctx, "channel")
defer pubsub.Close()

for msg := range pubsub.Channel() {
    fmt.Println(msg.Channel, msg.Payload)
}
```

---

### 2.6 JSON 序列化

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `github.com/json-iterator/go` | 13k+ stars | 效能比標準庫快 6x |
| 備選 | `github.com/bytedance/sonic` | 7k+ stars | 位元組跳動，需 amd64 |
| 備選 | `encoding/json` (標準庫) | - | 相容性最佳 |

#### 選擇理由：jsoniter

- **高效能**：比標準庫快 6 倍
- **100% 相容**：可直接替換 encoding/json
- **靈活配置**：支援多種編碼選項

#### 使用規範

```go
import jsoniter "github.com/json-iterator/go"

// 建議使用 ConfigCompatibleWithStandardLibrary
var json = jsoniter.ConfigCompatibleWithStandardLibrary

// 與 encoding/json 完全相容
data, err := json.Marshal(obj)
err = json.Unmarshal(data, &obj)

// 效能配置（更快但略不相容）
var jsonFast = jsoniter.ConfigFastest
```

---

### 2.7 HTTP 框架

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `github.com/gin-gonic/gin` | 79k+ stars | 效能優異，生態豐富 |
| 備選 | `github.com/labstack/echo` | 30k+ stars | API 設計優雅 |
| 備選 | `github.com/gofiber/fiber` | 34k+ stars | 極致效能，類 Express |

#### 選擇理由：Gin

- **效能**：基於 httprouter，極快
- **生態**：middleware 生態豐富
- **穩定**：大量生產環境驗證
- **文件**：完善的文件與範例

---

### 2.8 gRPC

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `google.golang.org/grpc` | 21k+ stars | Google 官方 |
| 輔助 | `github.com/grpc-ecosystem/go-grpc-middleware` | 6k+ stars | 攔截器集合 |
| 輔助 | `github.com/fullstorydev/grpcurl` | 11k+ stars | gRPC curl 工具 |

---

### 2.9 WebSocket

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `nhooyr.io/websocket` | 4k+ stars | 現代設計，context 支援 |
| 備選 | `github.com/gorilla/websocket` | 22k+ stars | 維護模式，但穩定 |

#### 選擇理由：nhooyr.io/websocket

- **現代 API**：原生 context 支援
- **更小**：程式碼更精簡
- **主動維護**：持續更新

---

### 2.10 設定管理

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `github.com/spf13/viper` | 27k+ stars | 功能完整，支援多種格式 |
| 備選 | `github.com/kelseyhightower/envconfig` | 5k+ stars | 專注環境變數 |

---

### 2.11 錯誤處理

| 套件 | 選擇 | GitHub | 說明 |
|------|------|--------|------|
| **推薦** | `github.com/pkg/errors` | 8k+ stars | Stack trace 支援 |
| 推薦 | 標準庫 `errors` + `fmt.Errorf` | - | Go 1.13+ 原生支援 |

---

### 2.12 測試工具

| 類型 | 套件 | 說明 |
|------|------|------|
| 斷言 | `github.com/stretchr/testify` | 斷言、Mock、Suite |
| Mock | `github.com/golang/mock` | 介面 Mock 產生 |
| HTTP 測試 | `net/http/httptest` | 標準庫 HTTP 測試 |
| 效能測試 | `testing.B` | 標準庫 benchmark |

---

### 2.13 可觀測性

| 類型 | 套件 | 說明 |
|------|------|------|
| Metrics | `github.com/prometheus/client_golang` | Prometheus 指標 |
| Tracing | `go.opentelemetry.io/otel` | OpenTelemetry 追蹤 |
| Profiling | `net/http/pprof` | 標準庫效能分析 |

---

## 3. 完整套件清單（go.mod 範例）

```go
module github.com/yourorg/hl-relay

go 1.21

require (
    // Logging
    go.uber.org/zap v1.27.0
    
    // Database
    github.com/jmoiron/sqlx v1.4.0
    github.com/go-sql-driver/mysql v1.8.1
    github.com/golang-migrate/migrate/v4 v4.17.0
    
    // Redis
    github.com/redis/go-redis/v9 v9.5.1
    
    // JSON
    github.com/json-iterator/go v1.1.12
    
    // HTTP/gRPC
    github.com/gin-gonic/gin v1.10.0
    google.golang.org/grpc v1.64.0
    
    // WebSocket
    nhooyr.io/websocket v1.8.11
    
    // Config
    github.com/spf13/viper v1.18.2
    
    // Goroutine Pool
    github.com/panjf2000/ants/v2 v2.9.1
    
    // Testing
    github.com/stretchr/testify v1.9.0
    
    // Metrics
    github.com/prometheus/client_golang v1.19.1
)
```

---

## 4. 效能基準參考

| 操作 | 套件 | 效能 |
|------|------|------|
| JSON Marshal | jsoniter vs encoding/json | ~6x 快 |
| Logging | zap vs logrus | ~10x 快 |
| HTTP Router | gin vs net/http | ~40x 快 |
| MySQL Query | sqlx vs gorm | ~2-3x 快 |

---

## 5. 版本管理

- 使用 Go Modules 管理依賴
- 定期執行 `go mod tidy` 清理未使用依賴
- 使用 `go mod verify` 驗證依賴完整性
- 考慮使用 Dependabot 或 Renovate 自動更新

---

## 附錄：相關文件

* [主架構文件](./hl-relay-architecture.md)
* [Relay Core 設計文件](./relay-core-design.md)
* [Control Plane 設計文件](./control-plane-design.md)
