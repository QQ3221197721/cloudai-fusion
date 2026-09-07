package tee

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// SGX 能力探测测试（用注入的伪造设备根，在无 SGX 的机器上也能验证 Linux 分支）
// ============================================================================

// setupFakeDevRoot 在临时目录里创建伪造的设备节点，并把 sgxDevRoot 指过去
func setupFakeDevRoot(t *testing.T, devices ...string) {
	t.Helper()
	root := t.TempDir()
	for _, d := range devices {
		full := filepath.Join(root, filepath.FromSlash(d))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0644); err != nil {
			t.Fatalf("write fake dev: %v", err)
		}
	}
	old := sgxDevRoot
	sgxDevRoot = root
	t.Cleanup(func() { sgxDevRoot = old })
}

func TestScanSGXDevices_DCAPKernel(t *testing.T) {
	setupFakeDevRoot(t, "/dev/sgx_enclave", "/dev/sgx_provision")
	c := scanSGXDevices()
	if !c.Available {
		t.Fatal("应探测为可用")
	}
	if c.Driver != DriverDCAPKernel {
		t.Errorf("期望 dcap_kernel，得到 %s", c.Driver)
	}
	if !c.DCAPReady || !c.ProvisionNode {
		t.Errorf("期望 DCAP 就绪且有 provision 节点，得到 %+v", c)
	}
}

func TestScanSGXDevices_LegacyEPID(t *testing.T) {
	setupFakeDevRoot(t, "/dev/isgx")
	c := scanSGXDevices()
	if !c.Available {
		t.Fatal("应探测为可用")
	}
	if c.Driver != DriverLegacyEPID {
		t.Errorf("期望 legacy_epid，得到 %s", c.Driver)
	}
	if c.DCAPReady {
		t.Error("遗留 EPID 不应标记 DCAP 就绪")
	}
}

func TestScanSGXDevices_None(t *testing.T) {
	setupFakeDevRoot(t) // 空目录，无任何设备节点
	c := scanSGXDevices()
	if c.Available {
		t.Error("无设备节点时应为不可用")
	}
	if c.Driver != DriverNone {
		t.Errorf("期望 none，得到 %s", c.Driver)
	}
}

func TestDetectSGXCapability_ThisHost(t *testing.T) {
	// 环境感知：只断言与真实运行环境一致的结论，不臆造
	c := DetectSGXCapability()
	if c.OS == "" {
		t.Error("OS 字段不应为空")
	}
	if c.OS != "linux" && c.Available {
		t.Errorf("非 Linux 平台不应报告 SGX 可用，得到 %+v", c)
	}
	// String() 不应 panic 且非空
	if c.String() == "" {
		t.Error("String() 不应为空")
	}
}

// ============================================================================
// Attestor 三种诚实模式测试
// ============================================================================

func TestAttestor_UnavailableByDefault(t *testing.T) {
	// 在无 SGX 环境（本测试机）默认应为 unavailable 且返回错误，Trusted=false
	setupFakeDevRoot(t) // 确保探测不到设备（即便在 Linux CI 上也可控）
	a := NewAttestor()
	rep, err := a.Attest(context.Background(), "enc-1")

	// 若真实运行环境就有 SGX（少见于 CI），跳过以免误判
	if rep.Capability.Available {
		t.Skip("本机真实存在 SGX，跳过 unavailable 断言")
	}
	if rep.Trusted {
		t.Error("无硬件时 Trusted 必须为 false")
	}
	if rep.Mode != ModeUnavailable {
		t.Errorf("期望 unavailable，得到 %s", rep.Mode)
	}
	if !errors.Is(err, ErrSGXUnavailable) {
		t.Errorf("期望 ErrSGXUnavailable，得到 %v", err)
	}
}

func TestAttestor_SimulationMode(t *testing.T) {
	setupFakeDevRoot(t)
	a := NewAttestor(WithSimulation())
	rep, err := a.Attest(context.Background(), "enc-sim")

	if rep.Capability.Available {
		t.Skip("本机真实存在 SGX，跳过 simulation 断言")
	}
	if err != nil {
		t.Fatalf("仿真模式不应返回错误，得到 %v", err)
	}
	if rep.Mode != ModeSimulation {
		t.Errorf("期望 simulation，得到 %s", rep.Mode)
	}
	if rep.Trusted {
		t.Error("仿真结果 Trusted 必须为 false（安全不变量）")
	}
}

// fakeHWBackend 用于测试硬件分支（内部包可直接构造 Attestor）
type fakeHWBackend struct {
	measurement string
	err         error
}

func (f *fakeHWBackend) GenerateAndVerifyQuote(ctx context.Context, enclaveID string) (string, error) {
	return f.measurement, f.err
}

func TestAttestor_HardwareBranch_Success(t *testing.T) {
	// 内部包直接构造：模拟"有硬件 + 已注册后端"
	a := &Attestor{
		cap:     SGXCapability{Available: true, Driver: DriverDCAPKernel, DCAPReady: true, OS: "linux"},
		backend: &fakeHWBackend{measurement: "MRENCLAVE-abc123"},
	}
	rep, err := a.Attest(context.Background(), "enc-hw")
	if err != nil {
		t.Fatalf("硬件后端成功时不应报错: %v", err)
	}
	if rep.Mode != ModeHardware || !rep.Trusted {
		t.Errorf("期望 hardware+Trusted，得到 mode=%s trusted=%v", rep.Mode, rep.Trusted)
	}
	if rep.Measurement != "MRENCLAVE-abc123" {
		t.Errorf("期望回填度量值，得到 %q", rep.Measurement)
	}
}

func TestAttestor_HardwareDetectedButNoBackend(t *testing.T) {
	// 有硬件但未编译后端：必须诚实降级为不可信
	a := &Attestor{
		cap:     SGXCapability{Available: true, Driver: DriverDCAPKernel, DCAPReady: true, OS: "linux"},
		backend: nil,
	}
	rep, err := a.Attest(context.Background(), "enc-nobackend")
	if err == nil {
		t.Error("未编译后端时应返回错误")
	}
	if rep.Trusted {
		t.Error("未编译后端时 Trusted 必须为 false")
	}
}

func TestAttestor_HardwareBranch_BackendError(t *testing.T) {
	a := &Attestor{
		cap:     SGXCapability{Available: true, Driver: DriverDCAPKernel, DCAPReady: true, OS: "linux"},
		backend: &fakeHWBackend{err: errors.New("quote-gen-failed")},
	}
	rep, err := a.Attest(context.Background(), "enc-fail")
	if err == nil {
		t.Error("后端报错时应返回错误")
	}
	if rep.Trusted {
		t.Error("后端报错时 Trusted 必须为 false")
	}
}
