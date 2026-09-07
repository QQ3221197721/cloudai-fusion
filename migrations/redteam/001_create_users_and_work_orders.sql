-- Red Team Platform Database Schema v1.0
-- Run this migration BEFORE starting the application
--
-- Prerequisites:
--   - PostgreSQL 13+ 
--   - Extensions: pgcrypto, citext
--
-- Execute:
--   psql -U postgres -d redteam_db -f 001_create_users_and_work_orders.sql

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";   -- For UUID generation
CREATE EXTENSION IF NOT EXISTS "citext";     -- Case-insensitive text

-- ============================================================
-- Core Users Table (Authentication & Authorization)
-- ============================================================
CREATE TABLE IF NOT EXISTS redteam_users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Identity fields
    username CITEXT UNIQUE NOT NULL,
    email CITEXT UNIQUE NOT NULL,
    full_name VARCHAR(255),
    
    -- Security (bcrypt hashed passwords)
    password_hash TEXT NOT NULL,
    
    -- Role-based access control
    role VARCHAR(50) NOT NULL DEFAULT 'pentester', 
        -- Values: admin, pentester, auditor
    
    -- Permissions array (JSONB for flexibility)
    permissions JSONB DEFAULT '["read"]'::jsonb,
    
    -- Account status
    active BOOLEAN DEFAULT true,
    last_login TIMESTAMPTZ,
    failed_login_attempts INTEGER DEFAULT 0,
    locked_until TIMESTAMPTZ,
    
    -- Audit trail
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    created_by UUID REFERENCES redteam_users(id),
    
    -- Indexes
    CONSTRAINT chk_valid_role CHECK (role IN ('admin', 'pentester', 'auditor'))
);

CREATE INDEX idx_username ON redteam_users(username);
CREATE INDEX idx_email ON redteam_users(email);
CREATE INDEX idx_active ON redteam_users(active);

COMMENT ON TABLE redteam_users IS 'Core authentication and authorization users';

-- ============================================================
-- Work Orders Table (Authorization Requests)
-- ============================================================
CREATE TABLE IF NOT EXISTS redteam_work_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Requester information
    user_id UUID NOT NULL REFERENCES redteam_users(id) ON DELETE CASCADE,
    company_name VARCHAR(255) NOT NULL,
    contact_email VARCHAR(255) NOT NULL,
    justification TEXT NOT NULL,
    
    -- Target systems (approved IPs/CIDR ranges)
    target_list JSONB NOT NULL DEFAULT '[]'::jsonb,
    
    -- Authorization documents (S3/GCS URLs)
    authorization_letter_url TEXT,
    legal_contract_url TEXT,
    
    -- Status workflow
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    priority_level INTEGER DEFAULT 2,  
        -- 1=critical, 2=high, 3=normal, 4=low
    
    -- Timestamps
    submitted_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    reviewed_at TIMESTAMPTZ,
    approved_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    
    -- Admin review
    reviewer_id UUID REFERENCES redteam_users(id),
    review_comments TEXT,
    rejection_reason TEXT,
    
    -- Check constraints
    CONSTRAINT chk_valid_priority CHECK (priority_level BETWEEN 1 AND 4),
    CONSTRAINT chk_valid_status CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled', 'expired'))
);

CREATE INDEX idx_work_order_status ON redteam_work_orders(status);
CREATE INDEX idx_work_order_submitted_at ON redteam_work_orders(submitted_at);
CREATE INDEX idx_work_order_user_id ON redteam_work_orders(user_id);

COMMENT ON TABLE redteam_work_orders IS 'Penetration test authorization requests';

-- ============================================================
-- Attack Campaigns Table
-- ============================================================
CREATE TABLE IF NOT EXISTS attack_campaigns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Basic info
    name VARCHAR(255) NOT NULL,
    description TEXT,
    work_order_id UUID REFERENCES redteam_work_orders(id) ON DELETE SET NULL,
    
    -- Execution details
    attack_types TEXT[] NOT NULL DEFAULT '{}',
    targets TEXT[] NOT NULL DEFAULT '{}',
    
    -- Status tracking
    status VARCHAR(50) NOT NULL DEFAULT 'scheduled',
        -- Values: scheduled, running, completed, failed, cancelled
    
    progress_percentage INTEGER DEFAULT 0,
    
    -- Timestamps
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    
    -- Performance metrics
    total_duration INTERVAL,
    findings_count INTEGER DEFAULT 0,
    
    -- Check constraints
    CONSTRAINT chk_progress_range CHECK (progress_percentage >= 0 AND progress_percentage <= 100)
);

CREATE INDEX idx_campaign_status ON attack_campaigns(status);
CREATE INDEX idx_campaign_started_at ON attack_campaigns(started_at);
CREATE INDEX idx_campaign_completed_at ON attack_campaigns(completed_at);
CREATE INDEX idx_campaign_work_order ON attack_campaigns(work_order_id);

COMMENT ON TABLE attack_campaigns IS 'Attack campaign execution records';

