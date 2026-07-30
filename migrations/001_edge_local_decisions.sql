-- Edge Autonomy - Local Decision Audit Log Schema
-- Migration: 001_edge_local_decisions.sql
-- Purpose: Track all offline decisions for reconciliation and evidence sealing

-- ============================================================================
-- Table: cached_nodes (extends existing infrastructure)
-- Stores cached node states for offline operation
-- ============================================================================

CREATE TABLE IF NOT EXISTS cached_nodes (
    id VARCHAR(255) PRIMARY KEY,
    node_id VARCHAR(255) NOT NULL,
    spec_json JSONB NOT NULL,          -- Kubernetes Node spec serialized
    status_json JSONB NOT NULL,        -- Kubernetes Node status serialized
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    
    -- Index for efficient querying of recent cache entries
    INDEX idx_cached_node_id (node_id),
    INDEX idx_cached_updated_at (updated_at),
    
    -- Enforce reasonable cache size
    CONSTRAINT chk_cache_valid CHECK (updated_at > NOW() - INTERVAL '7 days')
);

COMMENT ON TABLE cached_nodes IS 'Cached node state for offline decision making';
COMMENT ON COLUMN cached_nodes.spec_json IS 'Serialized Kubernetes Node specification';
COMMENT ON COLUMN cached_nodes.status_json IS 'Serialized Kubernetes Node status';

-- ============================================================================
-- Table: offline_decisions (NEW - Core for edge autonomy)
-- Tracks all scheduling decisions made during offline periods
-- ============================================================================

CREATE TABLE IF NOT EXISTS offline_decisions (
    record_id VARCHAR(255) PRIMARY KEY,                    -- Unique ID (UUID)
    node_id VARCHAR(255) NOT NULL,                         -- Source edge node
    workload_id VARCHAR(255) NOT NULL,                     -- Workload being scheduled
    decision_data JSONB NOT NULL,                          -- Full decision payload
    version_vec BYTEA NOT NULL,                            -- Version vector (binary)
    timestamp TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    synced BOOLEAN DEFAULT FALSE,                          -- Has cloud synced this?
    synced_at TIMESTAMP WITH TIME ZONE,                    -- When was it synced?
    sync_error TEXT,                                       -- Sync error if failed
    
    -- Foreign key reference to nodes table
    CONSTRAINT fk_offline_node FOREIGN KEY (node_id) 
        REFERENCES nodes(id) ON DELETE CASCADE,
    
    -- Indexes for efficient queries
    INDEX idx_offline_synced (synced),                              -- Filter unsynced decisions
    INDEX idx_offline_node_created (node_id, timestamp DESC),       -- Per-node timeline
    INDEX idx_offline_workload (workload_id),                       -- Find by workload
    UNIQUE idx_offline_record_id (record_id)                        -- Ensure uniqueness
);

COMMENT ON TABLE offline_decisions IS 'Audit log for all offline scheduling decisions';
COMMENT ON COLUMN offline_decisions.decision_data IS 'Complete decision payload including resource requests';
COMMENT ON COLUMN offline_decisions.version_vec IS 'Version vector for causal tracking (protobuf/binary)';

-- ============================================================================
-- Table: sync_queues (NEW - For bidirectional reconciliation)
-- Tracks pending sync operations from both directions
-- ============================================================================

CREATE TABLE IF NOT EXISTS sync_queues (
    queue_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    direction VARCHAR(50) NOT NULL CHECK (direction IN ('edge_to_cloud', 'cloud_to_edge')),
    entity_type VARCHAR(50) NOT NULL,                -- e.g., 'local_decision', 'policy_update'
    entity_id VARCHAR(255) NOT NULL,                 -- ID of the entity being synced
    payload JSONB NOT NULL,                          -- Entity data serialized
    priority INT DEFAULT 0,                          -- Higher number = higher priority
    retry_count INT DEFAULT 0,
    max_retries INT DEFAULT 3,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    last_retry_at TIMESTAMP WITH TIME ZONE,
    next_retry_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    error_message TEXT,
    
    -- Index for efficient queuing
    INDEX idx_sync_queue_pending (completed_at IS NULL),
    INDEX idx_sync_queue_next_retry (next_retry_at),
    INDEX idx_sync_queue_entity (entity_type, entity_id)
);

COMMENT ON TABLE sync_queues IS 'Queue for bidirectional sync between edge and cloud';
COMMENT ON COLUMN sync_queues.direction IS 'Direction: edge_to_cloud or cloud_to_edge';
COMMENT ON COLUMN sync_queues.priority IS 'Higher numbers have higher priority';

-- ============================================================================
-- Insert sample data for testing (optional)
-- ============================================================================

