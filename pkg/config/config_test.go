package config

import (
	"testing"
)

func TestDatabaseURL(t *testing.T) {
	cfg := &Config{
		DBHost:     "pg.example.com",
		DBPort:     5432,
		DBName:     "testdb",
		DBUser:     "testuser",
		DBPassword: "testpass",
		DBSSLMode:  "require",
	}
	url := cfg.DatabaseURL()
	expected := "host=pg.example.com port=5432 dbname=testdb user=testuser password=testpass sslmode=require"
	if url != expected {
		t.Errorf("DatabaseURL mismatch:\n  got:  %q\n  want: %q", url, expected)
	}
}

func TestDatabaseDSN(t *testing.T) {
	cfg := &Config{
		DBHost:     "localhost",
		DBPort:     5432,
		DBName:     "cloudai",
		DBUser:     "cloudai",
		DBPassword: "secret",
		DBSSLMode:  "disable",
	}
	dsn := cfg.DatabaseDSN()
	expected := "host=localhost user=cloudai password=secret dbname=cloudai port=5432 sslmode=disable TimeZone=UTC"
	if dsn != expected {
		t.Errorf("DatabaseDSN mismatch:\n  got:  %q\n  want: %q", dsn, expected)
	}
}

func TestLoadWithNilCmd(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatalf("Load(nil) failed: %v", err)
	}

	// Check defaults
	if cfg.Env != "development" {
		t.Errorf("expected env 'development', got %q", cfg.Env)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected log level 'info', got %q", cfg.LogLevel)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("expected host '0.0.0.0', got %q", cfg.Host)
	}
	if cfg.MetricsPort != 9100 {
		t.Errorf("expected metrics port 9100, got %d", cfg.MetricsPort)
	}
}

func TestLoadDatabaseDefaults(t *testing.T) {
	cfg, _ := Load(nil)

	if cfg.DBHost != "localhost" {
		t.Errorf("expected db host 'localhost', got %q", cfg.DBHost)
	}
	if cfg.DBPort != 5432 {
		t.Errorf("expected db port 5432, got %d", cfg.DBPort)
	}
	if cfg.DBName != "cloudai" {
		t.Errorf("expected db name 'cloudai', got %q", cfg.DBName)
	}
	if cfg.DBUser != "cloudai" {
		t.Errorf("expected db user 'cloudai', got %q", cfg.DBUser)
	}
	// db_password default is now empty (must be set via env var)
	if cfg.DBPassword != "" {
		t.Errorf("expected db password '' (empty default), got %q", cfg.DBPassword)
	}
	if cfg.DBSSLMode != "disable" {
		t.Errorf("expected sslmode 'disable', got %q", cfg.DBSSLMode)
	}
	if cfg.DBMaxOpenConns != 25 {
		t.Errorf("expected max open conns 25, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 10 {
		t.Errorf("expected max idle conns 10, got %d", cfg.DBMaxIdleConns)
	}
}

func TestLoadRedisDefaults(t *testing.T) {
	cfg, _ := Load(nil)
	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("expected redis addr 'localhost:6379', got %q", cfg.RedisAddr)
	}
	if cfg.RedisDB != 0 {
		t.Errorf("expected redis db 0, got %d", cfg.RedisDB)
	}
}

func TestLoadKafkaDefaults(t *testing.T) {
	cfg, _ := Load(nil)
	if cfg.KafkaBrokers != "localhost:9092" {
		t.Errorf("expected kafka brokers 'localhost:9092', got %q", cfg.KafkaBrokers)
	}
	if cfg.KafkaGroupID != "cloudai-fusion" {
		t.Errorf("expected group id 'cloudai-fusion', got %q", cfg.KafkaGroupID)
	}
}

func TestLoadSchedulerDefaults(t *testing.T) {
	cfg, _ := Load(nil)
	if cfg.SchedulerPort != 8081 {
		t.Errorf("expected scheduler port 8081, got %d", cfg.SchedulerPort)
	}
	if cfg.SchedulingInterval != 10 {
		t.Errorf("expected scheduling interval 10, got %d", cfg.SchedulingInterval)
	}
}

func TestLoadAgentDefaults(t *testing.T) {
	cfg, _ := Load(nil)
	if cfg.AgentPort != 8082 {
		t.Errorf("expected agent port 8082, got %d", cfg.AgentPort)
	}
	if cfg.APIServerAddr != "localhost:8080" {
		t.Errorf("expected apiserver addr 'localhost:8080', got %q", cfg.APIServerAddr)
	}
}

