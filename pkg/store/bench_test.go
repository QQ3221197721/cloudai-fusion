package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ============================================================================
// Store Benchmarks — persistence layer latency / throughput / allocations
// ============================================================================
//
// Run with:  go test ./pkg/store/ -bench=BenchmarkStore -benchmem -run=^$
//
// Backend: pure-Go SQLite (github.com/glebarez/sqlite), file-backed in a temp
// dir so the real SQL + driver + disk path is exercised. PostgreSQL is the
// production driver; SQLite keeps these benchmarks hermetic and CI-runnable.
// Numbers are therefore a *relative* measure of the store layer's own overhead
// (GORM reflection, SQL build, scan) and not a PostgreSQL capacity claim.
//
// MaxOpenConns is pinned to 1 because SQLite serializes writers; this removes
// driver-level lock contention noise from the per-operation latency figures.

// newBenchStore opens a file-backed SQLite Store with all models migrated.
func newBenchStore(b *testing.B) *Store {
	b.Helper()

	dsn := filepath.Join(b.TempDir(), "bench.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		b.Fatalf("gorm.Open failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		b.Fatalf("db.DB() failed: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	models := []interface{}{
		&User{}, &AuditLog{},
		&ClusterModel{}, &WorkloadModel{}, &WorkloadEvent{},
		&SecurityPolicyModel{}, &VulnerabilityScanModel{},
		&MeshPolicyModel{},
		&WasmModuleModel{}, &WasmInstanceModel{},
		&EdgeNodeModel{},
		&AlertRuleModel{}, &AlertEventModel{},
		&SchedulerSnapshotModel{},
	}
	for _, m := range models {
		if err := db.AutoMigrate(m); err != nil {
			b.Fatalf("AutoMigrate %T failed: %v", m, err)
		}
	}

	quiet := logrus.New()
	quiet.SetLevel(logrus.PanicLevel)

	return &Store{db: db, logger: quiet}
}

// seedUser inserts a single user used by read benchmarks.
func seedUser(b *testing.B, s *Store, id string) {
	b.Helper()
	u := &User{
		ID: id, Username: "u_" + id, Email: id + "@bench.local",
		PasswordHash: "$2a$10$benchmarkhashvaluepadding0000000000000000000000000000",
		Role:         "admin", Status: "active",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateUser(u); err != nil {
		b.Fatalf("seed CreateUser failed: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Single-key read latency
// ----------------------------------------------------------------------------

// BenchmarkStore_Read_GetUserByID measures primary-key point-read latency.
func BenchmarkStore_Read_GetUserByID(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedUser(b, s, "u-point-read")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, err := s.GetUserByID("u-point-read")
		if err != nil {
			b.Fatalf("GetUserByID failed at i=%d: %v", i, err)
		}
		if u.ID == "" {
			b.Fatal("GetUserByID returned empty user")
		}
	}
}

// BenchmarkStore_Read_GetUserByUsername measures secondary unique-index read latency.
func BenchmarkStore_Read_GetUserByUsername(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedUser(b, s, "u-idx-read")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u, err := s.GetUserByUsername("u_u-idx-read")
		if err != nil {
			b.Fatalf("GetUserByUsername failed at i=%d: %v", i, err)
		}
		if u.ID == "" {
			b.Fatal("GetUserByUsername returned empty user")
		}
	}
}

// BenchmarkStore_Read_GetClusterByID measures point-read latency on a wide row
// (ClusterModel has ~20 columns incl. text/jsonb) to expose scan-cost scaling.
func BenchmarkStore_Read_GetClusterByID(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()

	c := &ClusterModel{
		ID: "c-point-read", Name: "bench-cluster", Provider: "aws",
		Region: "us-east-1", Status: "healthy", Endpoint: "https://k8s.bench.local",
		KubernetesVersion: "v1.30.0", NodeCount: 32, GPUCount: 256,
		TotalCPU: 512000, TotalMemory: 2 << 40, TotalGPUMemory: 256 << 30,
		Labels: `{"env":"bench"}`, Annotations: `{}`, Config: `{}`,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateCluster(c); err != nil {
		b.Fatalf("seed CreateCluster failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := s.GetClusterByID("c-point-read")
		if err != nil {
			b.Fatalf("GetClusterByID failed at i=%d: %v", i, err)
		}
		if got.ID == "" {
			b.Fatal("GetClusterByID returned empty cluster")
		}
	}
}

// ----------------------------------------------------------------------------
// Single-key write / update latency
// ----------------------------------------------------------------------------

// BenchmarkStore_Write_CreateUser measures single-row INSERT latency.
func BenchmarkStore_Write_CreateUser(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u := &User{
			ID: fmt.Sprintf("u-w-%d", i), Username: fmt.Sprintf("uw_%d", i),
			Email: fmt.Sprintf("uw_%d@bench.local", i),
			PasswordHash: "$2a$10$benchmarkhashvaluepadding0000000000000000000000000000",
			Role:         "viewer", Status: "active",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := s.CreateUser(u); err != nil {
			b.Fatalf("CreateUser failed at i=%d: %v", i, err)
		}
	}
}

// BenchmarkStore_Write_UpdateUser measures full-row UPDATE (GORM Save) latency.
func BenchmarkStore_Write_UpdateUser(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedUser(b, s, "u-update")

	u, err := s.GetUserByID("u-update")
	if err != nil {
		b.Fatalf("seed read failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		u.DisplayName = fmt.Sprintf("display-%d", i)
		u.UpdatedAt = time.Now().UTC()
		if err := s.UpdateUser(u); err != nil {
			b.Fatalf("UpdateUser failed at i=%d: %v", i, err)
		}
	}
}

// BenchmarkStore_Write_UpdateClusterStatus measures a targeted column UPDATE,
// the hot path used by the cluster health-check loop.
func BenchmarkStore_Write_UpdateClusterStatus(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()

	c := &ClusterModel{
		ID: "c-status", Name: "status-cluster", Provider: "aws", Status: "pending",
		Labels: `{}`, Annotations: `{}`, Config: `{}`,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateCluster(c); err != nil {
		b.Fatalf("seed CreateCluster failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.UpdateClusterStatus("c-status", "healthy", 32, 256); err != nil {
			b.Fatalf("UpdateClusterStatus failed at i=%d: %v", i, err)
		}
	}
}

// ----------------------------------------------------------------------------
// Batch write throughput
// ----------------------------------------------------------------------------

const benchBatchSize = 100

// BenchmarkStore_BatchWrite_AuditLogs_Batched uses CreateInBatches (one
// multi-row INSERT). b.N counts *rows*, so ns/op is per-row amortized cost.
func BenchmarkStore_BatchWrite_AuditLogs_Batched(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i += benchBatchSize {
		n := benchBatchSize
		if remaining := b.N - i; remaining < n {
			n = remaining
		}
		logs := make([]AuditLog, n)
		for j := 0; j < n; j++ {
			logs[j] = AuditLog{
				ID: fmt.Sprintf("al-b-%d", i+j), UserID: "u-1", Username: "bench",
				Action: "create", ResourceType: "workload", ResourceID: fmt.Sprintf("wl-%d", i+j),
				IPAddress: "10.0.0.1", Status: "success", CreatedAt: time.Now().UTC(),
			}
		}
		if err := s.BatchCreateAuditLogs(ctx, logs); err != nil {
			b.Fatalf("BatchCreateAuditLogs failed at i=%d: %v", i, err)
		}
	}

	b.StopTimer()
	reportThroughput(b, b.N, time.Since(start))
}

// BenchmarkStore_BatchWrite_AuditLogs_Serial is the row-at-a-time control for
// BenchmarkStore_BatchWrite_AuditLogs_Batched. The ratio quantifies the batching win.
func BenchmarkStore_BatchWrite_AuditLogs_Serial(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		log := &AuditLog{
			ID: fmt.Sprintf("al-s-%d", i), UserID: "u-1", Username: "bench",
			Action: "create", ResourceType: "workload", ResourceID: fmt.Sprintf("wl-%d", i),
			IPAddress: "10.0.0.1", Status: "success", CreatedAt: time.Now().UTC(),
		}
		if err := s.CreateAuditLog(log); err != nil {
			b.Fatalf("CreateAuditLog failed at i=%d: %v", i, err)
		}
	}

	b.StopTimer()
	reportThroughput(b, b.N, time.Since(start))
}

// ----------------------------------------------------------------------------
// Transaction commit latency
// ----------------------------------------------------------------------------

// BenchmarkStore_Txn_UpdateWorkloadStatus measures a real multi-statement
// transaction: guarded UPDATE + event INSERT, committed atomically.
// Status is toggled each iteration so the optimistic WHERE clause keeps matching.
func BenchmarkStore_Txn_UpdateWorkloadStatus(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()

	w := &WorkloadModel{
		ID: "wl-txn", Name: "txn-workload", Namespace: "default", ClusterID: "c-txn",
		Type: "training", Status: "pending", Priority: 10, Framework: "pytorch",
		Image: "pytorch:2.0", ResourceRequest: `{"gpu_count":8}`,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := s.CreateWorkload(w); err != nil {
		b.Fatalf("seed CreateWorkload failed: %v", err)
	}

	states := [2]string{"pending", "running"}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		from := states[i%2]
		to := states[(i+1)%2]
		if err := s.UpdateWorkloadStatus("wl-txn", from, to, "benchmark transition"); err != nil {
			b.Fatalf("UpdateWorkloadStatus failed at i=%d (%s->%s): %v", i, from, to, err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "txn/sec")
}

// BenchmarkStore_Txn_TwoPCCommit measures the 2PC coordinator's happy-path
// PREPARE+COMMIT latency across 3 in-memory participants. This isolates
// coordination overhead from storage I/O.
func BenchmarkStore_Txn_TwoPCCommit(b *testing.B) {
	cfg := DefaultTwoPCConfig()
	quiet := logrus.New()
	quiet.SetLevel(logrus.PanicLevel)
	cfg.Logger = quiet

	coord := NewTwoPCCoordinator(cfg)
	names := []string{"shard-a", "shard-b", "shard-c"}
	for _, n := range names {
		coord.RegisterParticipant(NewMemoryParticipant(n))
	}

	ctx := context.Background()
	ops := make([]TxnOperation, len(names))
	for i, n := range names {
		ops[i] = TxnOperation{
			Type: "insert", Table: "workloads", Key: fmt.Sprintf("k-%d", i),
			Data: []byte(`{"status":"running"}`), ParticipantName: n,
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		if _, err := coord.Execute(ctx, ops); err != nil {
			b.Fatalf("2PC Execute failed at i=%d: %v", i, err)
		}
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "txn/sec")
}

// ----------------------------------------------------------------------------
// Range / iteration query throughput
// ----------------------------------------------------------------------------

// seedWorkloads inserts n workloads spread across 4 statuses.
func seedWorkloads(b *testing.B, s *Store, clusterID string, n int) []string {
	b.Helper()
	statuses := [4]string{"pending", "running", "succeeded", "failed"}
	ids := make([]string, n)
	batch := make([]WorkloadModel, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("wl-seed-%d", i)
		ids[i] = id
		batch = append(batch, WorkloadModel{
			ID: id, Name: fmt.Sprintf("wl-%d", i), Namespace: "default",
			ClusterID: clusterID, Type: "training", Status: statuses[i%4],
			Priority: i % 100, Framework: "pytorch", Image: "pytorch:2.0",
			ResourceRequest: `{"gpu_count":8}`,
			CreatedAt:       time.Now().UTC().Add(-time.Duration(i) * time.Minute),
			UpdatedAt:       time.Now().UTC(),
		})
	}
	if err := s.db.CreateInBatches(batch, 500).Error; err != nil {
		b.Fatalf("seedWorkloads failed: %v", err)
	}
	return ids
}

// BenchmarkStore_Range_ListWorkloads_Page100 measures COUNT + paged SELECT over
// a 5000-row table — the dashboard list path.
func BenchmarkStore_Range_ListWorkloads_Page100(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedWorkloads(b, s, "c-range", 5000)

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	var rows int
	for i := 0; i < b.N; i++ {
		got, total, err := s.ListWorkloads("c-range", "", 0, 100)
		if err != nil {
			b.Fatalf("ListWorkloads failed at i=%d: %v", i, err)
		}
		if total != 5000 {
			b.Fatalf("total = %d, want 5000", total)
		}
		rows += len(got)
	}

	b.StopTimer()
	b.ReportMetric(float64(rows)/time.Since(start).Seconds(), "rows/sec")
}

// BenchmarkStore_Range_ListWorkloads_Filtered adds a status predicate, so the
// COUNT and SELECT both narrow to ~1/4 of the table.
func BenchmarkStore_Range_ListWorkloads_Filtered(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedWorkloads(b, s, "c-range", 5000)

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	var rows int
	for i := 0; i < b.N; i++ {
		got, _, err := s.ListWorkloads("c-range", "running", 0, 100)
		if err != nil {
			b.Fatalf("ListWorkloads failed at i=%d: %v", i, err)
		}
		rows += len(got)
	}

	b.StopTimer()
	b.ReportMetric(float64(rows)/time.Since(start).Seconds(), "rows/sec")
}

// BenchmarkStore_Range_ListWorkloads_DeepOffset probes offset-pagination decay
// (SQLite must walk and discard 4900 rows before returning page 50).
func BenchmarkStore_Range_ListWorkloads_DeepOffset(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedWorkloads(b, s, "c-range", 5000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.ListWorkloads("c-range", "", 4900, 100); err != nil {
			b.Fatalf("ListWorkloads failed at i=%d: %v", i, err)
		}
	}
}

// BenchmarkStore_Range_GetWorkloadsByIDs_100 measures a 100-key `IN (...)`
// multi-get, the batched alternative to 100 point reads.
func BenchmarkStore_Range_GetWorkloadsByIDs_100(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	ids := seedWorkloads(b, s, "c-range", 5000)
	lookup := ids[:100]
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	var rows int
	for i := 0; i < b.N; i++ {
		got, err := s.GetWorkloadsByIDs(ctx, lookup)
		if err != nil {
			b.Fatalf("GetWorkloadsByIDs failed at i=%d: %v", i, err)
		}
		if len(got) != 100 {
			b.Fatalf("got %d rows, want 100", len(got))
		}
		rows += len(got)
	}

	b.StopTimer()
	b.ReportMetric(float64(rows)/time.Since(start).Seconds(), "rows/sec")
}

// BenchmarkStore_Range_CountWorkloadsByStatus measures a GROUP BY aggregation
// over 5000 rows (dashboard summary path).
func BenchmarkStore_Range_CountWorkloadsByStatus(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedWorkloads(b, s, "c-range", 5000)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counts, err := s.CountWorkloadsByStatus(ctx, "")
		if err != nil {
			b.Fatalf("CountWorkloadsByStatus failed at i=%d: %v", i, err)
		}
		if len(counts) != 4 {
			b.Fatalf("got %d status groups, want 4", len(counts))
		}
	}
}

// ----------------------------------------------------------------------------
// Concurrent read path
// ----------------------------------------------------------------------------

// BenchmarkStore_Parallel_GetUserByID measures point-read throughput under
// GOMAXPROCS-way concurrency. Note: MaxOpenConns=1 means the driver serializes,
// so this quantifies connection-pool queueing cost, not parallel speedup.
func BenchmarkStore_Parallel_GetUserByID(b *testing.B) {
	s := newBenchStore(b)
	defer s.Close()
	seedUser(b, s, "u-parallel")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := s.GetUserByID("u-parallel"); err != nil {
				b.Errorf("GetUserByID failed: %v", err)
				return
			}
		}
	})
}
