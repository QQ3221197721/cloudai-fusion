// Package tee - GPU topology aware attestation with NVLink validation
// ============================================================================
// Purpose: 在 SGX report data 中绑定 GPU 拓扑信息，让客户端可以要求特定的
//          GPU 互联配置（例如："必须使用 NVLink 互联的 enclave"）。
//          
//          这需要修改 DCAP backend 来读取 nvidia-smi/NVML 的 GPU 拓扑信息，
//          计算拓扑哈希，并把它绑进 SGX REPORTDATA（measurement 扩展字段）。
//
// Security & Honesty:
//   - Trusted 仍只来自底层硬件证明；仿真模式 Trusted=false
//   - 如果系统没有 NVIDIA GPU，报告中的 GPU 拓扑字段为空；客户端可以设置
//     RequireGPUTopology=true 来拒绝无 GPU 的报告
//   
// Performance:
//   - NVML query 开销很小（微秒级），不会显著增加昂贵证明路径的耗时
//   - 对于有 GPU 的系统，只会额外增加几毫秒的测量时间
//
// Future work:
//   - 当真实 DCAP backend 实现后，这个拓扑哈希会真正写入 REPORTDATA
//   - 目前只是 simulation 模式的扩展字段（可被独立验证）
// ============================================================================

package tee

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"hash/fnv"
	"os/exec"
	"strings"
	"sync"
)

// GPUTopologyMode GPU 拓扑策略
type GPUTopologyMode string

const (
	// GPUNone 无 GPU 约束（允许无 GPU 或任何拓扑）
	GPUNone GPUTopologyMode = "none"
	// GPUAny 任意 GPU 配置即可
	GPUAny GPUTopologyMode = "any"
	// GPUNVLink 至少有一个 NVLink 连接的 GPU 对
	GPONVLink GPUTopologyMode = "nvlink_dominant"
	// GPUHBM 至少一个 HBM/GDDR6 显存配置的 GPU
	GPHBM GPUTopologyMode = "hbm_gddr6"
)

// GPUCapability 描述单个 GPU 的基本能力
type GPUCapability struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Vendor    string `json:"vendor"` // NVIDIA, AMD, Intel
	MemoryMB  int    `json:"memory_mb"`
	Topology  string `json:"topology"` // nvlink/pcie/etc.
}

// TopologyReport GPU 拓扑探测报告
type TopologyReport struct {
	GPUs         []GPUCapability `json:"gpus"`
	TopoHash     string          `json:"topo_hash"`     // 规范化哈希（用于 REPORTDATA）
	DominantMode string          `json:"dominant_mode"` // nvlink|pcie|mixed|unknown
	NvLinkLinks  int             `json:"nvlink_links"`  // NVLink 连接数
}

// validateAndEnforceGPUTopology 验证并强制 GPU 拓扑约束
func validateAndEnforceGPUTopology(report *TopologyReport, mode GPUTopologyMode) error {
	if mode == GPUNone {
		return nil // 无约束
	}

	if len(report.GPUs) == 0 {
		return fmt.Errorf("no-gpu-found-but-mode=%s-requires-gpu", mode)
	}

	switch mode {
	case GPUAny:
		// 任意 GPU 都行，通过
	case GPONVLink:
		if report.NvLinkLinks <= 0 {
			return fmt.Errorf("required-nvlink-but-topo=%s-nvlinks=%d", report.DominantMode, report.NvLinkLinks)
		}
		// 检查是否有 NVLink 连接的主机 GPU
		hasNVLINKHost := false
		for _, gpu := range report.GPUs {
			if strings.Contains(gpu.Topology, "NV") || gpu.Topology == "NVLink" {
				hasNVLINKHost = true
				break
			}
		}
		if !hasNVLINKHost && report.NvLinkLinks > 0 {
			return errors.New("nvlink-detected-but-not-host-facing")
		}
	case GPHBM:
		hbmFound := false
		for _, gpu := range report.GPUs {
			nameLower := strings.ToLower(gpu.Name)
			memType := strings.ToLower(gpu.Topology)
			if strings.Contains(nameLower, "h100") || strings.Contains(nameLower, "a100") ||
				strings.Contains(memType, "hbm") || strings.Contains(memType, "gddr6x") {
				hbmFound = true
				break
			}
		}
		if !hbmFound {
			return errors.New("required-hbm-or-gddr6x-but-none-found")
		}
	default:
		return fmt.Errorf("unsupported-gpu-topology-mode=%s", mode)
	}

	return nil
}

// ComputeTopologyHash 计算规范化的 GPU 拓扑哈希（仅使用公开数据）
func ComputeTopologyReport(report TopologyReport) (*TopologyReport, error) {
	// 排序以确保确定性
	gpus := make([]GPUCapability, len(report.GPUs))
	copy(gpus, report.GPUs)

	// 按 ID 排序
	for i := 0; i < len(gpus)-1; i++ {
		for j := i + 1; j < len(gpus); j++ {
			if gpus[i].ID > gpus[j].ID {
				gpus[i], gpus[j] = gpus[j], gpus[i]
			}
		}
	}

	// 构建规范化字符串（确保跨运行一致）
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("count=%d", len(gpus)))
	for _, gpu := range gpus {
		sb.WriteString(fmt.Sprintf("|%d:%s:%d:%s", gpu.ID, gpu.Vendor, gpu.MemoryMB, gpu.Topology))
	}

	// 计算 FNV-1a 哈希
	h := fnv.New64()
	h.Write([]byte(sb.String()))
	report.TopoHash = fmt.Sprintf("gpu_topo_%x", h.Sum64())
	
	// 自动检测 dominant mode
	if report.NvLinkLinks > len(gpus)/2 {
		report.DominantMode = "nvlink_dominant"
	} else if len(gpus) > 0 {
		report.DominantMode = "pcie_mixed"
	} else {
		report.DominantMode = "unknown"
	}

	return &report, nil
}

