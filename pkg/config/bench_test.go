package config

// bench_test.go measures the real cost of Module 8 (global configuration
// management) on the machine running `go test -bench`.
//
// Measurement honesty notes:
//
//   - Load() is built ON TOP OF spf13/viper (see config.go: viper.New,
//     SetDefault, AutomaticEnv, Unmarshal). Therefore these benchmarks do NOT
//     compare "us vs Viper" — that would be meaningless. Instead they measure
//     the OVERHEAD OUR LAYER ADDS on top of a raw Viper pipeline:
//     benchmarkRawViperFile is the raw-Viper control, BenchmarkLoadFromFile is
//     the same work through our Load(). The delta is our layer.
//
//   - logrus output is redirected to io.Discard during benchmarks. Load() logs
//     one line per validation warning; measuring the terminal/file writer would
//     measure the logger, not the configuration engine. Startup logging cost is
//     therefore EXCLUDED from the reported numbers.
//
//   - Load() in a development environment auto-generates a 32-byte random JWT
//     secret via crypto/rand (see ValidateStrict). That cost IS included in the
//     dev-path numbers, because it is real work a dev-mode boot performs.
//
//   - The feature-flag engine itself lives in pkg/feature (imported read-only
//     here); config.go only carries the feature_profile selector. Flag-query
//     benchmarks are included because feature flags are part of Module 8's
//     surface, but the implementation under test is pkg/feature.

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/feature"
)

// benchConfigYAML is a realistic operator config file covering every major
// section of Config (general, server, db, redis, kafka, nats, auth, ai,
// clickhouse, contrib, cloud providers, monitoring).
const benchConfigYAML = `
env: staging
log_level: debug
host: 0.0.0.0
port: 18080
read_timeout: 45s
write_timeout: 45s
metrics_port: 19100
scheduler_port: 18081
scheduling_interval: 15
agent_port: 18082
apiserver_addr: apiserver.svc:18080
db_host: pg.internal
db_port: 5432
db_name: cloudai_staging
db_user: cloudai_rw
db_password: xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6
db_sslmode: require
db_max_open_conns: 50
db_max_idle_conns: 20
redis_addr: redis.internal:6379
redis_password: r3d1s#Str0ng!Pass9
redis_db: 3
kafka_brokers: kafka-0.internal:9092,kafka-1.internal:9092
kafka_group_id: cloudai-staging
nats_url: nats://nats.internal:4222
nats_cluster: cloudai-staging
jwt_secret: a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6
jwt_expiry: 12h
ai_engine_addr: ai.internal:8090
ai_model_path: /var/lib/cloudai/models
ai_device: cuda
feature_profile: full
run_mode: degraded
evidence_key_path: /etc/cloudai-fusion/evidence.pem
rekor_url: https://rekor.sigstore.dev
clickhouse_endpoint: http://clickhouse.internal:8123
clickhouse_db: security
clickhouse_user: intel_rw
clickhouse_password: ch#Str0ngPass!27
edr_real_collector: true
gateway_enable_ip_acl: true
soar_cluster_apply: false
contrib:
  dr_primary_host: pg-primary.internal
  dr_standby_host: pg-standby.internal
  dr_lag_threshold_seconds: 45
  cs_base_url: https://cs.internal
  cs_api_key: cs#Str0ngKey!91
  cs_threat_threshold: 0.42
  cs_max_requests_per_minute: 120
  render_farms:
    - name: farm-hangzhou
      base_url: https://farm-hz.internal
      cloud_provider: aliyun
      region: cn-hangzhou
      spot_price_usd: 0.42
cloud_providers:
  - name: aliyun-prod
    type: aliyun
    region: cn-hangzhou
    access_key_id: ak-bench
    access_key_secret: sk#Str0ngSecret!77
prometheus_endpoint: http://prometheus.internal:9090
grafana_endpoint: http://grafana.internal:3000
jaeger_endpoint: http://jaeger.internal:4317
`

// quietLogs silences logrus for the duration of a benchmark so the numbers
// reflect configuration work rather than log-writer throughput.
func quietLogs(b *testing.B) {
	b.Helper()
	prev := logrus.StandardLogger().Out
	logrus.SetOutput(io.Discard)
	b.Cleanup(func() { logrus.SetOutput(prev) })
}

