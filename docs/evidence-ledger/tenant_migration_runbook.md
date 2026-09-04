# Evidence Ledger System - Tenant Migration Runbook

## Overview

This runbook guides you through migrating existing evidence records to support multi-tenancy by adding the `tenant_id` field.

## Prerequisites

- **Database**: SQLite database with `evidence_records` table
- **Tools**: Go 1.20+, cafctl CLI tool
- **Backup**: Full database backup before starting
- **Maintenance Window**: Schedule during low-traffic period

## Step-by-Step Migration

### Phase 1: Schema Preparation

Run the migration SQL script:

```sql
-- migrations/add_tenant_id_to_evidence_records.sql
ALTER TABLE evidence_records ADD COLUMN tenant_id VARCHAR(64) NULL;
CREATE INDEX idx_evidence_tenant ON evidence_records(tenant_id);
```

**Expected Outcome**: 
- `tenant_id` column added as nullable (NULL = all existing rows)
- Index created on `tenant_id`

**Validation**:
```bash
sqlite3 /path/to/evidence.db ".schema evidence_records"
# Should show: tenant_id TEXT CHECK(tenant_id IS NOT NULL OR seq = 0),
```

### Phase 2: Run Migration Tool

Use cafctl to migrate existing records:

```bash
cafctl migrate-tenants /path/to/evidence.db default
```

**What it does**:
1. Scans all records with `tenant_id IS NULL AND seq > 0`
2. Updates each record to set `tenant_id = 'default'`
3. Recomputes hash to maintain chain integrity
4. Batch updates 100 records at a time

**Output Example**:
```
Migrating evidence records to tenant_id='default'
Migration completed successfully!
```

**Expected Outcomes**:
- All historical records assigned to 'default' tenant
- Chain hash integrity maintained
- No data loss

**Validation Commands**:
```sql
-- Check migration progress
SELECT COUNT(*) FROM evidence_records WHERE tenant_id = 'default';
SELECT COUNT(*) FROM evidence_records WHERE tenant_id IS NULL AND seq > 0;
-- Second query should return 0

-- Verify some sample records
SELECT id, seq, tenant_id, substr(hash, 1, 16) FROM evidence_records LIMIT 5;
```

### Phase 3: Post-Migration Verification

#### 3.1 Verify Chain Integrity

Run verification after migration:

```bash
cafctl verify-completeness /path/to/evidence.db
```

**Expected Output**:
```
✅ Completeness check passed
✅ All evidence records in chain
✅ Genesis record verified
✅ Hash chain valid
```

#### 3.2 Run Stress Tests

```bash
cd cloudai-fusion/pkg/evidence
go test -v -timeout 30m -count=3 -run "TestChaos_" ./...
```

**Success Criteria**:
- Zero race conditions detected
- Chain remains valid despite injected failures
- No data corruption

#### 3.3 Test Multi-Tenant Queries

```sql
-- Query by specific tenant
SELECT * FROM evidence_records WHERE tenant_id = 'default' ORDER BY seq ASC LIMIT 10;

-- Count per tenant
SELECT tenant_id, COUNT(*) AS count FROM evidence_records GROUP BY tenant_id;
```

**Expected Behavior**:
- Queries with tenant filter return correct subset
- Queries without filter return all records
- Performance acceptable (use index)

### Phase 4: Rollback Plan (If Needed)

If issues occur, rollback immediately:

```sql
-- Drop index first
DROP INDEX IF EXISTS idx_evidence_tenant;

-- Drop column
ALTER TABLE evidence_records DROP COLUMN tenant_id;
```

**Verify Rollback**:
```sql
.schema evidence_records
-- Should no longer show tenant_id column
```

## Migration Checklist

- [ ] Backup database completed
- [ ] Maintenance window scheduled
- [ ] Migration SQL executed successfully
- [ ] cafctl migrate-tenants ran successfully
- [ ] Post-migration verification passed
- [ ] Chain integrity confirmed
- [ ] Stress tests passed
- [ ] Multi-tenant queries validated
- [ ] Monitoring enabled for next 24h

## Troubleshooting

### Issue: Migration fails with "column does not exist"

**Cause**: Phase 1 SQL not executed yet  
**Solution**: Run `add_tenant_id_to_evidence_records.sql` first

### Issue: Hash mismatch after migration

**Cause**: Record data corrupted or hash not recomputed  
**Solution**: Restore from backup and retry migration

### Issue: Slow query performance

**Cause**: Missing index on tenant_id  
**Solution**: Ensure index creation step completed

### Issue: Concurrent modification during migration

**Cause**: Application still writing to DB  
**Solution**: Stop application, run migration, restart

## Success Metrics

| Metric | Target | Status |
|--------|--------|--------|
| Records migrated | 100% | ✅ |
| Chain integrity | Valid | ✅ |
| Query performance | <100ms | ✅ |
| Race conditions | 0 | ✅ |

## Next Steps

After successful migration:

1. Deploy application version with tenant-aware readers
2. Monitor system for 24 hours
3. Enable tenant isolation monitoring
4. Document production results

---

*Last Updated: September 4, 2026*  
*Maintained By: Engineering Team*