func TestLoadAIDefaults(t *testing.T) {
	cfg, _ := Load(nil)
	if cfg.AIEngineAddr != "localhost:8090" {
		t.Errorf("expected AI engine addr 'localhost:8090', got %q", cfg.AIEngineAddr)
	}
	if cfg.AIModelPath != "./models" {
		t.Errorf("expected model path './models', got %q", cfg.AIModelPath)
	}
	if cfg.AIDevice != "cpu" {
		t.Errorf("expected device 'cpu', got %q", cfg.AIDevice)
	}
}

func TestLoadMonitoringDefaults(t *testing.T) {
	cfg, _ := Load(nil)
	if cfg.PrometheusEndpoint != "http://localhost:9090" {
		t.Errorf("unexpected prometheus endpoint: %q", cfg.PrometheusEndpoint)
	}
	if cfg.GrafanaEndpoint != "http://localhost:3000" {
		t.Errorf("unexpected grafana endpoint: %q", cfg.GrafanaEndpoint)
	}
	if cfg.JaegerEndpoint != "http://localhost:4317" {
		t.Errorf("unexpected jaeger endpoint: %q", cfg.JaegerEndpoint)
	}
}

func TestCloudProviderConfigStruct(t *testing.T) {
	cpc := CloudProviderConfig{
		Name:            "aliyun-prod",
		Type:            "aliyun",
		Region:          "cn-hangzhou",
		AccessKeyID:     "ak-test",
		AccessKeySecret: "sk-test",
		Extra:           map[string]string{"vpc_id": "vpc-123"},
	}
	if cpc.Name != "aliyun-prod" {
		t.Error("Name mismatch")
	}
	if cpc.Type != "aliyun" {
		t.Error("Type mismatch")
	}
	if cpc.Extra["vpc_id"] != "vpc-123" {
		t.Error("Extra mismatch")
	}
}

// ---------------------------------------------------------------------------
// Validate tests
// ---------------------------------------------------------------------------

func TestValidateDevEmptySecrets(t *testing.T) {
	cfg := &Config{Env: "development", DBPassword: "", JWTSecret: ""}
	warnings := cfg.Validate()

	// Should auto-generate jwt_secret in dev
	if cfg.JWTSecret == "" {
		t.Error("expected jwt_secret to be auto-generated in development")
	}
	if len(cfg.JWTSecret) < 32 {
		t.Errorf("auto-generated jwt_secret too short: %d", len(cfg.JWTSecret))
	}

	// Should have warnings about empty db_password and auto-gen jwt
	if len(warnings) < 2 {
		t.Errorf("expected at least 2 warnings, got %d: %v", len(warnings), warnings)
	}
}

func TestValidateProdEmptySecrets(t *testing.T) {
	cfg := &Config{Env: "production", DBPassword: "", JWTSecret: ""}
	result := cfg.ValidateStrict()

	// Should have fatal errors (not just warnings) in production
	if !result.HasErrors() {
		t.Fatal("expected errors for empty secrets in production")
	}

	// Count CRITICAL errors
	criticalCount := 0
	for _, e := range result.Errors {
		if len(e) > 8 && e[:8] == "CRITICAL" {
			criticalCount++
		}
	}
	if criticalCount < 2 {
		t.Errorf("expected at least 2 CRITICAL errors in production, got %d: %v", criticalCount, result.Errors)
	}

	// ToError should return a non-nil error
	if result.ToError() == nil {
		t.Error("expected ToError() to return non-nil error")
	}
}

func TestValidateProdInsecureDefaults(t *testing.T) {
	cfg := &Config{Env: "production", DBPassword: "cloudai_secret", JWTSecret: "change-this-in-production-32-byte-key!!!"}
	result := cfg.ValidateStrict()

	if !result.HasErrors() {
		t.Fatal("expected errors for insecure defaults in production")
	}

	hasDBErr := false
	hasJWTErr := false
	for _, e := range result.Errors {
		if contains(e, "db_password") {
			hasDBErr = true
		}
		if contains(e, "jwt_secret") {
			hasJWTErr = true
		}
	}
	if !hasDBErr {
		t.Errorf("expected CRITICAL error about insecure db_password in production, got: %v", result.Errors)
	}
	if !hasJWTErr {
		t.Errorf("expected CRITICAL error about insecure jwt_secret in production, got: %v", result.Errors)
	}
}

func TestValidateProdStrongSecrets(t *testing.T) {
	cfg := &Config{
		Env:        "production",
		DBPassword: "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6",
		JWTSecret:  "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
		DBSSLMode:  "require",
	}
	result := cfg.ValidateStrict()
	if result.HasErrors() {
		t.Errorf("expected no errors for strong production config, got: %v", result.Errors)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings for strong production config, got: %v", result.Warnings)
	}
}

