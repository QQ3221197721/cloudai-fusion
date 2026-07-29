-- CloudAI Fusion AISecOps - Initial Schema for Threat Intelligence Module
-- This script is executed automatically on ClickHouse container startup

-- CVE Entries Table
CREATE DATABASE IF NOT EXISTS security;

CREATE TABLE IF NOT EXISTS security.cve_entries (
    cve_id String,
    description String,
    cvss_v3_score Float32,
    cvss_v3_vector String,
    mitre_tags Array(String),
    references Array(String),
    published_at DateTime,
    modified_date DateTime,
    vulnerable_software Array(String)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(published_at)
ORDER BY (published_at, cve_id)
SETTINGS index_granularity = 8192;

-- IOC Entries Table
CREATE TABLE IF NOT EXISTS security.ioc_entries (
    ioc_id UUID DEFAULT generateUUID4(),
    ioc_type String,
    value String,
    threat_actor Nullable(String),
    severity String,
    first_seen_at DateTime,
    last_seen_at DateTime,
    sources Array(String),
    inserted_at DateTime DEFAULT now()
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(first_seen_at)
ORDER BY (ioc_type, first_seen_at, value)
SETTINGS index_granularity = 8192;

-- Knowledge Graph Table (MITRE ATT&CK)
CREATE TABLE IF NOT EXISTS security.knowledge_graph (
    type String,
    id String,
    name String,
    description String,
    tactic_ids Array(String),
    updated_at DateTime DEFAULT now()
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (type, id);

-- Create materialized view for real-time CVE alerts
CREATE MATERIALIZED VIEW IF NOT EXISTS security.cve_alerts_mv TO security.cve_entries AS
SELECT 
    cve_id,
    cvss_v3_score,
    mitre_tags,
    published_at,
    'new_cve' as event_type
FROM security.cve_entries
WHERE cvss_v3_score >= 9.0;  -- Critical CVEs only
