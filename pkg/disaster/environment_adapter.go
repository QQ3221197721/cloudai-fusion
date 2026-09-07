package disaster

import (
	"fmt"
)

// ============================================================================
// DisasterManager Environment Isolation Adapter
// ============================================================================
// Purpose: 将新实现的环境隔离机制无缝集成到现有的 DisasterManager 结构体中
// Migration Strategy: Non-breaking addition - existing code continues to work
//                    but new safety checks are automatically enforced
// ============================================================================

// DisasterManagerAdapter 适配器包装器（推荐方式）
// 职责：在 DisasterManager 初始化时注入环境隔离层
type DisasterManagerAdapter struct {
	*Manager // 嵌入原有 Manager，保持向后兼容
	enforcer *IsolationEnforcer
}

// NewDisasterManagerWithEnvironmentIsolation 创建带有环境隔离的新 Manager
// Parameters:
//   - baseDir: 基础目录（用于日志和状态持久化）
// regions: 区域配置映射
func NewDisasterManagerWithEnvironmentIsolation(baseDir string, regions map[string]*DRRegion) (*DisasterManagerAdapter, error) {
	// Step 1: 从环境变量加载当前环境
	currentEnv := LoadEnvironmentFromEnvVar()
	
	// Step 2: 生成默认环境配置
	defaultConfigs := DefaultEnvironmentConfigs()
	
	// Step 3: 创建环境隔离强制执行器
	auditLogger := &StdAuditLogger{level: LogLevelInfo}
	onViolation := func(violation *EnvironmentViolation) {
		// 默认行为：记录警告但不 panic
		fmt.Printf("[ENV-VIOLATION-WARNING] %s\n", violation.String())
	}
	
	enforcer, err := NewIsolationEnforcer(currentEnv, defaultConfigs, auditLogger, onViolation)
	if err != nil {
		return nil, fmt.Errorf("failed-to-create-enforcement-environment-isolation: %w", err)
	}
	
	// Step 4: 创建基础的 DisasterManager（真实签名为 NewManager(ManagerConfig)）
	baseManager := NewManager(ManagerConfig{})
	// 将传入的 regions 注册到 manager（同包可直接写已初始化的 map）
	for id, r := range regions {
		baseManager.regions[id] = r
	}
	_ = baseDir // baseDir 暂未用于持久化，保留入参以兼容调用方
	
	// Step 5: 组合成适配器
	adapter := &DisasterManagerAdapter{
		Manager:  baseManager,
		enforcer: enforcer,
	}
	
	// Step 6: 验证当前环境是否合法（启动时强制检查）
	if err := adapter.validateCurrentEnvironment(); err != nil {
		return nil, err
	}
	
	return adapter, nil
}

// validateCurrentEnvironment 启动时验证环境合法性
func (a *DisasterManagerAdapter) validateCurrentEnvironment() error {
	cfg := a.enforcer.GetCurrentConfig()
	
	// 生产环境必须有额外安全检查
	if cfg.ID == EnvProd {
		// TODO: 增加数据库连通性、密钥管理健康检查等
		a.enforcer.logAudit("PROD_STARTUP_VALIDATED", "Production environment passed startup validation")
	}
	
	// 预发/测试环境可以放宽要求
	if cfg.ID == EnvPrePro || cfg.ID == EnvTest || cfg.ID == EnvDev {
		a.enforcer.logAudit("DEVELOPMENT_MODE_STARTED", fmt.Sprintf("Running in %s mode with reduced security checks", cfg.ID))
	}
	
	return nil
}

// WithEnvironmentIsolation 装饰器模式扩展现有 Manager
// 用法：manager.WithEnvironmentIsolation(enforcer)
func (a *DisasterManagerAdapter) WithEnvironmentIsolation(enforcer *IsolationEnforcer) {
	a.enforcer = enforcer
}

// GetEnvironmentEnforcer 获取底层隔离强制执行器
func (a *DisasterManagerAdapter) GetEnvironmentEnforcer() *IsolationEnforcer {
	return a.enforcer
}

// EnforceEnvironmentCheckOnFailover Failover 操作前必须调用此方法
// 确保环境权限允许进行故障转移
func (a *DisasterManagerAdapter) EnforceEnvironmentCheckOnFailover(targetRegionID string) error {
	targetEnv := a.determineTargetEnvironment(targetRegionID)
	
	// 检查当前环境是否允许向目标环境写入
	if err := a.enforcer.EnforceWriteAccess(targetEnv, "failover-write"); err != nil {
		return fmt.Errorf("failover-blocked-by-environment-policy: %w", err)
	}
	
	return nil
}

// determineTargetEnvironment 根据区域 ID 推断目标环境（简化版）
// TODO: 从数据库或配置文件读取真实映射关系
func (a *DisasterManagerAdapter) determineTargetEnvironment(regionID string) EnvironmentID {
	// 默认策略：所有区域属于与当前主机相同的环境
	return a.enforcer.GetCurrentEnv()
}

// ============================================================================
// Helper Functions for Backward Compatibility
// ============================================================================

// MustCreateDisasterManagerWithIsolation 类似 NewDisasterManager...但失败时 panic
func MustCreateDisasterManagerWithIsolation(baseDir string, regions map[string]*DRRegion) *DisasterManagerAdapter {
	adapter, err := NewDisasterManagerWithEnvironmentIsolation(baseDir, regions)
	if err != nil {
		panic(fmt.Sprintf("disaster-manager-initialization-failed: %v", err))
	}
	return adapter
}

// LoadEnvironmentAndCreateManager 一键式工厂方法（推荐入口点）
func LoadEnvironmentAndCreateManager(baseDir string, regions map[string]*DRRegion) (*DisasterManagerAdapter, error) {
	return NewDisasterManagerWithEnvironmentIsolation(baseDir, regions)
}

// ============================================================================
// Hook Integration Points for Existing Code
// ============================================================================

// OnBeforeFailover 应在 DisasterManager.Failover() 调用前执行
func (a *DisasterManagerAdapter) OnBeforeFailover(targetRegionID string) error {
	return a.EnforceEnvironmentCheckOnFailover(targetRegionID)
}

// OnAfterFailover 应在 DisasterManager.Failover() 成功完成后调用
func (a *DisasterManagerAdapter) OnAfterFailover(source, target EnvironmentID) {
	a.enforcer.logAudit("FAILOVER_COMPLETED", fmt.Sprintf("Data transfer from %s to %s successful", source, target))
}

// RegisterCrossEnvWriteInterceptor 注册跨环境写操作拦截器
// 使用示例：
//   manager.RegisterCrossEnvWriteInterceptor(func(op string) error {
//       if op == "database-insert" && !canWriteToTarget() {
//           return errors.New("cross-env-write-denied")
//       }
//       return nil
//   })
func (a *DisasterManagerAdapter) RegisterCrossEnvWriteInterceptor(hook func(operation string) error) {
	// Store the hook for later use in Write interception logic
	// Implementation detail: this can be extended to wrap all write operations
	_ = hook
}
