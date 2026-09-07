package disaster

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// ============================================================================
// Audit Logger & Helper Infrastructure
// ============================================================================
// Purpose: 为环境隔离系统提供审计日志和辅助工具函数
// Design Pattern: Interface-based logging for testability
// ============================================================================

// AuditLogger 审计日志接口（抽象层，便于测试 mock）
type AuditLogger interface {
	Log(category string, message string, env EnvironmentID)
}

// StdAuditLogger 标准审计日志实现（输出到 stdout）
type StdAuditLogger struct {
	prefix string
	level  LogLevel // 可选的日志级别控制
}

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

func (l *StdAuditLogger) Log(category string, message string, env EnvironmentID) {
	timestamp := logDate()
	fmt.Printf("[%s][%s][%s] %s: %s\n", timestamp, env, category, l.prefix, message)
}

func (l *StdAuditLogger) Info(category string, message string, env EnvironmentID) {
	if l.level <= LogLevelInfo {
		l.Log(category, message, env)
	}
}

func (l *StdAuditLogger) Warn(category string, message string, env EnvironmentID) {
	if l.level <= LogLevelWarn {
		l.Log("WARN_"+category, message, env)
	}
}

func (l *StdAuditLogger) Error(category string, message string, env EnvironmentID) {
	if l.level <= LogLevelError {
		l.Log("ERROR_"+category, message, env)
	}
}

// NullAuditLogger 空实现（禁用审计日志）
type NullAuditLogger struct{}

func (n *NullAuditLogger) Log(category string, message string, env EnvironmentID) {}

// FileAuditLogger 文件审计日志（用于生产环境持久化）
type FileAuditLogger struct {
	filepath string
	logFile  *log.Logger
}

// sanitizePath 安全地清理和验证文件路径（防止路径遍历攻击）
func sanitizePath(baseDir, requestedPath string) (string, error) {
	// 清理路径
	cleaned := filepath.Clean(requestedPath)
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid-path: %w", err)
	}
	
	// 确保最终路径在 baseDir 内
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid-base-dir: %w", err)
	}
	
	if absPath != baseAbs && !filepath.HasPrefix(absPath, baseAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path-traversal-blocked: requested path outside allowed directory")
	}
	
	return absPath, nil
}

func NewFileAuditLogger(auditDir string, filename string) (*FileAuditLogger, error) {
	// 安全验证路径
	safePath, err := sanitizePath(auditDir, filename)
	if err != nil {
		return nil, err
	}
	
	file, err := os.OpenFile(safePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed-to-open-audit-log-file: %w", err)
	}

	return &FileAuditLogger{
		filepath: safePath,
		logFile:  log.New(file, "", log.LstdFlags),
	}, nil
}

func (f *FileAuditLogger) Log(category string, message string, env EnvironmentID) {
	f.logFile.Printf("[%s][%s] %s: %s\n", env, category, message, time.Now().Format(time.RFC3339))
}

// ============================================================================
// Configuration Generator
// ============================================================================
// Purpose: 便捷地生成默认环境配置模板
// ============================================================================

// DefaultEnvironmentConfigs 生成所有环境的默认配置
func DefaultEnvironmentConfigs() map[EnvironmentID]*EnvironmentConfig {
	return map[EnvironmentID]*EnvironmentConfig{
		EnvProd: {
			ID:                EnvProd,
			ReadOnly:          false,       // 生产环境需要写入能力
			AllowCrossEnv:     false,       // 禁止跨环境写入
			DataRetention:     0,           // 永久保留
			SandboxMode:       false,       // 关闭沙箱模式
			MaxReplicationLag: 5,          // RPO < 5 seconds
		},
		EnvPrePro: {
			ID:                EnvPrePro,
			ReadOnly:          true,        // 预发环境只读保护
			AllowCrossEnv:     false,
			SandboxMode:       false,
			MaxReplicationLag: 60,         // RPO < 60 seconds
		},
		EnvDev: {
			ID:                EnvDev,
			ReadOnly:          false,
			AllowCrossEnv:     true,        // 开发环境允许互写
			DataRetention:     7,           // 7 天临时数据
			SandboxMode:       true,        // 启用沙箱模式（随机故障注入）
			MaxReplicationLag: 300,        // RPO < 5 minutes
		},
		EnvTest: {
			ID:                EnvTest,
			ReadOnly:          false,
			AllowCrossEnv:     true,
			DataRetention:     1,           // 1 天临时数据
			SandboxMode:       true,
			MaxReplicationLag: 300,
		},
	}
}

// LoadEnvironmentFromEnvVar 从环境变量 CLOUDAI_ENV 加载当前环境 ID
// Fallback: 如果环境变量未设置，默认为 development
func LoadEnvironmentFromEnvVar() EnvironmentID {
	envStr := os.Getenv("CLOUDAI_ENV")
	if envStr == "" {
		// 安全默认值：本地开发环境
		return EnvDev
	}
	
	// 转换并校验
	id := EnvironmentID(envStr)
	if _, exists := DefaultEnvironmentConfigs()[id]; !exists {
		// 无效环境 ID，使用默认值并警告
		log.Printf("[WARNING] invalid CLOUDAI_ENV=%s, using default %s", id, EnvDev)
		return EnvDev
	}
	
	return id
}

// ValidateAndEnforceAtStartup 完整的启动时验证逻辑（推荐入口点）
// Returns: (*IsolationEnforcer, error) - 成功则返回可用实例，失败则返回错误
func ValidateAndEnforceAtStartup(logger AuditLogger, onViolation ViolationHandler) (*IsolationEnforcer, error) {
	// Step 1: 从环境变量读取当前环境
	currentEnv := LoadEnvironmentFromEnvVar()
	
	// Step 2: 生成所有环境的默认配置
	defaultConfigs := DefaultEnvironmentConfigs()
	
	// Step 3: （可选）覆盖部分环境特定配置（来自配置文件或 CLI 参数）
	// e.g., overrideConfigs := readFromConfig("env-config.yaml")
	// for id, cfg := range overrideConfigs {
	//     defaultConfigs[id] = cfg
	// }
	
	// Step 4: 创建隔离强制执行器
	enforcer, err := NewIsolationEnforcer(currentEnv, defaultConfigs, logger, onViolation)
	if err != nil {
		return nil, fmt.Errorf("isolation-enforcer-initialization-failed: %w", err)
	}
	
	// Step 5: 立即返回，调用方负责继续初始化其他组件
	
	return enforcer, nil
}

// Helper utilities

func logDate() string {
	return time.Now().Format("2006-01-02T15:04:05Z07:00")
}

// MustLoadEnvironment 类似 ValidateAndEnforceAtStartup，但失败时 panic
func MustLoadEnvironment(logger AuditLogger, onViolation ViolationHandler) *IsolationEnforcer {
	enforcer, err := ValidateAndEnforceAtStartup(logger, onViolation)
	if err != nil {
		panic(fmt.Sprintf("environment-isolation-fatal-error: %v", err))
	}
	return enforcer
}

// IsProductionLike 检查当前环境是否属于生产类（prod/prepro）
func IsProductionLike(enforcer *IsolationEnforcer) bool {
	if enforcer == nil {
		return false
	}
	
	env := enforcer.GetCurrentEnv()
	return env == EnvProd || env == EnvPrePro
}

// HasHighSafetyGuarantees 检查环境是否具备高安全保证（production only）
func HasHighSafetyGuarantees(enforcer *IsolationEnforcer) bool {
	if enforcer == nil {
		return false
	}
	
	cfg := enforcer.GetCurrentConfig()
	return cfg.ID == EnvProd && !cfg.SandboxMode
}
