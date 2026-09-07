package disaster

import (
	"context"
	"fmt"
	"time"
)

// ============================================================================
// Split-Brain Detection & Containment Bundle
// ============================================================================
// Purpose: 一键式启动完整的 split-brain 检测和缓解系统
// Usage: bundle.Start(context.Background()) // Starts both detector and controller
// Lifecycle: Graceful shutdown with context cancellation
// ============================================================================

// SplitBrainBundle 分裂脑检测与缓解完整系统
type SplitBrainBundle struct {
	detector    *SplitBrainDetector
	controller  *SplitBrainContoller
	ctx         context.Context
	cancel      context.CancelFunc
	nodes       map[string]*DRRegion
}

// NewSplitBrainBundle 创建分裂脑检测与缓解_bundle_
func NewSplitBrainBundle(failoverManager *DisasterManagerAdapter, nodes map[string]*DRRegion) (*SplitBrainBundle, error) {
	if failoverManager == nil {
		return nil, fmt.Errorf("failover-manager-cannot-be-nil")
	}
	
	// Create evidence logger (use existing infrastructure or create new one)
	evidenceLogger := NewEvidenceLogger()
	
	// Step 1: Create detector
	detector := NewSplitBrainDetector(nodes, evidenceLogger, nil) // callback set by controller
	
	// Step 2: Create controller and register as detector callback
	controller := NewSplitBrainContoller(detector, failoverManager)
	detector.onDetection = controller.OnDetection
	
	// Step 3: Combine into bundle
	bundle := &SplitBrainBundle{
		detector:   detector,
		controller: controller,
		nodes:      nodes,
	}
	
	return bundle, nil
}

// Start 启动检测和缓解系统（非阻塞 goroutine）
func (b *SplitBrainBundle) Start(ctx context.Context) {
	b.ctx, b.cancel = context.WithCancel(ctx)
	
	// Launch detection loop
	go b.detector.Start(b.ctx)
	
	fmt.Printf("[SPLIT-BRAIN-BUNDLE] Started with %d monitored nodes\n", len(b.nodes))
}

// Stop 优雅停止检测和缓解系统
func (b *SplitBrainBundle) Stop() {
	if b.cancel != nil {
		b.cancel()
		fmt.Println("[SPLIT-BRAIN-BUNDLE] Stopped gracefully")
	}
}

// WaitForNodeRegistration 等待节点状态更新并刷新检测器
func (b *SplitBrainBundle) WaitForNodeRegistration(nodeID string, timeout time.Duration) error {
	done := make(chan bool)
	
	go func() {
		for {
			select {
			case <-b.ctx.Done():
				return
			default:
				// Check if node is registered in detector
				b.detector.mu.RLock()
				_, exists := b.detector.nodes[nodeID]
				b.detector.mu.RUnlock()
				
				if exists {
					done <- true
					return
				}
				
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()
	
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("node-registration-timeout: %s", nodeID)
	case <-b.ctx.Done():
		return fmt.Errorf("bundle-stopped-before-node-registered")
	}
}

// GetDetectorStatus 获取当前检测器状态
func (b *SplitBrainBundle) GetDetectorStatus() map[string]*NodeStatus {
	if b.detector == nil {
		return nil
	}
	
	b.detector.mu.RLock()
	defer b.detector.mu.RUnlock()
	
	result := make(map[string]*NodeStatus)
	for id, status := range b.detector.nodes {
		result[id] = status
	}
	
	return result
}

// GetCurrentContainmentState 获取当前缓解状态报告
func (b *SplitBrainBundle) GetCurrentContainmentState() string {
	if b.controller == nil {
		return "Controller not initialized"
	}
	
	return b.controller.GenerateContainmentReport()
}

// ForceRefreshDetection 强制立即执行一次检测（不等待 ticker interval）
func (b *SplitBrainBundle) ForceRefreshDetection() error {
	if b.detector == nil {
		return fmt.Errorf("detector-not-initialized")
	}
	
	return b.detector.detectAndMitigate()
}

// ============================================================================
// Convenience Factory Functions
// ============================================================================

// MustCreateSplitBrainBundle 类似 NewSplitBrainBundle 但失败时 panic
func MustCreateSplitBrainBundle(failoverManager *DisasterManagerAdapter, nodes map[string]*DRRegion) *SplitBrainBundle {
	bundle, err := NewSplitBrainBundle(failoverManager, nodes)
	if err != nil {
		panic(fmt.Sprintf("split-brain-bundle-initialization-failed: %v", err))
	}
	return bundle
}

// LoadEnvironmentAndCreateSplitBrainBundle 一键式工厂方法
func LoadEnvironmentAndCreateSplitBrainBundle(baseDir string, regions map[string]*DRRegion) (*SplitBrainBundle, error) {
	// First create the disaster manager with environment isolation
	manager, err := LoadEnvironmentAndCreateManager(baseDir, regions)
	if err != nil {
		return nil, fmt.Errorf("failed-to-create-disaster-manager: %w", err)
	}
	
	// Then create the split-brain bundle
	return NewSplitBrainBundle(manager, regions)
}