// writeBenchConfig materialises benchConfigYAML in a temp dir and returns its path.
func writeBenchConfig(b *testing.B) string {
	b.Helper()
	path := filepath.Join(b.TempDir(), "cloudai-fusion.yaml")
	if err := os.WriteFile(path, []byte(benchConfigYAML), 0o600); err != nil {
		b.Fatalf("write bench config: %v", err)
	}
	return path
}

// benchCmd builds a cobra command carrying --config, mirroring how the real
// binaries (cmd/apiserver, cmd/scheduler, cmd/agent) invoke Load.
func benchCmd(b *testing.B, configPath string) *cobra.Command {
	b.Helper()
	cmd := &cobra.Command{Use: "bench"}
	cmd.Flags().String("config", configPath, "config file")
	return cmd
}

// strongProdEnv sets credentials strong enough to pass ValidateStrict without
// warnings, isolating the parse path from the dev-secret generation path.
func strongProdEnv(b *testing.B) {
	b.Helper()
	b.Setenv("CLOUDAI_ENV", "staging")
	b.Setenv("CLOUDAI_DB_PASSWORD", "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6")
	b.Setenv("CLOUDAI_JWT_SECRET", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6")
	b.Setenv("CLOUDAI_DB_SSLMODE", "require")
}

// ---------------------------------------------------------------------------
// Load latency: defaults, file, env
// ---------------------------------------------------------------------------

// BenchmarkLoadDefaults measures a zero-config boot: defaults only, no config
// file, development env (includes crypto/rand dev-secret generation).
func BenchmarkLoadDefaults(b *testing.B) {
	quietLogs(b)
	b.ReportAllocs()
	for b.Loop() {
		cfg, err := Load(nil)
		if err != nil {
			b.Fatalf("Load: %v", err)
		}
		if cfg.Port != 8080 {
			b.Fatalf("unexpected port %d", cfg.Port)
		}
	}
}

// BenchmarkLoadDefaultsStrongSecrets measures the same zero-config boot with
// credentials supplied, so no dev secret is generated and no warning is logged.
func BenchmarkLoadDefaultsStrongSecrets(b *testing.B) {
	quietLogs(b)
	strongProdEnv(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Load(nil); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}

// BenchmarkLoadFromFile measures a full operator-grade boot: read the YAML from
// disk, bind env, bind CLI flags, unmarshal, validate.
func BenchmarkLoadFromFile(b *testing.B) {
	quietLogs(b)
	path := writeBenchConfig(b)
	cmd := benchCmd(b, path)
	b.ReportAllocs()
	for b.Loop() {
		cfg, err := Load(cmd)
		if err != nil {
			b.Fatalf("Load: %v", err)
		}
		if cfg.Port != 18080 || cfg.DBHost != "pg.internal" {
			b.Fatalf("config not applied: port=%d db=%s", cfg.Port, cfg.DBHost)
		}
	}
}

// BenchmarkLoadFromFileWithEnvOverrides measures the same file-based boot with
// 12 CLOUDAI_* overrides in the environment. Comparing against
// BenchmarkLoadFromFile isolates the env-override resolution cost.
func BenchmarkLoadFromFileWithEnvOverrides(b *testing.B) {
	quietLogs(b)
	path := writeBenchConfig(b)
	cmd := benchCmd(b, path)

	b.Setenv("CLOUDAI_ENV", "staging")
	b.Setenv("CLOUDAI_LOG_LEVEL", "warn")
	b.Setenv("CLOUDAI_PORT", "28080")
	b.Setenv("CLOUDAI_METRICS_PORT", "29100")
	b.Setenv("CLOUDAI_DB_HOST", "pg-override.internal")
	b.Setenv("CLOUDAI_DB_PORT", "6432")
	b.Setenv("CLOUDAI_DB_PASSWORD", "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6")
	b.Setenv("CLOUDAI_DB_SSLMODE", "verify-full")
	b.Setenv("CLOUDAI_REDIS_ADDR", "redis-override.internal:6379")
	b.Setenv("CLOUDAI_JWT_SECRET", "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6")
	b.Setenv("CLOUDAI_AI_DEVICE", "cuda")
	b.Setenv("CLOUDAI_RUN_MODE", "degraded")

	b.ReportAllocs()
	for b.Loop() {
		cfg, err := Load(cmd)
		if err != nil {
			b.Fatalf("Load: %v", err)
		}
		// Assert the override actually won over the file value (28080 vs 18080).
		if cfg.Port != 28080 || cfg.DBHost != "pg-override.internal" {
			b.Fatalf("env override not applied: port=%d db=%s", cfg.Port, cfg.DBHost)
		}
	}
}

// ---------------------------------------------------------------------------
// Wrapper-overhead control: raw Viper doing the same job
// ---------------------------------------------------------------------------

// BenchmarkRawViperFile is the control for BenchmarkLoadFromFile. It performs
// the same Viper pipeline (read file, bind env, unmarshal) WITHOUT our added
// layer: no ~90 SetDefault registrations, no CLI flag binding, no security
// validation. Load() - RawViper = the cost our layer adds.
func BenchmarkRawViperFile(b *testing.B) {
	path := writeBenchConfig(b)
	b.ReportAllocs()
	for b.Loop() {
		v := viper.New()
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			b.Fatalf("ReadInConfig: %v", err)
		}
		v.SetEnvPrefix("CLOUDAI")
		v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
		v.AutomaticEnv()
		cfg := &Config{}
		if err := v.Unmarshal(cfg); err != nil {
			b.Fatalf("Unmarshal: %v", err)
		}
		if cfg.Port != 18080 {
			b.Fatalf("unexpected port %d", cfg.Port)
		}
	}
}

// BenchmarkSetDefaults isolates the cost of registering the platform's default
// set (~90 keys) into a fresh Viper instance — the largest single component of
// our wrapper's added work.
func BenchmarkSetDefaults(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		v := viper.New()
		setDefaults(v)
		if v.GetInt("port") != 8080 {
			b.Fatal("defaults not registered")
		}
	}
}