-- Example: Create a test cached node entry
INSERT INTO cached_nodes (id, node_id, spec_json, status_json, updated_at)
VALUES (
    'test-node-001',
    'edge-worker-01',
    '{"apiVersion":"v1","kind":"Node","spec":{"podCIDR":"10.244.0.0/24"},"metadata":{"name":"edge-worker-01"}}',
    '{"status":{"capacity":{"cpu":"4","memory":"8Gi","nvidia.com/gpu":"2"},"conditions":[{"type":"Ready","status":"True"}]}}',
    CURRENT_TIMESTAMP
) ON CONFLICT (id) DO NOTHING;

-- Example: Create a test offline decision entry
INSERT INTO offline_decisions (
    record_id, node_id, workload_id, decision_data, version_vec, synced
)
VALUES (
    'test-decision-001',
    'edge-worker-01',
    'workload-training-job-abc',
    '{"nodeId":"edge-worker-01","gpuHours":100,"qosClass":"high","startTime":"2026-07-30T12:00:00Z"}',
    decode('01000001', 'hex'),
    false
) ON CONFLICT (record_id) DO NOTHING;

-- ============================================================================
-- Create functions for common operations
-- ============================================================================

-- Function: Get latest cached state for a node (within grace period)
CREATE OR REPLACE FUNCTION get_latest_cached_state(node_id_param VARCHAR, grace_period_minutes INT DEFAULT 5)
RETURNS TABLE(spec_json JSONB, status_json JSONB, updated_at TIMESTAMP) AS $$
BEGIN
    RETURN QUERY
    SELECT cn.spec_json, cn.status_json, cn.updated_at
    FROM cached_nodes cn
    WHERE cn.node_id = node_id_param
      AND cn.updated_at >= NOW() - (grace_period_minutes || ' minutes')::INTERVAL
    ORDER BY cn.updated_at DESC
    LIMIT 1;
END;
$$ LANGUAGE plpgsql STABLE;

-- Function: Get unsynced local decisions for processing
CREATE OR REPLACE FUNCTION get_unsynced_decisions(limit_count INT DEFAULT 100)
RETURNS TABLE(record_id VARCHAR, node_id VARCHAR, workload_id VARCHAR, decision_data JSONB) AS $$
BEGIN
    RETURN QUERY
    SELECT od.record_id, od.node_id, od.workload_id, od.decision_data
    FROM offline_decisions od
    WHERE od.synced = false
    ORDER BY od.timestamp ASC
    LIMIT limit_count;
END;
$$ LANGUAGE plpgsql STABLE;

-- Function: Mark decision as synced
CREATE OR REPLACE FUNCTION mark_decision_synced(record_id_param VARCHAR, sync_error_msg TEXT DEFAULT NULL)
RETURNS BOOLEAN AS $$
DECLARE
    updated_count INT;
BEGIN
    UPDATE offline_decisions
    SET synced = true,
        synced_at = CURRENT_TIMESTAMP,
        sync_error = sync_error_msg
    WHERE record_id = record_id_param
      AND synced = false;
    
    GET DIAGNOSTICS updated_count = ROW_COUNT;
    RETURN updated_count > 0;
END;
$$ LANGUAGE plpgsql;

-- ============================================================================
-- Create indexes for performance
-- ============================================================================

-- Optimize cache lookup patterns
CREATE INDEX IF NOT EXISTS idx_cached_nodes_recent 
ON cached_nodes(node_id, updated_at DESC);

-- Optimize decision retrieval patterns  
CREATE INDEX IF NOT EXISTS idx_offline_decisions_lookup
ON offline_decisions(node_id, timestamp DESC) WHERE synced = false;

CREATE INDEX IF NOT EXISTS idx_offline_decisions_workload
ON offline_decisions(workload_id, timestamp DESC);

-- ============================================================================
-- Data retention policy (optional - depends on compliance requirements)
-- ============================================================================

-- Example: Automatically delete old cache entries (older than 30 days)
-- CREATE EVENT TRIGGER delete_old_cache_entries
--     ON DATABASE_CHANGE
-- EXECUTE FUNCTION pg_cron.schedule('0 0 * * *', 
--     'DELETE FROM cached_nodes WHERE updated_at < NOW() - INTERVAL ''30 days''');

-- Note: If using PostgreSQL with pg_cron extension enabled

COMMENT ON FUNCTION get_latest_cached_state IS 'Retrieve most recent cached node state within grace period';
COMMENT ON FUNCTION get_unsynced_decisions IS 'Fetch unsynced local decisions for sync processing';
COMMENT ON FUNCTION mark_decision_synced IS 'Mark a local decision as successfully synced with cloud';