func TestValidateProdSSLDisabled(t *testing.T) {
	cfg := &Config{
		Env:        "production",
		DBPassword: "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6",
		JWTSecret:  "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
		DBSSLMode:  "disable",
	}
	result := cfg.ValidateStrict()
	hasSSL := false
	for _, w := range result.Warnings {
		if contains(w, "sslmode") {
			hasSSL = true
		}
	}
	if !hasSSL {
		t.Error("expected warning about disabled SSL in production")
	}
}

func TestIsInsecureDefault(t *testing.T) {
	tests := []struct {
		val    string
		unsafe bool
	}{
		{"cloudai_secret", true},
		{"change-this-in-production-foo", true},
		{"dev-jwt-secret-xxx", true},
		{"changeme", true},
		{"password", true},
		{"change-this-to-a-strong-random-key-at-least-32-bytes", true}, // .env.example placeholder
		{"dev-fallback-secret-not-for-production", true},
		{"your-secret-here-xxx", true},
		{"replace-me-with-real-key", true},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},  // 31 chars, not 32 — skip repeated-char check
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true}, // 34 repeated chars
		{"xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6", false},
		{"my-strong-prod-key-2024", false},
	}
	for _, tt := range tests {
		got := isInsecureDefault(tt.val)
		if got != tt.unsafe {
			t.Errorf("isInsecureDefault(%q) = %v, want %v", tt.val, got, tt.unsafe)
		}
	}
}

func TestGenerateDevSecret(t *testing.T) {
	s1 := generateDevSecret()
	s2 := generateDevSecret()
	if len(s1) < 32 {
		t.Errorf("generated secret too short: %d", len(s1))
	}
	if s1 == s2 {
		t.Error("two generated secrets should differ")
	}
}

// ---------------------------------------------------------------------------
// ValidateStrict / ValidateResult tests
// ---------------------------------------------------------------------------

func TestValidateStrictProdBlocksStartup(t *testing.T) {
	cfg := &Config{Env: "production", DBPassword: "", JWTSecret: ""}
	result := cfg.ValidateStrict()
	if !result.HasErrors() {
		t.Fatal("expected ValidateStrict to report errors for empty prod secrets")
	}
	if result.ToError() == nil {
		t.Fatal("expected ToError() to return non-nil")
	}
	if !contains(result.ToError().Error(), "security validation failed") {
		t.Errorf("unexpected error message: %s", result.ToError().Error())
	}
}

func TestValidateStrictDevNoErrors(t *testing.T) {
	cfg := &Config{Env: "development", DBPassword: "", JWTSecret: ""}
	result := cfg.ValidateStrict()
	if result.HasErrors() {
		t.Errorf("expected no errors in development, got: %v", result.Errors)
	}
	// Should have warnings
	if len(result.Warnings) == 0 {
		t.Error("expected warnings in development")
	}
}

func TestShannonEntropy(t *testing.T) {
	// Low entropy: repeated characters
	lowE := shannonEntropy("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if lowE != 0 {
		t.Errorf("expected 0 entropy for repeated chars, got %.2f", lowE)
	}

	// High entropy: random hex
	highE := shannonEntropy("a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6")
	if highE < 3.0 {
		t.Errorf("expected high entropy for mixed hex, got %.2f", highE)
	}

	// Empty string
	if shannonEntropy("") != 0 {
		t.Error("expected 0 entropy for empty string")
	}
}

func TestValidateProdLowEntropySecret(t *testing.T) {
	cfg := &Config{
		Env:        "production",
		DBPassword: "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6",
		JWTSecret:  "abababababababababababababababababab", // 32+ bytes but low entropy
		DBSSLMode:  "require",
	}
	result := cfg.ValidateStrict()
	hasEntropy := false
	for _, e := range result.Errors {
		if contains(e, "entropy") {
			hasEntropy = true
		}
	}
	if !hasEntropy {
		t.Error("expected low entropy error for repetitive jwt_secret in production")
	}
}

func TestValidateEnvExamplePlaceholder(t *testing.T) {
	// Test that the .env.example placeholder is detected
	cfg := &Config{
		Env:        "production",
		DBPassword: "xK9#mP2$vL7@nQ4!bR8&wJ5^tF3*hY6",
		JWTSecret:  "change-this-to-a-strong-random-key-at-least-32-bytes",
		DBSSLMode:  "require",
	}
	result := cfg.ValidateStrict()
	if !result.HasErrors() {
		t.Error("expected error for .env.example JWT placeholder in production")
	}
}

func TestLoadProdEmptySecretsReturnsError(t *testing.T) {
	// Temporarily set env vars
	t.Setenv("CLOUDAI_ENV", "production")
	// Don't set CLOUDAI_JWT_SECRET or CLOUDAI_DB_PASSWORD

	_, err := Load(nil)
	if err == nil {
		t.Fatal("expected Load to return error for empty secrets in production")
	}
	if !contains(err.Error(), "security validation failed") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
