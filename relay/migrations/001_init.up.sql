-- 001_init.up.sql
-- Initial database schema for HL Relay Service

-- Tenants table
CREATE TABLE IF NOT EXISTS tenants (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    status ENUM('active', 'suspended', 'deleted') NOT NULL DEFAULT 'active',
    metadata JSON,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    INDEX idx_status (status),
    INDEX idx_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Plans table
CREATE TABLE IF NOT EXISTS plans (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT,
    max_concurrent_streams INT UNSIGNED NOT NULL DEFAULT 10,
    max_rps INT UNSIGNED NOT NULL DEFAULT 100,
    max_symbols INT UNSIGNED NOT NULL DEFAULT 50,
    max_daily_requests BIGINT UNSIGNED,
    monthly_price DECIMAL(10, 2) NOT NULL DEFAULT 0.00,
    status ENUM('active', 'deprecated') NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- API Keys table
CREATE TABLE IF NOT EXISTS api_keys (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    key_prefix VARCHAR(8) NOT NULL,
    key_hash VARCHAR(64) NOT NULL,
    name VARCHAR(100),
    status ENUM('active', 'revoked', 'expired') NOT NULL DEFAULT 'active',
    permissions JSON,
    expires_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME,
    
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (plan_id) REFERENCES plans(id),
    UNIQUE INDEX idx_key_hash (key_hash),
    INDEX idx_tenant_status (tenant_id, status),
    INDEX idx_prefix (key_prefix)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Daily usage statistics table
CREATE TABLE IF NOT EXISTS usage_daily (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT UNSIGNED NOT NULL,
    api_key_id BIGINT UNSIGNED NOT NULL,
    usage_date DATE NOT NULL,
    total_requests BIGINT UNSIGNED NOT NULL DEFAULT 0,
    total_messages BIGINT UNSIGNED NOT NULL DEFAULT 0,
    peak_concurrent_streams INT UNSIGNED NOT NULL DEFAULT 0,
    avg_latency_ms DECIMAL(10, 2),
    error_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    
    FOREIGN KEY (tenant_id) REFERENCES tenants(id),
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id),
    UNIQUE INDEX idx_key_date (api_key_id, usage_date),
    INDEX idx_tenant_date (tenant_id, usage_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Audit logs table
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT UNSIGNED,
    api_key_id BIGINT UNSIGNED,
    action VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50),
    resource_id VARCHAR(100),
    details JSON,
    ip_address VARCHAR(45),
    user_agent VARCHAR(500),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    INDEX idx_tenant (tenant_id),
    INDEX idx_action_time (action, created_at),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Insert default plans
INSERT INTO plans (name, description, max_concurrent_streams, max_rps, max_symbols, monthly_price) VALUES
('free', 'Free plan for development and testing', 5, 10, 10, 0.00),
('pro', 'Professional plan for small teams', 50, 100, 50, 99.00),
('enterprise', 'Enterprise plan with high limits', 500, 1000, 200, 499.00);