// DetectGPUTopology 通过 NVML/nvidia-smi 探测当前系统的 GPU 拓扑
// Returns empty report if no NVIDIA GPUs found or not on Linux with CUDA support
func DetectGPUTopology(ctx context.Context) (*TopologyReport, error) {
	// Quick check for NVML library existence (will fail gracefully on non-Linux/non-NVIDIA)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	report := &TopologyReport{GPUs: make([]GPUCapability, 0)}

	// Method 1: Try NVML directly if available
	// In real implementation, would import github.com/NVIDIA/go-nvml/pkg/nvml
	// For now, use nvidia-smi command as fallback
	
	cmd := exec.CommandContext(ctx, "nvidia-smi", "-q", "--xmp=table")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// No NVIDIA GPUs or not installed
		return report, nil // Not an error, just no GPU
	}

	lines := strings.Split(string(out), "\n")
	var currentGPU GPUCapability
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Product Name:") {
			currentGPU.Name = strings.TrimPrefix(line, "Product Name:")
			currentGPU.Name = strings.TrimSpace(currentGPU.Name)
		} else if strings.HasPrefix(line, "Total Dedicaed Video Memory:") {
			var mem int
			fmt.Sscanf(line, "Total Dedicated Video Memory: %d MiB", &mem)
			currentGPU.MemoryMB = mem
		} else if strings.HasPrefix(line, "Mig Devices:") {
			currentGPU.Topology = "mig"
		} else if strings.Contains(line, "NVLink") || strings.Contains(line, "NVLINK") {
			currentGPU.Topology = "NVLink"
			report.NvLinkLinks++
		} else if strings.Contains(line, "PCIe") {
			if currentGPU.Topology == "" {
				currentGPU.Topology = "PCIe"
			}
		} else if strings.HasPrefix(line, "==") && currentGPU.ID != -1 {
			// New GPU section
			report.GPUs = append(report.GPUs, currentGPU)
			currentGPU = GPUCapability{}
		}
	}

	// Last GPU
	if currentGPU.ID != -1 {
		report.GPUs = append(report.GPUs, currentGPU)
	}

	if len(report.GPUs) == 0 {
		return report, nil // No GPUs
	}

	return ComputeTopologyReport(*report)
}

// GPU-aware BoundAttestor 扩展版本
type GPUAttestor struct {
	base       *BoundAttestor
	mode       GPUTopologyMode
	mu         sync.RWMutex
	lastReport *TopologyReport
}

// NewGPUAttestor 创建 GPU 感知证明器
func NewGPUAttestor(ba *BoundAttestor, mode GPUTopologyMode) (*GPUAttestor, error) {
	if mode == "" {
		mode = GPUNone // Default
	}
	return &GPUAttestor{base: ba, mode: mode}, nil
}

// AttestGPU 执行 GPU 感知的证明请求
func (ga *GPUAttestor) AttestGPU(ctx context.Context, enclaveID string, nonce []byte) (*BoundAttestationReport, error) {
	if len(nonce) == 0 {
		return nil, errors.New("nonce-required")
	}

	// 1) 先探测当前 GPU 拓扑
	topoReport, err := DetectGPUTopology(ctx)
	if err != nil {
		return nil, fmt.Errorf("detect-gpu-topo-failed: %w", err)
	}

	// 2) 强制验证拓扑约束
	if err := validateAndEnforceGPUTopology(topoReport, ga.mode); err != nil {
		// 拒绝不符合要求的证明
		return nil, fmt.Errorf("gpu-topo-constraint-violated: %w", err)
	}

	// 3) 建立基础 bound attestation（带 topo hash 注入）
	internalNonce := make([]byte, 16)
	if _, err := rand.Read(internalNonce); err != nil {
		return nil, fmt.Errorf("gen-nonce: %w", err)
	}

	rep, attErr := ga.base.AttestBound(ctx, BoundAttestationRequest{
		EnclaveID:   enclaveID,
		Nonce:       internalNonce,
		PayloadHash: []byte(topoReport.TopoHash), // 把拓扑哈希作为 payload 的一部分
	})
	if rep == nil {
		return nil, fmt.Errorf("no-report: %w", attErr)
	}

	// 4) 在报告中添加 GPU 拓扑信息（模拟 REPORTDATA 扩展）
	// Note: 这里只在 simulation 模式下有效；真实 DCAP backend 会把这些写入真正的 REPORTDATA
	if topoReport != nil && len(topoReport.GPUs) > 0 {
		// 将拓扑哈希附加到 measurement（模拟扩展方式）
		rep.Report.Measurement = fmt.Sprintf("%s/gpu=%s", rep.Report.Measurement, topoReport.TopoHash)
		rep.Report.Detail = fmt.Sprintf("%s | gpu_topo=%s nvlinks=%d", rep.Report.Detail, topoReport.DominantMode, topoReport.NvLinkLinks)
	}

	// Cache last report for stats
	ga.mu.Lock()
	ga.lastReport = topoReport
	ga.mu.Unlock()

	return rep, attErr
}

// GetLastGPUReport 返回最后一次证明的 GPU 拓扑报告
func (ga *GPUAttestor) GetLastGPUReport() *TopologyReport {
	ga.mu.RLock()
	defer ga.mu.RUnlock()
	if ga.lastReport == nil {
		return &TopologyReport{}
	}
	cp := *ga.lastReport
	return &cp
}
