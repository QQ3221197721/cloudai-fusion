// Package config provides unified configuration loading for all CloudAI Fusion services.
// Supports environment variables, config files (YAML/JSON), and CLI flags.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Config holds all configuration for CloudAI Fusion services
type Config struct {
	// General
	Env      string `mapstructure:"env"`
	LogLevel string `mapstructure:"log_level"`

	// API Server
	Host         string        `mapstructure:"host"`
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
	MetricsPort  int           `mapstructure:"metrics_port"`

	// Scheduler
	SchedulerPort      int `mapstructure:"scheduler_port"`
	SchedulingInterval int `mapstructure:"scheduling_interval"`

	// Agent
	AgentPort     int    `mapstructure:"agent_port"`
	APIServerAddr string `mapstructure:"apiserver_addr"`

	// Database
	DBHost         string `mapstructure:"db_host"`
	DBPort         int    `mapstructure:"db_port"`
	DBName         string `mapstructure:"db_name"`
	DBUser         string `mapstructure:"db_user"`
	DBPassword     string `mapstructure:"db_password"`     //nolint:gosec // G101: config field, not a hardcoded credential
	DBSSLMode      string `mapstructure:"db_sslmode"`
	DBMaxOpenConns int    `mapstructure:"db_max_open_conns"`
	DBMaxIdleConns int    `mapstructure:"db_max_idle_conns"`

	// Redis
	RedisAddr     string `mapstructure:"redis_addr"`
	RedisPassword string `mapstructure:"redis_password"` //nolint:gosec // G101: config field, not a hardcoded credential
	RedisDB       int    `mapstructure:"redis_db"`

	// Kafka
	KafkaBrokers string `mapstructure:"kafka_brokers"`
	KafkaGroupID string `mapstructure:"kafka_group_id"`

	// NATS (real-time messaging)
	NATSURL     string `mapstructure:"nats_url"`
	NATSCluster string `mapstructure:"nats_cluster"`

	// Authentication
	JWTSecret string        `mapstructure:"jwt_secret"` //nolint:gosec // G101: config field, not a hardcoded credential
	JWTExpiry time.Duration `mapstructure:"jwt_expiry"`

	// AI Engine
	AIEngineAddr string `mapstructure:"ai_engine_addr"`
	AIModelPath  string `mapstructure:"ai_model_path"`
	AIDevice     string `mapstructure:"ai_device"`

	// Feature Flags
	// Profile selects a predefined feature set: "minimal", "standard" (default), "full".
	// Individual flags can be overridden via CLOUDAI_FF_<KEY>=true|false env vars.
	FeatureProfile string `mapstructure:"feature_profile"`

	// Cloud Providers
	CloudProviders []CloudProviderConfig `mapstructure:"cloud_providers"`

	// Monitoring
	PrometheusEndpoint string `mapstructure:"prometheus_endpoint"`
	GrafanaEndpoint    string `mapstructure:"grafana_endpoint"`
	JaegerEndpoint     string `mapstructure:"jaeger_endpoint"`
}

// CloudProviderConfig holds cloud provider specific configuration
type CloudProviderConfig struct {
	Name            string            `mapstructure:"name"`
	Type            string            `mapstructure:"type"` // aliyun, aws, azure, gcp, huawei, tencent
	Region          string            `mapstructure:"region"`
	AccessKeyID     string            `mapstructure:"access_key_id"`
	AccessKeySecret string            `mapstructure:"access_key_secret"` //nolint:gosec // G101: config field, not a hardcoded credential
	Extra           map[string]string `mapstructure:"extra"`
}

// DatabaseURL constructs the PostgreSQL connection string
func (c *Config) DatabaseURL() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBName, c.DBUser, c.DBPassword, c.DBSSLMode,
	)
}

// DatabaseDSN returns a DSN string for GORM
func (c *Config) DatabaseDSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=UTC",
		c.DBHost, c.DBUser, c.DBPassword, c.DBName, c.DBPort, c.DBSSLMode,
	)
}

