package migrate

// BuiltinMigrations returns all CloudAI Fusion schema migrations in version order.
// These are applied automatically on server startup via Migrator.Up().
func BuiltinMigrations() []Migration {
	return []Migration{
		{
			Version:     "0001",
			Description: "Create users table",
			UpSQL: `CREATE TABLE IF NOT EXISTS users (
				id            UUID PRIMARY KEY,
				username      VARCHAR(64) UNIQUE NOT NULL,
				email         VARCHAR(128) UNIQUE NOT NULL,
				password_hash VARCHAR(256) NOT NULL,
				display_name  VARCHAR(128),
				role          VARCHAR(32) NOT NULL DEFAULT 'viewer',
				status        VARCHAR(32) NOT NULL DEFAULT 'active',
				last_login_at TIMESTAMP,
				created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at    TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at    TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users(deleted_at);`,
			DownSQL: `DROP TABLE IF EXISTS users;`,
		},
		{
			Version:     "0002",
			Description: "Create audit_logs table",
			UpSQL: `CREATE TABLE IF NOT EXISTS audit_logs (
				id            UUID PRIMARY KEY,
				user_id       VARCHAR(64),
				username      VARCHAR(64),
				action        VARCHAR(64) NOT NULL,
				resource_type VARCHAR(64),
				resource_id   VARCHAR(128),
				ip_address    VARCHAR(64),
				user_agent    VARCHAR(256),
				status        VARCHAR(32) NOT NULL,
				details       TEXT,
				created_at    TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
			CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);`,
			DownSQL: `DROP TABLE IF EXISTS audit_logs;`,
		},
		{
			Version:     "0003",
			Description: "Create clusters table",
			UpSQL: `CREATE TABLE IF NOT EXISTS clusters (
				id                   UUID PRIMARY KEY,
				name                 VARCHAR(128) NOT NULL,
				provider             VARCHAR(32) NOT NULL,
				provider_cluster_id  VARCHAR(256),
				region               VARCHAR(64),
				kubernetes_version   VARCHAR(32),
				endpoint             VARCHAR(512),
				ca_certificate       TEXT,
				status               VARCHAR(32) NOT NULL DEFAULT 'pending',
				node_count           INT DEFAULT 0,
				gpu_count            INT DEFAULT 0,
				total_cpu            BIGINT DEFAULT 0,
				total_memory         BIGINT DEFAULT 0,
				total_gpu_memory     BIGINT DEFAULT 0,
				labels               JSONB DEFAULT '{}',
				annotations          JSONB DEFAULT '{}',
				config               JSONB DEFAULT '{}',
				health_check_at      TIMESTAMP,
				created_by           UUID,
				created_at           TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at           TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at           TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_clusters_deleted_at ON clusters(deleted_at);`,
			DownSQL: `DROP TABLE IF EXISTS clusters;`,
		},
		{
			Version:     "0004",
			Description: "Create workloads and workload_events tables",
			UpSQL: `CREATE TABLE IF NOT EXISTS workloads (
				id                 UUID PRIMARY KEY,
				name               VARCHAR(256) NOT NULL,
				namespace          VARCHAR(128) DEFAULT 'default',
				cluster_id         UUID NOT NULL,
				type               VARCHAR(32) NOT NULL,
				status             VARCHAR(32) NOT NULL DEFAULT 'pending',
				priority           INT DEFAULT 0,
				framework          VARCHAR(32),
				model_name         VARCHAR(256),
				image              VARCHAR(512),
				command            TEXT,
				resource_request   JSONB NOT NULL DEFAULT '{}',
				resource_limit     JSONB DEFAULT '{}',
				gpu_type_required  VARCHAR(64),
				gpu_count_required INT DEFAULT 0,
				gpu_mem_required   BIGINT DEFAULT 0,
				env_vars           JSONB DEFAULT '{}',
				scheduling_policy  JSONB DEFAULT '{}',
				assigned_node      VARCHAR(256),
				assigned_gpus      VARCHAR(256),
				started_at         TIMESTAMP,
				completed_at       TIMESTAMP,
				error_message      TEXT,
				metrics            JSONB DEFAULT '{}',
				created_by         UUID,
				created_at         TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at         TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at         TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_workloads_cluster_id ON workloads(cluster_id);
			CREATE INDEX IF NOT EXISTS idx_workloads_deleted_at ON workloads(deleted_at);

			CREATE TABLE IF NOT EXISTS workload_events (
				id          UUID PRIMARY KEY,
				workload_id UUID NOT NULL,
				from_status VARCHAR(32),
				to_status   VARCHAR(32) NOT NULL,
				reason      VARCHAR(256),
				message     TEXT,
				operator    VARCHAR(128),
				created_at  TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_workload_events_workload_id ON workload_events(workload_id);
			CREATE INDEX IF NOT EXISTS idx_workload_events_created_at ON workload_events(created_at);`,
			DownSQL: `DROP TABLE IF EXISTS workload_events; DROP TABLE IF EXISTS workloads;`,
		},
		{
			Version:     "0005",
			Description: "Create security_policies and vulnerability_scans tables",
			UpSQL: `CREATE TABLE IF NOT EXISTS security_policies (
				id          UUID PRIMARY KEY,
				name        VARCHAR(128) NOT NULL,
				description TEXT,
				type        VARCHAR(32) NOT NULL,
				scope       VARCHAR(32) NOT NULL DEFAULT 'cluster',
				cluster_id  UUID,
				rules       JSONB NOT NULL DEFAULT '[]',
				enforcement VARCHAR(16) NOT NULL DEFAULT 'enforce',
				status      VARCHAR(16) NOT NULL DEFAULT 'active',
				created_by  UUID,
				created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at  TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_security_policies_deleted_at ON security_policies(deleted_at);

			CREATE TABLE IF NOT EXISTS vulnerability_scans (
				id              UUID PRIMARY KEY,
				cluster_id      UUID,
				scan_type       VARCHAR(32) NOT NULL,
				status          VARCHAR(32) NOT NULL DEFAULT 'pending',
				findings        JSONB DEFAULT '[]',
				summary         JSONB DEFAULT '{}',
				total_findings  INT DEFAULT 0,
				critical_count  INT DEFAULT 0,
				high_count      INT DEFAULT 0,
				medium_count    INT DEFAULT 0,
				low_count       INT DEFAULT 0,
				started_at      TIMESTAMP,
				completed_at    TIMESTAMP,
				created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at      TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_vulnerability_scans_cluster_id ON vulnerability_scans(cluster_id);`,
			DownSQL: `DROP TABLE IF EXISTS vulnerability_scans; DROP TABLE IF EXISTS security_policies;`,
		},
		{
			Version:     "0006",
			Description: "Create mesh_policies, wasm_modules, wasm_instances, edge_nodes tables",
			UpSQL: `CREATE TABLE IF NOT EXISTS mesh_policies (
				id            UUID PRIMARY KEY,
				name          VARCHAR(128) NOT NULL,
				namespace     VARCHAR(128) DEFAULT 'default',
				cluster_id    UUID,
				type          VARCHAR(16) NOT NULL,
				selector      JSONB DEFAULT '{}',
				ingress_rules JSONB DEFAULT '[]',
				egress_rules  JSONB DEFAULT '[]',
				l7_rules      JSONB DEFAULT '[]',
				enforcement   VARCHAR(16) NOT NULL DEFAULT 'enforce',
				status        VARCHAR(16) NOT NULL DEFAULT 'active',
				created_at    TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at    TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at    TIMESTAMP
			);

			CREATE TABLE IF NOT EXISTS wasm_modules (
				id             UUID PRIMARY KEY,
				name           VARCHAR(128) NOT NULL,
				version        VARCHAR(32),
				runtime        VARCHAR(32),
				source_url     VARCHAR(512),
				size_bytes     BIGINT DEFAULT 0,
				checksum       VARCHAR(128),
				permissions    JSONB DEFAULT '[]',
				status         VARCHAR(16) NOT NULL DEFAULT 'active',
				registered_at  TIMESTAMP NOT NULL DEFAULT NOW(),
				created_at     TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at     TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at     TIMESTAMP
			);

			CREATE TABLE IF NOT EXISTS wasm_instances (
				id          UUID PRIMARY KEY,
				module_id   UUID NOT NULL,
				module_name VARCHAR(128),
				node_name   VARCHAR(256),
				status      VARCHAR(16) NOT NULL DEFAULT 'pending',
				memory_mb   INT DEFAULT 0,
				cpu_shares  INT DEFAULT 0,
				started_at  TIMESTAMP,
				created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at  TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at  TIMESTAMP
			);

			CREATE TABLE IF NOT EXISTS edge_nodes (
				id              UUID PRIMARY KEY,
				name            VARCHAR(128) NOT NULL,
				tier            VARCHAR(16) NOT NULL,
				location        VARCHAR(256),
				endpoint        VARCHAR(512),
				status          VARCHAR(16) NOT NULL DEFAULT 'offline',
				capabilities    JSONB DEFAULT '{}',
				max_power_watts FLOAT DEFAULT 0,
				current_power   FLOAT DEFAULT 0,
				resource_usage  JSONB DEFAULT '{}',
				last_heartbeat  TIMESTAMP,
				registered_at   TIMESTAMP NOT NULL DEFAULT NOW(),
				created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at      TIMESTAMP
			);`,
			DownSQL: `DROP TABLE IF EXISTS edge_nodes; DROP TABLE IF EXISTS wasm_instances; DROP TABLE IF EXISTS wasm_modules; DROP TABLE IF EXISTS mesh_policies;`,
		},
		{
			Version:     "0007",
			Description: "Create alert_rules and alert_events tables",
			UpSQL: `CREATE TABLE IF NOT EXISTS alert_rules (
				id           UUID PRIMARY KEY,
				name         VARCHAR(128) NOT NULL,
				description  TEXT,
				cluster_id   UUID,
				severity     VARCHAR(16) NOT NULL,
				condition    VARCHAR(256) NOT NULL,
				threshold    DOUBLE PRECISION DEFAULT 0,
				duration_sec INT DEFAULT 0,
				channels     JSONB DEFAULT '[]',
				labels       JSONB DEFAULT '{}',
				status       VARCHAR(16) NOT NULL DEFAULT 'active',
				created_at   TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at   TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at   TIMESTAMP
			);

			CREATE TABLE IF NOT EXISTS alert_events (
				id          UUID PRIMARY KEY,
				rule_id     UUID NOT NULL,
				rule_name   VARCHAR(128),
				cluster_id  UUID,
				severity    VARCHAR(16) NOT NULL,
				message     TEXT,
				status      VARCHAR(16) NOT NULL DEFAULT 'firing',
				labels      JSONB DEFAULT '{}',
				fired_at    TIMESTAMP NOT NULL DEFAULT NOW(),
				resolved_at TIMESTAMP,
				created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at  TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_alert_events_fired_at ON alert_events(fired_at);`,
			DownSQL: `DROP TABLE IF EXISTS alert_events; DROP TABLE IF EXISTS alert_rules;`,
		},
		{
			Version:     "0008",
			Description: "Create feature_flags table",
			UpSQL: `CREATE TABLE IF NOT EXISTS feature_flags (
				key          VARCHAR(128) PRIMARY KEY,
				enabled      BOOLEAN NOT NULL DEFAULT false,
				description  TEXT,
				percentage   INT DEFAULT 100,
				metadata     JSONB DEFAULT '{}',
				updated_by   VARCHAR(64),
				created_at   TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at   TIMESTAMP NOT NULL DEFAULT NOW()
			);`,
			DownSQL: `DROP TABLE IF EXISTS feature_flags;`,
		},
		// =========================================================================
		// Commercialization: Multi-Tenant Isolation
		// =========================================================================
		{
			Version:     "0009",
			Description: "Create tenants, tenant_resource_quotas, tenant_billing tables",
			UpSQL: `CREATE TABLE IF NOT EXISTS tenants (
				id                  UUID PRIMARY KEY,
				name                VARCHAR(128) UNIQUE NOT NULL,
				display_name        VARCHAR(256),
				tier                VARCHAR(32) NOT NULL DEFAULT 'starter',
				status              VARCHAR(32) NOT NULL DEFAULT 'active',
				namespace           VARCHAR(128) UNIQUE NOT NULL,
				admin_email         VARCHAR(256) NOT NULL,
				max_users           INT DEFAULT 5,
				max_clusters        INT DEFAULT 1,
				max_workloads       INT DEFAULT 10,
				vpc_cidr            VARCHAR(32),
				encryption_key_id   VARCHAR(256),
				billing_plan        VARCHAR(64) DEFAULT 'monthly',
				billing_email       VARCHAR(256),
				metadata            JSONB DEFAULT '{}',
				suspended_at        TIMESTAMP,
				suspended_reason    TEXT,
				created_at          TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at          TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at          TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_tenants_tier ON tenants(tier);
			CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
			CREATE INDEX IF NOT EXISTS idx_tenants_deleted_at ON tenants(deleted_at);

			CREATE TABLE IF NOT EXISTS tenant_resource_quotas (
				id                UUID PRIMARY KEY,
				tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
				resource_type     VARCHAR(64) NOT NULL,
				hard_limit        BIGINT NOT NULL DEFAULT 0,
				soft_limit        BIGINT DEFAULT 0,
				current_usage     BIGINT DEFAULT 0,
				unit              VARCHAR(32) DEFAULT 'count',
				enforced          BOOLEAN DEFAULT true,
				created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				UNIQUE(tenant_id, resource_type)
			);
			CREATE INDEX IF NOT EXISTS idx_tenant_quotas_tenant_id ON tenant_resource_quotas(tenant_id);

			CREATE TABLE IF NOT EXISTS tenant_billing_records (
				id                UUID PRIMARY KEY,
				tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
				period_start      TIMESTAMP NOT NULL,
				period_end        TIMESTAMP NOT NULL,
				resource_type     VARCHAR(64) NOT NULL,
				usage_quantity    DOUBLE PRECISION DEFAULT 0,
				unit_price        DOUBLE PRECISION DEFAULT 0,
				total_cost        DOUBLE PRECISION DEFAULT 0,
				currency          VARCHAR(8) DEFAULT 'USD',
				status            VARCHAR(32) DEFAULT 'pending',
				invoice_id        VARCHAR(128),
				created_at        TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_billing_records_tenant_id ON tenant_billing_records(tenant_id);
			CREATE INDEX IF NOT EXISTS idx_billing_records_period ON tenant_billing_records(period_start, period_end);`,
			DownSQL: `DROP TABLE IF EXISTS tenant_billing_records;
				DROP TABLE IF EXISTS tenant_resource_quotas;
				DROP TABLE IF EXISTS tenants;`,
		},
		// =========================================================================
		// Commercialization: Enterprise Features (SSO, Audit, SLA)
		// =========================================================================
		{
			Version:     "0010",
			Description: "Create sso_configurations, enterprise_audit_reports, sla_contracts tables",
			UpSQL: `CREATE TABLE IF NOT EXISTS sso_configurations (
				id                UUID PRIMARY KEY,
				tenant_id         UUID REFERENCES tenants(id) ON DELETE CASCADE,
				provider_type     VARCHAR(32) NOT NULL,
				name              VARCHAR(128) NOT NULL,
				enabled           BOOLEAN DEFAULT false,
				config            JSONB NOT NULL DEFAULT '{}',
				saml_metadata_url VARCHAR(512),
				saml_certificate  TEXT,
				ldap_host         VARCHAR(256),
				ldap_port         INT DEFAULT 389,
				ldap_base_dn      VARCHAR(256),
				ldap_bind_dn      VARCHAR(256),
				oidc_issuer_url   VARCHAR(512),
				oidc_client_id    VARCHAR(256),
				oidc_client_secret VARCHAR(256),
				created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at        TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_sso_tenant_id ON sso_configurations(tenant_id);

			CREATE TABLE IF NOT EXISTS enterprise_audit_reports (
				id                UUID PRIMARY KEY,
				tenant_id         UUID REFERENCES tenants(id) ON DELETE CASCADE,
				report_type       VARCHAR(64) NOT NULL,
				period_start      TIMESTAMP NOT NULL,
				period_end        TIMESTAMP NOT NULL,
				format            VARCHAR(16) DEFAULT 'pdf',
				status            VARCHAR(32) DEFAULT 'pending',
				storage_path      VARCHAR(512),
				total_events      INT DEFAULT 0,
				summary           JSONB DEFAULT '{}',
				generated_at      TIMESTAMP,
				created_at        TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_audit_reports_tenant_id ON enterprise_audit_reports(tenant_id);

			CREATE TABLE IF NOT EXISTS sla_contracts (
				id                UUID PRIMARY KEY,
				tenant_id         UUID REFERENCES tenants(id) ON DELETE CASCADE,
				name              VARCHAR(128) NOT NULL,
				tier              VARCHAR(32) NOT NULL,
				availability_target DOUBLE PRECISION NOT NULL,
				latency_p99_ms    INT DEFAULT 0,
				latency_p95_ms    INT DEFAULT 0,
				support_hours     VARCHAR(32) DEFAULT '8x5',
				response_time_min INT DEFAULT 60,
				penalty_credits    DOUBLE PRECISION DEFAULT 0,
				status            VARCHAR(32) DEFAULT 'active',
				effective_from    TIMESTAMP NOT NULL,
				effective_until   TIMESTAMP,
				created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at        TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_sla_contracts_tenant_id ON sla_contracts(tenant_id);`,
			DownSQL: `DROP TABLE IF EXISTS sla_contracts;
				DROP TABLE IF EXISTS enterprise_audit_reports;
				DROP TABLE IF EXISTS sso_configurations;`,
		},
		// =========================================================================
		// Commercialization: Support Ticket System
		// =========================================================================
		{
			Version:     "0011",
			Description: "Create support_tickets and ticket_comments tables",
			UpSQL: `CREATE TABLE IF NOT EXISTS support_tickets (
				id                UUID PRIMARY KEY,
				tenant_id         UUID REFERENCES tenants(id) ON DELETE CASCADE,
				title             VARCHAR(256) NOT NULL,
				description       TEXT,
				category          VARCHAR(64) NOT NULL DEFAULT 'general',
				priority          VARCHAR(16) NOT NULL DEFAULT 'medium',
				status            VARCHAR(32) NOT NULL DEFAULT 'open',
				assigned_to       UUID,
				reporter_email    VARCHAR(256) NOT NULL,
				sla_contract_id   UUID REFERENCES sla_contracts(id),
				response_due_at   TIMESTAMP,
				resolution_due_at TIMESTAMP,
				first_response_at TIMESTAMP,
				resolved_at       TIMESTAMP,
				closed_at         TIMESTAMP,
				satisfaction_score INT,
				satisfaction_comment TEXT,
				tags              JSONB DEFAULT '[]',
				metadata          JSONB DEFAULT '{}',
				created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				deleted_at        TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_tickets_tenant_id ON support_tickets(tenant_id);
			CREATE INDEX IF NOT EXISTS idx_tickets_status ON support_tickets(status);
			CREATE INDEX IF NOT EXISTS idx_tickets_priority ON support_tickets(priority);
			CREATE INDEX IF NOT EXISTS idx_tickets_assigned_to ON support_tickets(assigned_to);
			CREATE INDEX IF NOT EXISTS idx_tickets_deleted_at ON support_tickets(deleted_at);

			CREATE TABLE IF NOT EXISTS ticket_comments (
				id                UUID PRIMARY KEY,
				ticket_id         UUID NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
				author_id         UUID,
				author_email      VARCHAR(256) NOT NULL,
				author_type       VARCHAR(32) DEFAULT 'customer',
				content           TEXT NOT NULL,
				is_internal       BOOLEAN DEFAULT false,
				attachments       JSONB DEFAULT '[]',
				created_at        TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at        TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_ticket_comments_ticket_id ON ticket_comments(ticket_id);
			CREATE INDEX IF NOT EXISTS idx_ticket_comments_created_at ON ticket_comments(created_at);`,
			DownSQL: `DROP TABLE IF EXISTS ticket_comments;
				DROP TABLE IF EXISTS support_tickets;`,
		},
	}
}
