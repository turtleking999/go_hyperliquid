# HL Relay Service 技術文件

本目錄包含 Hyperliquid 全自建中繼服務（HL Relay Service）的技術設計文件。

## 文件目錄

| 文件 | 說明 | 狀態 |
|------|------|------|
| [hl-relay-architecture.md](./hl-relay-architecture.md) | 主架構設計文件 v0.1 | ✅ 完成 |
| [relay-core-design.md](./relay-core-design.md) | Relay Core (Data Plane) 詳細設計 | ✅ 完成 |
| [control-plane-design.md](./control-plane-design.md) | Control Plane 詳細設計 | ✅ 完成 |

## 系統概述

HL Relay Service 是一個全自建的 Hyperliquid 中繼服務，角色類似 Dwellir，提供：

- **低延遲**：針對 HFT 優化的資料傳輸
- **高可用**：多節點備援與自動 failover
- **多租戶**：支援多個客戶同時使用
- **可計費**：完整的使用量追蹤與計費支援
- **可觀測**：完整的 metrics、logging、alerting

## 技術選型

- **程式語言**：Go
- **資料庫**：MySQL
- **快取**：In-memory / Redis
- **協定**：gRPC, WebSocket, HTTP
- **部署**：Docker / Kubernetes

## 架構概覽

```
┌─────────────────────────────────────────────────────────────┐
│                    Upstream Layer                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │ L1 Node (1)  │  │ L1 Node (2)  │  │ EVM Node    │       │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘       │
│         └─────────────┬───┴─────────────────┘               │
│                       ▼                                      │
│              ┌────────────────┐                              │
│              │  HL Gateway    │                              │
│              │ (REST/WS/gRPC) │                              │
│              └────────┬───────┘                              │
└───────────────────────┼─────────────────────────────────────┘
                        │
┌───────────────────────┼─────────────────────────────────────┐
│                       ▼                                      │
│              ┌────────────────┐                              │
│              │  Relay Core    │                              │
│              │ ┌────────────┐ │                              │
│              │ │  Upstream  │ │                              │
│              │ │  Manager   │ │                              │
│              │ ├────────────┤ │                              │
│              │ │  Fanout    │ │                              │
│              │ │  Hub       │ │                              │
│              │ ├────────────┤ │                              │
│              │ │  Cache     │ │                              │
│              │ ├────────────┤ │                              │
│              │ │  Rate      │ │                              │
│              │ │  Limiter   │ │                              │
│              │ └────────────┘ │                              │
│              └────────────────┘                              │
│                       │                                      │
│    ┌─────────────────┼──────────────────┐                   │
│    │                 │                   │                   │
│    ▼                 ▼                   ▼                   │
│ ┌──────┐        ┌──────┐           ┌──────┐                 │
│ │ gRPC │        │  WS  │           │ HTTP │                 │
│ │ API  │        │ API  │           │ API  │                 │
│ └──────┘        └──────┘           └──────┘                 │
│                    HL Relay Service                          │
└─────────────────────────────────────────────────────────────┘
                        │
        ┌───────────────┼───────────────┐
        │               │               │
        ▼               ▼               ▼
   ┌─────────┐    ┌─────────┐    ┌─────────┐
   │ HFT Bot │    │ Internal │   │ External │
   │         │    │ System   │   │ Customer │
   └─────────┘    └─────────┘    └─────────┘
```

## 開發路線圖

### Phase 1: MVP（目前）
- [x] 架構設計文件
- [ ] 基本 Relay Core 實現
- [ ] 單一 symbol orderbook streaming
- [ ] 基本 API Key 認證

### Phase 2: 多租戶
- [ ] 完整 Tenant 管理
- [ ] Plan & Quota 系統
- [ ] 使用量統計

### Phase 3: 生產就緒
- [ ] 多 Gateway failover
- [ ] 完整 metrics & alerting
- [ ] 壓力測試與效能優化

### Phase 4: 擴展
- [ ] 多 region 支援
- [ ] 交易代理功能
- [ ] 歷史資料服務

## 聯絡方式

如有問題或建議，請開 Issue 或聯繫維護團隊。