// Load reads configuration from file, environment variables, and CLI flags
func Load(cmd *cobra.Command) (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read config file if specified
	if cmd != nil {
		configFile, _ := cmd.Flags().GetString("config")
		if configFile != "" {
			v.SetConfigFile(configFile)
		} else {
			v.SetConfigName("cloudai-fusion")
			v.SetConfigType("yaml")
			v.AddConfigPath(".")
			v.AddConfigPath("/etc/cloudai-fusion/")
			v.AddConfigPath("$HOME/.cloudai-fusion/")
		}
	}

	// Read config file (ignore file not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Only return error if it's not a "file not found" error
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Bind environment variables
	v.SetEnvPrefix("CLOUDAI")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind CLI flags
	if cmd != nil {
		v.BindPFlags(cmd.Flags())
	}

	// Unmarshal config
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate sensitive fields
	result := cfg.ValidateStrict()
	for _, w := range result.Warnings {
		logrus.Warn(w)
	}
	for _, e := range result.Errors {
		logrus.Error(e)
	}
	// In production, refuse to start with critical security issues
	if result.HasErrors() {
		return nil, fmt.Errorf("security validation failed in %s environment: %s",
			cfg.Env, strings.Join(result.Errors, "; "))
	}

	return cfg, nil
}

// ValidateResult holds the result of configuration validation
type ValidateResult struct {
	Warnings []string // Non-fatal warnings (logged but don't block startup)
	Errors   []string // Fatal errors (block startup in production)
}

// HasErrors returns true if any fatal validation errors were found
func (r *ValidateResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// ToError converts fatal validation errors to a single Go error
func (r *ValidateResult) ToError() error {
	if !r.HasErrors() {
		return nil
	}
	return errors.New("security validation failed: " + strings.Join(r.Errors, "; "))
}

// AllMessages returns all warnings and errors combined (for backward compatibility)
func (r *ValidateResult) AllMessages() []string {
	return append(r.Warnings, r.Errors...)
}

// Validate checks configuration for security issues.
// Returns a list of warnings (backward compatible).
// Use ValidateStrict() for production environments that should block on critical issues.
func (c *Config) Validate() []string {
	result := c.ValidateStrict()
	return result.AllMessages()
}

// ValidateStrict performs comprehensive security validation.
// In production, critical issues are returned as errors (not just warnings).
func (c *Config) ValidateStrict() *ValidateResult {
	result := &ValidateResult{}
	isProd := c.Env != "development" && c.Env != "test" && c.Env != ""

	// db_password checks
	if c.DBPassword == "" {
		if isProd {
			result.Errors = append(result.Errors, "CRITICAL: db_password is empty in production — set CLOUDAI_DB_PASSWORD environment variable")
		} else {
			result.Warnings = append(result.Warnings, "db_password is empty — set CLOUDAI_DB_PASSWORD for local development")
		}
	} else if isInsecureDefault(c.DBPassword) {
		if isProd {
			result.Errors = append(result.Errors, "CRITICAL: db_password is using an insecure default value in production — set CLOUDAI_DB_PASSWORD to a strong password")
		} else {
			result.Warnings = append(result.Warnings, "db_password is using a known insecure default — change before deploying")
		}
	}

	// jwt_secret checks
	if c.JWTSecret == "" {
		if isProd {
			result.Errors = append(result.Errors, "CRITICAL: jwt_secret is empty in production — set CLOUDAI_JWT_SECRET environment variable")
		} else {
			// Auto-generate a random secret for development convenience
			c.JWTSecret = generateDevSecret()
			result.Warnings = append(result.Warnings, "jwt_secret auto-generated for development — set CLOUDAI_JWT_SECRET in production")
		}
	} else if isInsecureDefault(c.JWTSecret) {
		if isProd {
			result.Errors = append(result.Errors, "CRITICAL: jwt_secret is using an insecure default value in production — set CLOUDAI_JWT_SECRET to a 32+ byte random key")
		} else {
			result.Warnings = append(result.Warnings, "jwt_secret is using a known insecure default — change before deploying")
		}
	}

	// jwt_secret minimum length (prod = error, dev = warning)
	if c.JWTSecret != "" && len(c.JWTSecret) < 32 {
		if isProd {
			result.Errors = append(result.Errors, "jwt_secret must be at least 32 bytes in production for adequate security")
		} else {
			result.Warnings = append(result.Warnings, "jwt_secret should be at least 32 bytes in production for adequate security")
		}
	}

	// jwt_secret entropy check — detect low-entropy keys even if they pass length check
	if isProd && c.JWTSecret != "" && len(c.JWTSecret) >= 32 && !isInsecureDefault(c.JWTSecret) {
		if entropy := shannonEntropy(c.JWTSecret); entropy < 3.0 {
			result.Errors = append(result.Errors, fmt.Sprintf(
				"jwt_secret has low entropy (%.2f bits/char) — use a cryptographically random key (openssl rand -hex 32)", entropy))
		}
	}

	// SSL mode for production DB
	if isProd && (c.DBSSLMode == "disable" || c.DBSSLMode == "") {
		result.Warnings = append(result.Warnings, "db_sslmode is 'disable' — strongly recommend 'require' or 'verify-full' in production")
	}

	// Cloud provider credentials check
	for _, cp := range c.CloudProviders {
		if isProd && cp.AccessKeyID != "" && isInsecureDefault(cp.AccessKeySecret) {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"cloud provider %q access_key_secret appears insecure — rotate credentials", cp.Name))
		}
	}

	return result
}

