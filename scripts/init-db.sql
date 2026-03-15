-- CloudAI Fusion Database Initialization
-- PostgreSQL 16+

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- =============================================================================
-- Users & Authentication
-- =============================================================================
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(64) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    display_name VARCHAR(128),
    avatar_url TEXT,
    role VARCHAR(32) NOT NULL DEFAULT 'viewer',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    scopes TEXT[] DEFAULT '{}',
    expires_at TIMESTAMP WITH TIME ZONE,
    last_used_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- Cloud Providers
-- =============================================================================
CREATE TABLE IF NOT EXISTS cloud_providers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) NOT NULL,
    type VARCHAR(32) NOT NULL, -- aliyun, aws, azure, gcp, huawei, tencent
    region VARCHAR(64) NOT NULL,
    credentials_encrypted BYTEA,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    config JSONB DEFAULT '{}',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- Clusters
-- =============================================================================
CREATE TABLE IF NOT EXISTS clusters (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) NOT NULL,
    provider_id UUID REFERENCES cloud_providers(id),
    kubernetes_version VARCHAR(32),
    endpoint VARCHAR(512),
    ca_certificate TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    node_count INTEGER DEFAULT 0,
    gpu_count INTEGER DEFAULT 0,
    total_cpu_millicores BIGINT DEFAULT 0,
    total_memory_bytes BIGINT DEFAULT 0,
    total_gpu_memory_bytes BIGINT DEFAULT 0,
    labels JSONB DEFAULT '{}',
    annotations JSONB DEFAULT '{}',
    config JSONB DEFAULT '{}',
    health_check_at TIMESTAMP WITH TIME ZONE,
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cluster_nodes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'worker',
    status VARCHAR(32) NOT NULL DEFAULT 'ready',
    ip_address VARCHAR(45),
    os_image VARCHAR(128),
    kernel_version VARCHAR(64),
    container_runtime VARCHAR(64),
    cpu_capacity_millicores INTEGER DEFAULT 0,
    memory_capacity_bytes BIGINT DEFAULT 0,
    gpu_type VARCHAR(64),
    gpu_count INTEGER DEFAULT 0,
    gpu_memory_bytes BIGINT DEFAULT 0,
    labels JSONB DEFAULT '{}',
    conditions JSONB DEFAULT '[]',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- AI Workloads & Scheduling
-- =============================================================================
CREATE TABLE IF NOT EXISTS workloads (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(256) NOT NULL,
    namespace VARCHAR(128) DEFAULT 'default',
    cluster_id UUID NOT NULL REFERENCES clusters(id),
    type VARCHAR(32) NOT NULL, -- training, inference, fine-tuning, batch
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    priority INTEGER DEFAULT 0,
    framework VARCHAR(32), -- pytorch, tensorflow, jax
    model_name VARCHAR(256),
    resource_request JSONB NOT NULL DEFAULT '{}',
    resource_limit JSONB DEFAULT '{}',
    gpu_type_required VARCHAR(64),
    gpu_count_required INTEGER DEFAULT 0,
    gpu_memory_required BIGINT DEFAULT 0,
    scheduling_policy JSONB DEFAULT '{}',
    assigned_nodes TEXT[] DEFAULT '{}',
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    error_message TEXT,
    metrics JSONB DEFAULT '{}',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- Security & Audit
-- =============================================================================
CREATE TABLE IF NOT EXISTS security_policies (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) NOT NULL,
    description TEXT,
    type VARCHAR(32) NOT NULL, -- network, rbac, pod-security, compliance
    scope VARCHAR(32) NOT NULL DEFAULT 'cluster',
    cluster_id UUID REFERENCES clusters(id),
    rules JSONB NOT NULL DEFAULT '[]',
    enforcement VARCHAR(16) NOT NULL DEFAULT 'enforce', -- enforce, audit, warn
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id),
    action VARCHAR(64) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id UUID,
    resource_name VARCHAR(256),
    cluster_id UUID REFERENCES clusters(id),
    details JSONB DEFAULT '{}',
    ip_address VARCHAR(45),
    user_agent TEXT,
    status VARCHAR(16) NOT NULL DEFAULT 'success',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

-- Partition audit_logs by month for performance
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX idx_audit_logs_user_action ON audit_logs(user_id, action);

-- =============================================================================
-- Monitoring & Alerts
-- =============================================================================
CREATE TABLE IF NOT EXISTS alert_rules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(128) NOT NULL,
    description TEXT,
    cluster_id UUID REFERENCES clusters(id),
    severity VARCHAR(16) NOT NULL DEFAULT 'warning', -- critical, warning, info
    condition_expr TEXT NOT NULL,
    threshold DOUBLE PRECISION,
    duration_seconds INTEGER DEFAULT 300,
    notification_channels TEXT[] DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS alert_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    rule_id UUID NOT NULL REFERENCES alert_rules(id),
    cluster_id UUID REFERENCES clusters(id),
    severity VARCHAR(16) NOT NULL,
    message TEXT NOT NULL,
    labels JSONB DEFAULT '{}',
    annotations JSONB DEFAULT '{}',
    status VARCHAR(16) NOT NULL DEFAULT 'firing', -- firing, resolved
    fired_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMP WITH TIME ZONE,
    acknowledged_by UUID REFERENCES users(id),
    acknowledged_at TIMESTAMP WITH TIME ZONE
);

-- =============================================================================
-- Cost Management
-- =============================================================================
CREATE TABLE IF NOT EXISTS cost_records (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cluster_id UUID NOT NULL REFERENCES clusters(id),
    provider_id UUID NOT NULL REFERENCES cloud_providers(id),
    resource_type VARCHAR(64) NOT NULL,
    resource_name VARCHAR(256),
    usage_amount DOUBLE PRECISION NOT NULL,
    usage_unit VARCHAR(32) NOT NULL,
    cost_amount DECIMAL(12, 4) NOT NULL,
    cost_currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    period_start TIMESTAMP WITH TIME ZONE NOT NULL,
    period_end TIMESTAMP WITH TIME ZONE NOT NULL,
    tags JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cost_records_period ON cost_records(period_start, period_end);
CREATE INDEX idx_cost_records_cluster ON cost_records(cluster_id, period_start);

-- =============================================================================
-- Seed Data
-- =============================================================================
INSERT INTO users (id, username, email, password_hash, display_name, role) VALUES
    ('00000000-0000-0000-0000-000000000001', 'admin', 'admin@cloudai-fusion.io',
     '$2a$10$WryHuCDJYLl02w6e6twZDeZ3IjsoiKC//RxuQC04iAw77q7DsD6ba', 'System Admin', 'admin')
ON CONFLICT (username) DO NOTHING;
