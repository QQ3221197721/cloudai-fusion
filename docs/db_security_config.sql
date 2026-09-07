-- CloudAI Fusion Database Security Configuration
-- Implements least privilege principle and row-level security

-- ============================================================================
-- 1. Create separate database users with minimal permissions
-- ============================================================================

-- Application user - CREATE/READ/UPDATE only, NO DROP/ALTER
CREATE USER cloudai_app WITH PASSWORD 'strong_password_encrypted';

-- Grant only required table permissions
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO cloudai_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO cloudai_app;

-- Block dangerous operations at database level
REVOKE ALL ON DATABASE cloudai_db FROM cloudai_app;
GRANT CONNECT ON DATABASE cloudai_db TO cloudai_app;

-- Read-only replica user for analytics/reporting
CREATE USER cloudai_readonly WITH PASSWORD 'readonly_password_encrypted';

GRANT SELECT ON ALL TABLES IN SCHEMA public TO cloudai_readonly;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO cloudai_readonly;

-- ============================================================================
-- 2. Row-Level Security (RLS) Policies for Multi-Tenant Isolation
-- ============================================================================

-- Enable RLS on tenant-sensitive tables
ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_usage ENABLE ROW LEVEL SECURITY;

-- Create policy function to get current tenant ID from JWT claim
CREATE OR REPLACE FUNCTION get_current_tenant_id() RETURNS TEXT AS $$
BEGIN
    -- Extract tenant_id from JWT claims (assumes proper JWT parsing)
    RETURN current_setting('app.current_tenant_id', true);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Apply tenant isolation policies
CREATE POLICY tenant_isolation_orders ON orders
    USING (tenant_id = get_current_tenant_id());

CREATE POLICY tenant_isolation_decisions ON decisions
    USING (tenant_id = get_current_tenant_id());

CREATE POLICY tenant_isolation_usage ON tenant_usage
    USING (tenant_id = get_current_tenant_id());

-- Prevent cross-tenant access by default
CREATE POLICY deny_cross_tenant_access ON orders
    FOR ALL TO cloudai_app
    USING (tenant_id = get_current_tenant_id());

-- ============================================================================
-- 3. Schema-Based Tenant Isolation (Enhanced Security Layer)
-- ============================================================================

-- Each tenant gets isolated schema
CREATE SCHEMA IF NOT EXISTS tenant_8003 AUTHORIZATION cloudai_app;
CREATE SCHEMA IF NOT EXISTS tenant_1002 AUTHORIZATION cloudai_app;
CREATE SCHEMA IF NOT EXISTS tenant_111111 AUTHORIZATION cloudai_app;

-- Migrate data to tenant schemas (example for tenant_8003)
ALTER TABLE tenant_8003.orders SET (parallel_workers = 4);
SET search_path TO tenant_8003, public;

-- Create unified view across tenants (for admin operations)
CREATE VIEW all_tenants_combined_vw AS
SELECT *, 'tenant_8003' as tenant_schema_name FROM tenant_8003.orders
UNION ALL
SELECT *, 'tenant_1002' as tenant_schema_name FROM tenant_1002.orders
UNION ALL
SELECT *, 'tenant_111111' as tenant_schema_name FROM tenant_111111.orders;

GRANT SELECT ON all_tenants_combined_vw TO cloudai_admin;

-- ============================================================================
-- 4. Sensitive Data Encryption at Rest
-- ============================================================================

-- Enable pgcrypto extension
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Create encryption function
CREATE OR REPLACE FUNCTION encrypt_customer_pii(data TEXT, key_id UUID)
RETURNS BYTEAS AS $$
BEGIN
    RETURN pgp_sym_encrypt(data, key_id::TEXT, 'algo=aes256');
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Create decryption function  
CREATE OR REPLACE FUNCTION decrypt_customer_pii(ciphertext BYTEAS, key_id UUID)
RETURNS TEXT AS $$
BEGIN
    RETURN pgp_sym_decrypt(ciphertext, key_id::TEXT, 'algo=_aes256');
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Encrypt sensitive columns
UPDATE customers 
SET email_encrypted = encrypt_customer_pii(email, current_setting('app.encryption_key')::uuid)
WHERE email IS NOT NULL;

-- ============================================================================
-- 5. Database Auditing for Compliance
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS auditable;

-- Audit all DDL operations
CREATE TABLE ddl_audit_log (
    id SERIAL PRIMARY KEY,
    operation_time TIMESTAMPTZ DEFAULT now(),
    operation_type TEXT,
    database_user TEXT,
    sql_statement TEXT,
    tenant_id TEXT,
    application_ip INET
);

-- Trigger function for audit logging
CREATE OR REPLACE FUNCTION log_ddl_operations()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO ddl_audit_log (operation_type, database_user, sql_statement, tenant_id)
    VALUES (TG_OP, session_user, COALESCE(current_query, TG_ARGV[0]), NULL);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ddl_audit_trigger
    AFTERddl ON DATABASE cloudai_db
    EXECUTE PROCEDURE log_ddl_operations();

-- ============================================================================
-- 6. Connection Pool Separation for Different Workloads
-- ============================================================================

-- Read-heavy workloads use replica connections
-- Write-intensive workloads use primary connections
-- Analytics queries use dedicated pool with resource limits

-- Example psql configuration:
-- # .pgbouncer.ini
-- [databases]
-- cloudai_replica = host=replica-host dbname=cloudai_db port=5432
-- cloudai_primary = host=primary-host dbname=cloudai_db port=5432

-- [pgbouncer]
-- listen_addr = *
-- listen_port = 6432
-- auth_type = md5
-- auth_file = /etc/pgbouncer/userlist.txt

-- Pool sizes per workload type
-- [databases.cloudai_replica]
-- pool_size = 50
-- min_pool_size = 10
-- reserve_pool_size = 5
-- reserve_pool_timeout = 3

-- [databases.cloudai_primary]
-- pool_size = 30
-- min_pool_size = 5
-- max_client_conn = 100