// isInsecureDefault checks if a value matches known placeholder/default secrets.
// Covers .env.example placeholders, common weak passwords, and CI/CD defaults.
func isInsecureDefault(val string) bool {
	insecureDefaults := []string{
		"cloudai_secret",
		"change-this-in-production",
		"change-this-to-a-strong",
		"dev-jwt-secret",
		"dev-fallback-secret",
		"changeme",
		"password",
		"secret",
		"default",
		"example",
		"test-key",
		"12345",
		"admin",
		"your-secret-here",
		"replace-me",
		"not-for-production",
	}
	lower := strings.ToLower(val)
	for _, d := range insecureDefaults {
		if strings.Contains(lower, d) {
			return true
		}
	}
	// Detect repeated characters (e.g., "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if len(val) >= 32 {
		allSame := true
		for i := 1; i < len(val); i++ {
			if val[i] != val[0] {
				allSame = false
				break
			}
		}
		if allSame {
			return true
		}
	}
	return false
}

// shannonEntropy calculates the Shannon entropy (bits per character) of a string.
// Low entropy indicates a weak/predictable key.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]float64)
	for _, c := range s {
		freq[c]++
	}
	length := float64(len([]rune(s)))
	entropy := 0.0
	for _, count := range freq {
		p := count / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}

// generateDevSecret generates a random hex secret for development use
func generateDevSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "dev-fallback-secret-not-for-production"
	}
	return hex.EncodeToString(b)
}

func setDefaults(v *viper.Viper) {
	// General
	v.SetDefault("env", "development")
	v.SetDefault("log_level", "info")

	// API Server
	v.SetDefault("host", "0.0.0.0")
	v.SetDefault("port", 8080)
	v.SetDefault("read_timeout", 30*time.Second)
	v.SetDefault("write_timeout", 30*time.Second)
	v.SetDefault("metrics_port", 9100)

	// Scheduler
	v.SetDefault("scheduler_port", 8081)
	v.SetDefault("scheduling_interval", 10)

	// Agent
	v.SetDefault("agent_port", 8082)
	v.SetDefault("apiserver_addr", "localhost:8080")

	// Database
	v.SetDefault("db_host", "localhost")
	v.SetDefault("db_port", 5432)
	v.SetDefault("db_name", "cloudai")
	v.SetDefault("db_user", "cloudai")
	v.SetDefault("db_password", "") // MUST be set via CLOUDAI_DB_PASSWORD env var
	v.SetDefault("db_sslmode", "disable")
	v.SetDefault("db_max_open_conns", 25)
	v.SetDefault("db_max_idle_conns", 10)

	// Redis
	v.SetDefault("redis_addr", "localhost:6379")
	v.SetDefault("redis_password", "")
	v.SetDefault("redis_db", 0)

	// Kafka
	v.SetDefault("kafka_brokers", "localhost:9092")
	v.SetDefault("kafka_group_id", "cloudai-fusion")

	// NATS
	v.SetDefault("nats_url", "nats://localhost:4222")
	v.SetDefault("nats_cluster", "cloudai-fusion")

	// Auth
	v.SetDefault("jwt_secret", "") // MUST be set via CLOUDAI_JWT_SECRET env var
	v.SetDefault("jwt_expiry", 24*time.Hour)

	// AI Engine
	v.SetDefault("ai_engine_addr", "localhost:8090")
	v.SetDefault("ai_model_path", "./models")
	v.SetDefault("ai_device", "cpu")

	// Feature Flags
	v.SetDefault("feature_profile", "standard")

	// Monitoring
	v.SetDefault("prometheus_endpoint", "http://localhost:9090")
	v.SetDefault("grafana_endpoint", "http://localhost:3000")
	v.SetDefault("jaeger_endpoint", "http://localhost:4317")
}
