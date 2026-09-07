package disaster

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// L16 Trust-On-Failover - Environment Isolation
// ============================================================================
// Purpose: 强制执行生产/预发/开发环境的数据和配置物理隔离
// Core Principle: "Honesty by Design" - 运行时验证环境边界，防止误操作导致数据污染
// Reference: docs/architecture.md section "Security Model -> Honesty over Illusion"
// ============================================================================

// EnvironmentID 标识部署环境的严格类型（不可隐式转换）
type EnvironmentID string

const (
	// EnvProd - 生产环境：只写保护，数据永不回滚到非 prod 环境
	EnvProd EnvironmentID = "prod"

	// EnvPrePro - 预发环境：只读复制，禁止写入生产数据
	EnvPrePro EnvironmentID = "prepro"

	// EnvDev - 开发环境：全权限沙箱，允许破坏性操作用于测试
	EnvDev EnvironmentID = "dev"

	// EnvTest - 测试环境：临时数据，启动时自动清理
	EnvTest EnvironmentID = "test"
)

// EnvironmentConfig 环境特定配置
type EnvironmentConfig struct {
	ID              EnvironmentID `json:"id"`
	ReadOnly        bool          `json:"read_only"`             // 是否只读模式
	AllowCrossEnv   bool          `json:"allow_cross_env_write"` // 是否允许跨环境写入（仅 dev/test 可开启）
	DataRetention   int           `json:"data_retention_days"`   // 数据保留天数
	SandboxMode     bool          `json:"sandbox_mode"`          // 沙箱模式（随机失败注入）
	MaxReplicationLag int          `json:"max_replication_lag_sec"` // 最大允许的复制延迟阈值
}

// ValidateEnvironmentConfig 验证环境配置合法性
func (c *EnvironmentConfig) ValidateEnvironmentConfig() error {
	if c.ID == "" {
		return errors.New("environment-id-required")
	}

	// 只读环境不允许跨环境写入
	if c.ReadOnly && c.AllowCrossEnv {
		return fmt.Errorf("invalid-config: readonly-environment-cannot-allow-cross-env-write")
	}

	// 生产环境数据保留期永久
	if c.ID == EnvProd && c.DataRetention != 0 {
		return fmt.Errorf("production-data-must-have-permanent-retention")
	}

	return nil
}

// IsAllowedToWriteTo 检查当前环境是否可以向目标环境写入数据
func (c *EnvironmentConfig) IsAllowedToWriteTo(targetEnv EnvironmentID) bool {
	// 生产环境永远不能向其他环境写入
	if c.ID == EnvProd {
		return false
	}

	// 预发环境只能向生产环境同步（单向流动）
	if c.ID == EnvPrePro {
		return targetEnv == EnvProd || targetEnv == EnvPrePro
	}

	// 开发和测试环境可以互写（需显式配置允许）
	if c.ID == EnvDev || c.ID == EnvTest {
		return c.AllowCrossEnv
	}

	return false
}

// ============================================================================
// IsolationEnforcer - 环境隔离强制执行器
// ============================================================================
// Responsibilities:
// 1. 启动时加载环境变量并校验合法性
// 2. 拦截所有跨环境写操作并验证权限
// 3. 提供实时审计日志记录
//
// Thread-Safety: All methods are goroutine-safe with internal mutex
// ============================================================================

type IsolationEnforcer struct {
	mu                sync.RWMutex
	currentEnv        EnvironmentID
	config            map[EnvironmentID]*EnvironmentConfig
	auditLogger       AuditLogger            // 可选的审计日志接口
	onViolation       ViolationHandler       // 违规回调（可插入阻断逻辑）
}

// ViolationHandler 环境违规处理回调
type ViolationHandler func(violation *EnvironmentViolation)

// EnvironmentViolation 环境违规行为记录
type EnvironmentViolation struct {
	Timestamp      uint64                 `json:"timestamp"`
	SourceEnv      EnvironmentID          `json:"source_env"`
	DestEnv        EnvironmentID          `json:"dest_env"`
	Operation      string                 `json:"operation"`
	BlockedBy      string                 `json:"blocked_by"`
	EvidenceChain  []byte                 `json:"evidence,omitempty"`
}

// NewIsolationEnforcer 创建环境隔离强制执行器
// Parameters:
//   - env: 从 CLOUDAI_ENV 读取的环境 ID
//   - configs: 各环境的配置映射（支持动态更新）
//   - auditLogger: 审计日志记录器（可为 nil）
//   - onViolation: 违规回调（推荐设置为 ReturnError 或 Panic）
func NewIsolationEnforcer(env EnvironmentID, configs map[EnvironmentID]*EnvironmentConfig, auditLogger AuditLogger, onViolation ViolationHandler) (*IsolationEnforcer, error) {
	enforcer := &IsolationEnforcer{
		currentEnv:    env,
		config:        make(map[EnvironmentID]*EnvironmentConfig),
		auditLogger:   auditLogger,
		onViolation:   onViolation,
	}

	// 加载默认配置
	for id, cfg := range configs {
		if err := cfg.ValidateEnvironmentConfig(); err != nil {
			return nil, fmt.Errorf("invalid-config-for-%s: %w", id, err)
		}
		enforcer.config[id] = cfg
	}

	// 自我注册
	if cfg, ok := configs[env]; ok {
		enforcer.config[env] = cfg
	} else {
		// 确保当前环境的配置存在
		enforcer.config[env] = &EnvironmentConfig{
			ID: env,
		}
	}

	// 启动时立即验证当前环境存在且合法
	if _, exists := enforcer.config[env]; !exists {
		return nil, fmt.Errorf("environment-not-found: %s. available: %v", env, listAvailableEnvironments(configs))
	}

	// 应用层调用 Hook：在第一次写操作前验证
	enforcer.validateAtStartup()

	return enforcer, nil
}

