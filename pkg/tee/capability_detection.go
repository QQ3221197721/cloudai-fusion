package tee

import (
	"fmt"
	"os"
	"runtime"
)

// ============================================================================
// SGX Capability Detection (Real, Cross-Platform)
// ============================================================================
// Purpose: 在运行时真实探测本机是否具备可用的 Intel SGX 环境。
// Principle: 只报告"探测到的事实"，绝不假装。用户在任何机器上调用都能得到
//            正确结论——有硬件就说有，没有就说没有。
//
// 检测依据（Linux）：SGX 驱动会创建设备节点，其存在是"本机可用 SGX"的可靠信号：
//   - /dev/sgx_enclave + /dev/sgx_provision : 主线内核(5.11+) 的 DCAP 驱动
//   - /dev/sgx/enclave                       : 旧的 out-of-tree DCAP 驱动路径
//   - /dev/isgx                              : 遗留 EPID 驱动（已弃用）
// 非 Linux 平台（Windows/macOS）：本框架不支持这些平台上的 SGX，直接报告不可用。
// ============================================================================

// SGXDriver 标识探测到的 SGX 驱动类型
type SGXDriver string

const (
	DriverNone       SGXDriver = "none"         // 未探测到任何 SGX 驱动
	DriverDCAPKernel SGXDriver = "dcap_kernel"  // 主线内核 in-kernel DCAP (/dev/sgx_enclave)
	DriverDCAPOOT    SGXDriver = "dcap_oot"      // out-of-tree DCAP (/dev/sgx/enclave)
	DriverLegacyEPID SGXDriver = "legacy_epid"   // 遗留 /dev/isgx（EPID，已弃用）
)

// SGXCapability 描述本机 SGX 环境的探测结果
type SGXCapability struct {
	Available     bool      `json:"available"`      // 本机是否可用 SGX
	Driver        SGXDriver `json:"driver"`         // 探测到的驱动类型
	DCAPReady     bool      `json:"dcap_ready"`     // 是否支持 DCAP/ECDSA（服务器推荐路径）
	ProvisionNode bool      `json:"provision_node"` // 是否存在 /dev/sgx_provision（DCAP 出证所需）
	OS            string    `json:"os"`             // 运行平台 (runtime.GOOS)
	Detail        string    `json:"detail"`         // 人类可读的解释
}

// deviceExists 判断设备节点/文件是否存在（可测试时通过 sgxDevRoot 覆盖根路径）
func deviceExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// sgxDevRoot 允许测试注入伪造的设备根目录；生产为空表示使用真实 "/"。
var sgxDevRoot = ""

func devPath(p string) string {
	if sgxDevRoot == "" {
		return p
	}
	return sgxDevRoot + p
}

// DetectSGXCapability 探测本机 SGX 能力（纯 Go，无 CGO，任何平台都能安全调用）
func DetectSGXCapability() SGXCapability {
	// 非 Linux 平台：本框架不支持其 SGX 集成
	if runtime.GOOS != "linux" {
		return SGXCapability{
			Available: false,
			Driver:    DriverNone,
			OS:        runtime.GOOS,
			Detail:    fmt.Sprintf("SGX not supported on %s by this framework (Linux with SGX driver required)", runtime.GOOS),
		}
	}
	c := scanSGXDevices()
	c.OS = runtime.GOOS
	return c
}

// scanSGXDevices 仅按设备节点判定驱动类型（不做 OS 门控，便于单元测试注入 sgxDevRoot）
func scanSGXDevices() SGXCapability {
	cap := SGXCapability{
		Available: false,
		Driver:    DriverNone,
		OS:        runtime.GOOS,
	}

	// 主线内核 DCAP：/dev/sgx_enclave（+ /dev/sgx_provision 用于出证）
	if deviceExists(devPath("/dev/sgx_enclave")) {
		cap.Available = true
		cap.Driver = DriverDCAPKernel
		cap.DCAPReady = true
		cap.ProvisionNode = deviceExists(devPath("/dev/sgx_provision"))
		if cap.ProvisionNode {
			cap.Detail = "in-kernel DCAP driver detected (/dev/sgx_enclave + /dev/sgx_provision)"
		} else {
			cap.Detail = "in-kernel DCAP enclave node detected, but /dev/sgx_provision missing (attestation may be limited)"
		}
		return cap
	}

	// out-of-tree DCAP：/dev/sgx/enclave
	if deviceExists(devPath("/dev/sgx/enclave")) {
		cap.Available = true
		cap.Driver = DriverDCAPOOT
		cap.DCAPReady = true
		cap.ProvisionNode = deviceExists(devPath("/dev/sgx/provision"))
		cap.Detail = "out-of-tree DCAP driver detected (/dev/sgx/enclave)"
		return cap
	}

	// 遗留 EPID 驱动：/dev/isgx（已弃用，不建议服务器使用）
	if deviceExists(devPath("/dev/isgx")) {
		cap.Available = true
		cap.Driver = DriverLegacyEPID
		cap.DCAPReady = false
		cap.Detail = "legacy EPID driver detected (/dev/isgx); EPID is deprecated for servers, DCAP recommended"
		return cap
	}

	cap.Detail = "no SGX device node found (/dev/sgx_enclave, /dev/sgx/enclave, /dev/isgx); SGX unavailable on this host"
	return cap
}

// String 返回能力的简要描述
func (c SGXCapability) String() string {
	if !c.Available {
		return fmt.Sprintf("SGX unavailable (os=%s): %s", c.OS, c.Detail)
	}
	return fmt.Sprintf("SGX available (driver=%s, dcap=%v): %s", c.Driver, c.DCAPReady, c.Detail)
}