// ---------------------------------------------------------------------------
// Validation latency
// ---------------------------------------------------------------------------

// BenchmarkValidateStrictProdClean measures the security gate on a compliant
// production config (all checks run, no findings).
func BenchmarkValidateStrictProdClean(b *testing.B) {
	cfg := &Config{
		Env:        "production",
		DBPassword: "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6",
		JWTSecret:  "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",
		DBSSLMode:  "require",
	}
	b.ReportAllocs()
	for b.Loop() {
		if r := cfg.ValidateStrict(); r.HasErrors() {
			b.Fatalf("unexpected errors: %v", r.Errors)
		}
	}
}

// BenchmarkValidateStrictProdFindings measures the worst case: insecure values
// that trip the placeholder scan, the length gate and the entropy computation.
func BenchmarkValidateStrictProdFindings(b *testing.B) {
	cfg := &Config{
		Env:        "production",
		DBPassword: "cloudai_secret",
		JWTSecret:  "abababababababababababababababababab",
		DBSSLMode:  "disable",
		CloudProviders: []CloudProviderConfig{
			{Name: "aliyun-prod", AccessKeyID: "ak", AccessKeySecret: "changeme"},
		},
	}
	b.ReportAllocs()
	for b.Loop() {
		if r := cfg.ValidateStrict(); !r.HasErrors() {
			b.Fatal("expected errors")
		}
	}
}

// BenchmarkShannonEntropy isolates the entropy estimator used to reject
// low-randomness JWT secrets.
func BenchmarkShannonEntropy(b *testing.B) {
	const key = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0"
	b.ReportAllocs()
	for b.Loop() {
		if shannonEntropy(key) <= 0 {
			b.Fatal("expected positive entropy")
		}
	}
}

// BenchmarkIsInsecureDefault isolates the placeholder/weak-secret scan
// (16 substring probes plus the repeated-character check).
func BenchmarkIsInsecureDefault(b *testing.B) {
	const key = "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6"
	b.ReportAllocs()
	for b.Loop() {
		if isInsecureDefault(key) {
			b.Fatal("false positive")
		}
	}
}

// ---------------------------------------------------------------------------
// Hot-path accessors (called per request / per reconcile, not per boot)
// ---------------------------------------------------------------------------