-- ============================================================
-- Findings Table (Vulnerability Results)
-- ============================================================
CREATE TABLE IF NOT EXISTS findings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    -- Reference tables
    campaign_id UUID NOT NULL REFERENCES attack_campaigns(id) ON DELETE CASCADE,
    
    -- Finding details
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    severity VARCHAR(50) NOT NULL,
        -- Values: critical, high, medium, low, informational
    cvss_score DECIMAL(3,1),
    
    -- Affected assets
    affected_asset VARCHAR(255),
    asset_type VARCHAR(50),
    
    -- Evidence collection
    evidence_urls JSONB DEFAULT '[]'::jsonb,
    
    -- Remediation guidance
    remediation TEXT,
    references TEXT[],
    
    -- Status
    verified BOOLEAN DEFAULT false,
    patched BOOLEAN DEFAULT false,
    
    -- Timestamps
    discovered_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    
    -- Check constraints
    CONSTRAINT chk_valid_severity CHECK (severity IN ('critical', 'high', 'medium', 'low', 'informational')),
    CONSTRAINT chk_cvss_range CHECK (cvss_score IS NULL OR (cvss_score >= 0.0 AND cvss_score <= 10.0))
);

CREATE INDEX idx_finding_campaign ON findings(campaign_id);
CREATE INDEX idx_finding_severity ON findings(severity);
CREATE INDEX idx_finding_discovered ON findings(discovered_at);
CREATE INDEX idx_finding_verified ON findings(verified);

COMMENT ON TABLE findings IS 'Vulnerability findings from security assessments';

-- ============================================================
-- Work Order Audit Trail
-- ============================================================
CREATE TABLE IF NOT EXISTS redteam_work_order_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    work_order_id UUID NOT NULL REFERENCES redteam_work_orders(id) ON DELETE CASCADE,
    
    action VARCHAR(50) NOT NULL,  
    performed_by UUID NOT NULL REFERENCES redteam_users(id),
    performed_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    ip_address INET,
    user_agent TEXT,
    changes_made JSONB,
    
    CONSTRAINT chk_valid_action CHECK (action IN ('submitted', 'reviewed', 'approved', 'rejected', 'cancelled'))
);

CREATE INDEX idx_audit_work_order ON redteam_work_order_audit(work_order_id);
CREATE INDEX idx_audit_performed_at ON redteam_work_order_audit(performed_at);

COMMENT ON TABLE redteam_work_order_audit IS 'Audit log for work order status changes';

-- ============================================================
-- Login Attempts Table (Rate Limiting)
-- ============================================================
CREATE TABLE IF NOT EXISTS login_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username CITEXT NOT NULL,
    attempted_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    ip_address INET,
    success BOOLEAN DEFAULT false,
    
    INDEX idx_login_username_attempted (username, attempted_at)
);

-- ============================================================
-- Refresh Tokens Table (Token Blacklisting)
-- ============================================================
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token TEXT NOT NULL,
    user_id UUID NOT NULL REFERENCES redteam_users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    
    UNIQUE(token),
    INDEX idx_token_user (user_id, is_active)
);

-- ============================================================
-- Trigger Functions
-- ============================================================

-- Automatic timestamp update trigger
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply to users table
DROP TRIGGER IF EXISTS update_redteam_users_updated_at ON redteam_users;
CREATE TRIGGER update_redteam_users_updated_at
    BEFORE UPDATE ON redteam_users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- ============================================================
-- Sample Test Data
-- ============================================================
-- Note: Password is same for all three accounts: "Password123!"
-- Bcrypt hash generated with salt rounds = 12

INSERT INTO redteam_users (username, email, password_hash, role, permissions, active)
VALUES 
    ('admin', 'admin@redteam.local', '$2a$12$LQv3c.yGLYdKJ8lCpCq9g.uRmMqZn7tqGqTfz5xKqJqHqYjWkXqXy', 'admin', '["admin","read","write","execute"]'::jsonb, true),
    ('pentester', 'pentester@redteam.local', '$2a$12$LQv3c.yGLYdKJ8lCpCq9g.uRmMqZn7tqGqTfz5xKqJqHqYjWkXqXy', 'pentester', '["read","execute"]'::jsonb, true),
    ('auditor', 'auditor@redteam.local', '$2a$12$LQv3c.yGLYdKJ8lCpCq9g.uRmMqZn7tqGqTfz5xKqJqHqYjWkXqXy', 'auditor', '["read"]'::jsonb, true);

-- ============================================================
-- Comments and Documentation
-- ============================================================
COMMENT ON COLUMN redteam_users.password_hash IS 'BCrypt hashed password, never store plaintext';
COMMENT ON COLUMN redteam_users.permissions IS 'JSONB array of permission strings';
COMMENT ON COLUMN redteam_work_orders.target_list IS 'JSONB array of approved target IPs/CIDR ranges';
COMMENT ON COLUMN attack_campaigns.attack_types IS 'Array of attack type names (e.g., ["sql-injection", "xss"])';
COMMENT ON COLUMN findings.evidence_urls IS 'JSONB array of S3/Cloud storage URLs containing vulnerability evidence';

-- ============================================================
-- END OF MIGRATION
-- ============================================================