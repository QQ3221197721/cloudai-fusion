package common

import (
	"testing"
	"time"
)

func TestNewUUID(t *testing.T) {
	uuid1 := NewUUID()
	uuid2 := NewUUID()

	if uuid1 == "" {
		t.Error("UUID should not be empty")
	}

	if uuid1 == uuid2 {
		t.Error("two UUIDs should be unique")
	}

	// UUID format: 8-4-4-4-12
	if len(uuid1) != 36 {
		t.Errorf("UUID should be 36 chars, got %d: %s", len(uuid1), uuid1)
	}
}

func TestNowUTC(t *testing.T) {
	now := NowUTC()
	if now.Location() != time.UTC {
		t.Error("NowUTC should return UTC time")
	}

	// Should be within last second
	diff := time.Since(now)
	if diff > time.Second {
		t.Errorf("NowUTC drift too large: %v", diff)
	}
}

func TestStringPtr(t *testing.T) {
	s := "hello"
	p := StringPtr(s)
	if p == nil {
		t.Fatal("pointer should not be nil")
	}
	if *p != s {
		t.Errorf("expected %q, got %q", s, *p)
	}
}

func TestIntPtr(t *testing.T) {
	i := 42
	p := IntPtr(i)
	if p == nil {
		t.Fatal("pointer should not be nil")
	}
	if *p != i {
		t.Errorf("expected %d, got %d", i, *p)
	}
}

func TestTimePtr(t *testing.T) {
	now := time.Now()
	p := TimePtr(now)
	if p == nil {
		t.Fatal("pointer should not be nil")
	}
	if !p.Equal(now) {
		t.Error("time should match")
	}
}

func TestCalculateUtilization(t *testing.T) {
	tests := []struct {
		used, total int64
		expected    float64
	}{
		{50, 100, 50.0},
		{0, 100, 0.0},
		{100, 100, 100.0},
		{0, 0, 0.0}, // zero division safe
		{75, 200, 37.5},
	}

	for _, tc := range tests {
		result := CalculateUtilization(tc.used, tc.total)
		if result != tc.expected {
			t.Errorf("CalculateUtilization(%d, %d) = %.2f, expected %.2f", tc.used, tc.total, result, tc.expected)
		}
	}
}

func TestCloudProviderTypes(t *testing.T) {
	providers := []CloudProviderType{
		CloudProviderAliyun,
		CloudProviderAWS,
		CloudProviderAzure,
		CloudProviderGCP,
		CloudProviderHuawei,
		CloudProviderTencent,
	}

	seen := make(map[CloudProviderType]bool)
	for _, p := range providers {
		if p == "" {
			t.Error("provider type should not be empty")
		}
		if seen[p] {
			t.Errorf("duplicate provider type: %s", p)
		}
		seen[p] = true
	}

	if len(providers) != 6 {
		t.Errorf("expected 6 cloud providers, got %d", len(providers))
	}
}

func TestWorkloadTypes(t *testing.T) {
	types := []WorkloadType{
		WorkloadTypeTraining,
		WorkloadTypeInference,
		WorkloadTypeFineTuning,
		WorkloadTypeBatch,
		WorkloadTypeServing,
	}

	for _, wt := range types {
		if wt == "" {
			t.Error("workload type should not be empty")
		}
	}
}

func TestGPUTypes(t *testing.T) {
	types := []GPUType{
		GPUTypeNvidiaA100,
		GPUTypeNvidiaH100,
		GPUTypeNvidiaH200,
		GPUTypeNvidiaL40S,
		GPUTypeAMDMI300X,
		GPUTypeHuaweiAscend,
	}

	for _, gt := range types {
		if gt == "" {
			t.Error("GPU type should not be empty")
		}
	}
}