func BenchmarkEffectiveRunMode(b *testing.B) {
	cfg := &Config{Env: "production"}
	b.ReportAllocs()
	for b.Loop() {
		_ = cfg.EffectiveRunMode()
	}
}

func BenchmarkDatabaseDSN(b *testing.B) {
	cfg := &Config{
		DBHost: "pg.internal", DBPort: 5432, DBName: "cloudai",
		DBUser: "cloudai_rw", DBPassword: "secret", DBSSLMode: "require",
	}
	b.ReportAllocs()
	for b.Loop() {
		if cfg.DatabaseDSN() == "" {
			b.Fatal("empty DSN")
		}
	}
}

// ---------------------------------------------------------------------------
// Feature flags (engine lives in pkg/feature; exercised read-only)
// ---------------------------------------------------------------------------

// BenchmarkFeatureManagerInit measures one-time flag-engine startup: register
// the built-in flags, apply the profile, then scan the environment for
// CLOUDAI_FF_* overrides (one os.LookupEnv per registered flag).
func BenchmarkFeatureManagerInit(b *testing.B) {
	quietLogs(b)
	b.ReportAllocs()
	for b.Loop() {
		m := feature.NewManager(feature.Config{})
		if m == nil {
			b.Fatal("nil manager")
		}
	}
}

// BenchmarkFeatureIsEnabled measures the steady-state query path: RLock plus
// map lookup. This runs on request-handling hot paths.
func BenchmarkFeatureIsEnabled(b *testing.B) {
	quietLogs(b)
	m := feature.NewManager(feature.Config{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = m.IsEnabled("distributed_tracing")
	}
}

// BenchmarkFeatureIsEnabledMiss measures the unknown-flag path (map miss).
func BenchmarkFeatureIsEnabledMiss(b *testing.B) {
	quietLogs(b)
	m := feature.NewManager(feature.Config{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if m.IsEnabled("no_such_flag_xyz") {
			b.Fatal("unknown flag reported enabled")
		}
	}
}

// BenchmarkFeatureIsEnabledForRollout measures the percentage-rollout path,
// which additionally hashes the entity id for a stable bucket assignment.
func BenchmarkFeatureIsEnabledForRollout(b *testing.B) {
	quietLogs(b)
	m := feature.NewManager(feature.Config{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = m.IsEnabledFor("auto_scaling", "tenant-42")
	}
}

// BenchmarkFeatureIsEnabledParallel measures concurrent read scalability of the
// RWMutex-guarded flag map — the realistic multi-goroutine server pattern.
func BenchmarkFeatureIsEnabledParallel(b *testing.B) {
	quietLogs(b)
	m := feature.NewManager(feature.Config{})
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = m.IsEnabled("distributed_tracing")
		}
	})
}

// ---------------------------------------------------------------------------
// Evidence-native config barrier (pkg/config/evidence_config.go)
// ---------------------------------------------------------------------------

// BenchmarkEvidenceConfigSetConfig measures a config mutation that is sealed
// into a signed evidence receipt — the capability plain Viper/Consul/etcd
// clients do not provide, and the dominant cost is the Ed25519 signature.
func BenchmarkEvidenceConfigSetConfig(b *testing.B) {
	eng := NewEvidenceConfigEngine()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		res, err := eng.SetConfig("db_max_open_conns", 25, 50+i)
		if err != nil {
			b.Fatalf("SetConfig: %v", err)
		}
		if res.Receipt == nil {
			b.Fatal("expected signed receipt")
		}
		i++
	}
}

// BenchmarkEvidenceConfigBlastRadius measures the blast-radius snapshot over a
// 50-service / 10-key dependency graph.
func BenchmarkEvidenceConfigBlastRadius(b *testing.B) {
	eng := NewEvidenceConfigEngine()
	keys := []string{
		"db_host", "db_port", "redis_addr", "kafka_brokers", "nats_url",
		"jwt_secret", "ai_engine_addr", "feature_profile", "run_mode", "log_level",
	}
	for i := 0; i < 50; i++ {
		eng.RegisterService("svc-"+string(rune('a'+i%26))+string(rune('0'+i/26)), keys[:1+i%len(keys)])
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		m := eng.ComputeBlastRadiusMap()
		if m.TotalServices == 0 {
			b.Fatal("expected registered services")
		}
	}
}