// MustNewIsolationEnforcer 类似 NewIsolationEnforcer，但在失败时 panic（适合初始化阶段）
func MustNewIsolationEnforcer(env EnvironmentID, configs map[EnvironmentID]*EnvironmentConfig, auditLogger AuditLogger, onViolation ViolationHandler) *IsolationEnforcer {
	enforcer, err := NewIsolationEnforcer(env, configs, auditLogger, onViolation)
	if err != nil {
		panic(fmt.Sprintf("failed-to-create-isolation-enforcer: %v", err))
	}
	return enforcer
}

// validateAtStartup 启动时验证环境合法性（防止错误部署）
func (e *IsolationEnforcer) validateAtStartup() {
	cfg := e.GetCurrentConfig()

	// 生产环境必须有额外安全检查
	if cfg.ID == EnvProd {
		e.logAudit("ENV_PROD_STARTUP", "Production environment started")
		
		// TODO: 增加额外的健康检查和配置验证
		// e.runAdditionalSafetyChecks()
	}
}

// GetCurrentEnv 获取当前环境 ID
func (e *IsolationEnforcer) GetCurrentEnv() EnvironmentID {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentEnv
}

// GetCurrentConfig 获取当前环境配置
func (e *IsolationEnforcer) GetCurrentConfig() *EnvironmentConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config[e.currentEnv]
}

// SetCurrentEnv 切换当前环境（仅限内部使用，如 failover 流程）
func (e *IsolationEnforcer) SetCurrentEnv(env EnvironmentID) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	_, exists := e.config[env]
	if !exists {
		return fmt.Errorf("environment-not-exists: %s", env)
	}

	prevEnv := e.currentEnv
	e.currentEnv = env
	e.logAudit("ENV_SWITCH", fmt.Sprintf("Switched from %s to %s", prevEnv, env))

	return nil
}

// CanWriteTo 检查当前环境是否允许向目标环境写入
func (e *IsolationEnforcer) CanWriteTo(targetEnv EnvironmentID, operation string) bool {
	e.mu.RLock()
	cfg := e.config[e.currentEnv]
	e.mu.RUnlock()

	return cfg.IsAllowedToWriteTo(targetEnv)
}

// EnforceWriteAccess 强制执行写访问控制（包装器，带审计和违规处理）
func (e *IsolationEnforcer) EnforceWriteAccess(targetEnv EnvironmentID, operation string) error {
	e.mu.RLock()
	cfg := e.config[e.currentEnv]
	e.mu.RUnlock()

	if !cfg.IsAllowedToWriteTo(targetEnv) {
		violation := &EnvironmentViolation{
			Timestamp: uint64(e.nowUnix()),
			SourceEnv: e.currentEnv,
			DestEnv:   targetEnv,
			Operation: operation,
			BlockedBy: "environment-isolation-policy",
		}

		// 记录审计日志
		e.logAudit("VIOLATION_BLOCKED", violation.String())

		// 执行违规回调（如果已注册）
		if e.onViolation != nil {
			e.onViolation(violation)
		}

		return fmt.Errorf("cross-env-write-blocked: cannot write from %s to %s via %s", 
			e.currentEnv, targetEnv, operation)
	}

	return nil
}

// listAvailableEnvironments 返回可用的环境列表（用于错误提示）
func listAvailableEnvironments(configs map[EnvironmentID]*EnvironmentConfig) []string {
	result := make([]string, 0, len(configs))
	for id := range configs {
		result = append(result, string(id))
	}
	return result
}

// logAudit 记录审计日志（线程安全，无锁 fallback）
func (e *IsolationEnforcer) logAudit(category string, message string) {
	if e.auditLogger != nil {
		// 直接读 currentEnv 字段（而非 GetCurrentEnv()）：部分调用方（如 SetCurrentEnv）
		// 已持有写锁，再取 RLock 会自锁；且 Log 第三参需要 EnvironmentID 而非 string。
		e.auditLogger.Log(category, message, e.currentEnv)
	} else {
		// Fallback to standard logging
		// Note: This should be replaced with actual structured logging in production
		fmt.Printf("[AUDIT][%s][%s] %s\n", category, e.currentEnv, message)
	}
}

// nowUnix 获取当前时间戳（便于测试 mock）
func (e *IsolationEnforcer) nowUnix() int64 {
	return time.Now().Unix()
}

// String implements Stringer interface for EnvironmentID
func (e EnvironmentID) String() string {
	return string(e)
}

// Helper functions for validation
func (v *EnvironmentViolation) String() string {
	return fmt.Sprintf("EnvViolation{%s->%s op=%s blocked=%s}", 
		v.SourceEnv, v.DestEnv, v.Operation, v.BlockedBy)
}
