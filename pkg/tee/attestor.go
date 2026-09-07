package tee

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ============================================================================
// Attestor — 面向用户的统一证明入口（开箱即用）
// ============================================================================
// 设计目标：用户在【任何机器】上调用 NewAttestor().Attest() 都能得到一个
//          正确、诚实、不崩的结果。绝不会在没有真实硬件时返回 Trusted=true。
//
// 三种运行模式（由运行时能力探测自动决定）：
//   1. hardware    : 探测到 SGX 且编译了 DCAP 后端 → 真实硬件证明，Trusted=true
//   2. simulation  : 无 SGX 但用户显式开启仿真(WithSimulation) → 明确标注、Trusted=false
//   3. unavailable : 无 SGX 且未开启仿真 → 返回明确错误，Trusted=false
//
// 关键安全不变量：Trusted=true 当且仅当【真实硬件后端】完成了验证。
// ============================================================================

// AttestationMode 证明运行模式
type AttestationMode string

const (
	ModeHardware    AttestationMode = "hardware"    // 真实 SGX 硬件证明
	ModeSimulation  AttestationMode = "simulation"  // 仿真（开发用，不可信）
	ModeUnavailable AttestationMode = "unavailable" // 本机不可用
)

// ErrSGXUnavailable 表示本机无可用 SGX 且未开启仿真模式
var ErrSGXUnavailable = errors.New("sgx-unavailable-on-this-host")

// AttestationReport 用户拿到的证明报告（JSON 友好，可直接作为 API 返回）
type AttestationReport struct {
	Mode        AttestationMode `json:"mode"`               // hardware / simulation / unavailable
	Trusted     bool            `json:"trusted"`            // 仅真实硬件验证通过才为 true
	EnclaveID   string          `json:"enclave_id"`         // 目标 enclave 标识
	Measurement string          `json:"measurement,omitempty"` // MRENCLAVE（仿真/不可用时为空）
	Capability  SGXCapability   `json:"capability"`         // 本机能力探测结果
	Detail      string          `json:"detail"`             // 人类可读说明
	VerifiedAt  time.Time       `json:"verified_at"`        // 生成时间(UTC)
}

// HardwareAttestor 真实硬件证明后端接口。
// 默认二进制不包含实现；在带 SGX 的 Linux 上以 `-tags sgx` 编译时注入真实 DCAP 实现。
type HardwareAttestor interface {
	// GenerateAndVerifyQuote 在真实 SGX 中生成并本地验证 quote，返回度量值(MRENCLAVE)。
	GenerateAndVerifyQuote(ctx context.Context, enclaveID string) (measurement string, err error)
}

// hardwareBackend 由 `//go:build sgx` 的文件在 init() 中注册；默认 nil。
var hardwareBackend HardwareAttestor

// RegisterHardwareBackend 注册真实硬件后端（仅 SGX 构建调用）。
func RegisterHardwareBackend(b HardwareAttestor) {
	hardwareBackend = b
}

// Attestor 证明器
type Attestor struct {
	cap      SGXCapability
	allowSim bool
	backend  HardwareAttestor
}

// AttestorOption 配置项
type AttestorOption func(*Attestor)

// WithSimulation 显式开启仿真模式（无 SGX 时用于跑通开发/演示流程）。
// 注意：仿真结果 Trusted 永远为 false。
func WithSimulation() AttestorOption {
	return func(a *Attestor) { a.allowSim = true }
}

// NewAttestor 创建证明器：自动探测本机 SGX 能力。任何平台调用都安全。
func NewAttestor(opts ...AttestorOption) *Attestor {
	a := &Attestor{
		cap:     DetectSGXCapability(),
		backend: hardwareBackend,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Capability 返回本机能力探测结果（供 UI/诊断展示）。
func (a *Attestor) Capability() SGXCapability {
	return a.cap
}

// Attest 执行证明，永远返回一个报告；不可用且未开仿真时同时返回 ErrSGXUnavailable。
//
// 用户典型用法：
//
//	rep, err := tee.NewAttestor().Attest(ctx, "my-enclave")
//	if rep.Trusted { /* 硬件级可信 */ } else { /* 按 rep.Mode 处理 */ }
func (a *Attestor) Attest(ctx context.Context, enclaveID string) (*AttestationReport, error) {
	now := time.Now().UTC()
	base := AttestationReport{
		EnclaveID:  enclaveID,
		Capability: a.cap,
		VerifiedAt: now,
	}

	// 1) 有 SGX 硬件
	if a.cap.Available {
		if a.backend != nil {
			measurement, err := a.backend.GenerateAndVerifyQuote(ctx, enclaveID)
			if err != nil {
				base.Mode = ModeUnavailable
				base.Trusted = false
				base.Detail = fmt.Sprintf("hardware attestation failed: %v", err)
				return &base, fmt.Errorf("hardware-attestation-failed: %w", err)
			}
			base.Mode = ModeHardware
			base.Trusted = true
			base.Measurement = measurement
			base.Detail = "verified via real SGX DCAP attestation"
			return &base, nil
		}
		// 有硬件但当前二进制未编译 DCAP 后端
		base.Mode = ModeUnavailable
		base.Trusted = false
		base.Detail = "SGX hardware detected but this binary was built without the DCAP backend; rebuild with `-tags sgx` on the SGX host"
		return &base, fmt.Errorf("dcap-backend-not-compiled: %w", ErrSGXUnavailable)
	}

	// 2) 无 SGX，但显式开启仿真
	if a.allowSim {
		base.Mode = ModeSimulation
		base.Trusted = false // 仿真绝不可信
		base.Detail = "SIMULATION mode: flow exercised without real hardware; result is NOT trustworthy"
		return &base, nil
	}

	// 3) 无 SGX 且未开仿真
	base.Mode = ModeUnavailable
	base.Trusted = false
	base.Detail = a.cap.Detail + " (enable simulation with WithSimulation() for dev/demo)"
	return &base, ErrSGXUnavailable
}
