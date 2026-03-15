package common

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// ============================================================================
// Fuzz: CalculateUtilization (should never panic or return NaN/Inf)
// ============================================================================

func FuzzCalculateUtilization(f *testing.F) {
	f.Add(int64(50), int64(100))
	f.Add(int64(0), int64(0))
	f.Add(int64(0), int64(100))
	f.Add(int64(100), int64(100))
	f.Add(int64(-1), int64(100))
	f.Add(int64(200), int64(100)) // over 100%
	f.Add(int64(1), int64(1))
	f.Add(int64(9223372036854775807), int64(1))  // max int64
	f.Add(int64(-9223372036854775808), int64(1)) // min int64
	f.Add(int64(0), int64(-9223372036854775808)) // negative total
	f.Add(int64(9223372036854775807), int64(9223372036854775807))
	f.Add(int64(-9223372036854775808), int64(-9223372036854775808))

	f.Fuzz(func(t *testing.T, used, total int64) {
		result := CalculateUtilization(used, total)
		// Should never be NaN
		if math.IsNaN(result) {
			t.Errorf("CalculateUtilization(%d, %d) = NaN", used, total)
		}
		// Should never be Inf
		if math.IsInf(result, 0) {
			t.Errorf("CalculateUtilization(%d, %d) = Inf", used, total)
		}
		// When total is 0, result should be 0
		if total == 0 && result != 0 {
			t.Errorf("CalculateUtilization(%d, 0) = %f, want 0", used, result)
		}
	})
}

// ============================================================================
// Fuzz: NewUUID (should always return valid UUID format)
// ============================================================================

func FuzzNewUUID(f *testing.F) {
	f.Add(1)
	f.Add(100)
	f.Add(0)
	f.Add(-1)

	f.Fuzz(func(t *testing.T, n int) {
		count := n % 10
		if count < 0 {
			count = -count
		}
		seen := make(map[string]bool, count)
		for i := 0; i < count; i++ {
			u := NewUUID()
			if len(u) == 0 {
				t.Error("UUID should not be empty")
			}
			// UUID v4 format: 8-4-4-4-12 hex chars
			if len(u) != 36 {
				t.Errorf("UUID length = %d, want 36", len(u))
			}
			if seen[u] {
				t.Errorf("duplicate UUID: %s", u)
			}
			seen[u] = true
		}
	})
}

// ============================================================================
// Fuzz: ResourceRequest JSON round-trip (marshal/unmarshal consistency)
// ============================================================================

func FuzzResourceRequestJSON(f *testing.F) {
	f.Add(int64(1000), int64(1073741824), 4, "nvidia-a100", int64(85899345920), int64(0), int64(0))
	f.Add(int64(0), int64(0), 0, "", int64(0), int64(0), int64(0))
	f.Add(int64(-1), int64(-1), -1, "invalid", int64(-1), int64(-1), int64(-1))
	f.Add(int64(9223372036854775807), int64(9223372036854775807), 2147483647, "x", int64(9223372036854775807), int64(9223372036854775807), int64(9223372036854775807))

	f.Fuzz(func(t *testing.T, cpu, mem int64, gpuCount int, gpuType string, gpuMem, storage, net int64) {
		req := ResourceRequest{
			CPUMillicores:    cpu,
			MemoryBytes:      mem,
			GPUCount:         gpuCount,
			GPUType:          GPUType(gpuType),
			GPUMemoryBytes:   gpuMem,
			StorageBytes:     storage,
			NetworkBandwidth: net,
		}

		// Marshal should not panic
		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		// Unmarshal back should produce same struct
		var decoded ResourceRequest
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if decoded.CPUMillicores != req.CPUMillicores {
			t.Errorf("CPUMillicores mismatch: got %d, want %d", decoded.CPUMillicores, req.CPUMillicores)
		}
		if decoded.GPUCount != req.GPUCount {
			t.Errorf("GPUCount mismatch: got %d, want %d", decoded.GPUCount, req.GPUCount)
		}
	})
}

// ============================================================================
// Fuzz: APIResponse JSON (should never panic on arbitrary data)
// ============================================================================

func FuzzAPIResponseJSON(f *testing.F) {
	f.Add(200, "success", "trace-123")
	f.Add(0, "", "")
	f.Add(-1, strings.Repeat("x", 10000), "")
	f.Add(500, "error\x00null", "\xff\xfe")

	f.Fuzz(func(t *testing.T, code int, message, traceID string) {
		resp := APIResponse{
			Code:    code,
			Message: message,
			TraceID: traceID,
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		var decoded APIResponse
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if decoded.Code != resp.Code {
			t.Errorf("Code mismatch: %d vs %d", decoded.Code, resp.Code)
		}
	})
}

// ============================================================================
// Fuzz: GPUTopologyInfo JSON (complex nested struct)
// ============================================================================

func FuzzGPUTopologyInfoJSON(f *testing.F) {
	f.Add("node-1", 0, "gpu-uuid", "nvidia-a100", int64(81920000000), 50.5, 72)
	f.Add("", -1, "", "", int64(0), 0.0, 0)
	f.Add("\x00", 999, "\xff", "invalid", int64(-1), -1.0, -300)

	f.Fuzz(func(t *testing.T, nodeName string, gpuIdx int, gpuUUID, gpuType string, memBytes int64, util float64, temp int) {
		if math.IsNaN(util) || math.IsInf(util, 0) {
			t.Skip("skip NaN/Inf utilization")
		}

		info := GPUTopologyInfo{
			NodeName: nodeName,
			GPUDevices: []GPUDevice{{
				Index:       gpuIdx,
				UUID:        gpuUUID,
				Type:        GPUType(gpuType),
				MemoryBytes: memBytes,
				Utilization: util,
				Temperature: temp,
			}},
		}

		data, err := json.Marshal(info)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		var decoded GPUTopologyInfo
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if decoded.NodeName != info.NodeName {
			t.Errorf("NodeName mismatch")
		}
		if len(decoded.GPUDevices) != 1 {
			t.Errorf("GPUDevices length mismatch")
		}
	})
}

// ============================================================================
// Fuzz: PaginatedResponse boundary conditions
// ============================================================================

func FuzzPaginatedResponse(f *testing.F) {
	f.Add(int64(100), 1, 10)
	f.Add(int64(0), 0, 0)
	f.Add(int64(-1), -1, -1)
	f.Add(int64(9223372036854775807), 2147483647, 2147483647)

	f.Fuzz(func(t *testing.T, total int64, page, pageSize int) {
		resp := PaginatedResponse{
			Total:    total,
			Page:     page,
			PageSize: pageSize,
		}

		// Calculate total pages (should not panic)
		if pageSize > 0 {
			resp.TotalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		if len(data) == 0 {
			t.Error("empty JSON output")
		}
	})
}

// ============================================================================
// Benchmark: Common Utilities
// ============================================================================

func BenchmarkNewUUID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		NewUUID()
	}
}

func BenchmarkNowUTC(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		NowUTC()
	}
}

func BenchmarkCalculateUtilization(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		CalculateUtilization(75, 200)
	}
}

func BenchmarkResourceRequestMarshal(b *testing.B) {
	req := ResourceRequest{
		CPUMillicores: 4000, MemoryBytes: 8 * 1024 * 1024 * 1024,
		GPUCount: 4, GPUType: GPUTypeNvidiaA100,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		json.Marshal(req)
	}
}
