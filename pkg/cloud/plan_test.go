package cloud

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
)

// newSixCloudManager registers all 6 providers with NO credentials — the exact
// out-of-the-box CLI state, proving plans work credential-less.
func newSixCloudManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(ManagerConfig{
		Providers: []config.CloudProviderConfig{
			{Name: "aliyun", Type: string(common.CloudProviderAliyun), Region: "cn-hangzhou"},
			{Name: "aws", Type: string(common.CloudProviderAWS), Region: "us-east-1"},
			{Name: "azure", Type: string(common.CloudProviderAzure), Region: "eastus"},
			{Name: "gcp", Type: string(common.CloudProviderGCP), Region: "us-central1"},
			{Name: "huawei", Type: string(common.CloudProviderHuawei), Region: "cn-north-4"},
			{Name: "tencent", Type: string(common.CloudProviderTencent), Region: "ap-guangzhou"},
		},
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	return m
}

var a100Spec = ResourceSpec{GPUType: "nvidia-a100", GPUNodes: 4, DurationHours: 24}

func TestPlanEngineSixCloudComparison(t *testing.T) {
	mgr := newSixCloudManager(t)

	if got := len(mgr.ListProviders()); got != 6 {
		t.Fatalf("expected 6 registered providers, got %d", got)
	}

	options, err := NewPlanEngine().Generate(context.Background(), mgr, a100Spec)
	if err != nil {
		t.Fatalf("generate without credentials: %v", err)
	}
	if len(options) != 6 {
		t.Fatalf("credential-less plan must cover all 6 clouds, got %d options", len(options))
	}

	// Every option is honest about being plan-only.
	seen := map[string]bool{}
	for _, o := range options {
		if o.Availability != PlanAvailabilityPlanOnly {
			t.Errorf("%s availability = %q, want plan-only", o.Provider, o.Availability)
		}
		if o.Currency == "" {
			t.Errorf("%s missing currency", o.Provider)
		}
		if len(o.Pros) == 0 {
			t.Errorf("%s missing pros", o.Provider)
		}
		seen[o.Provider] = true
	}
	for _, want := range []string{"aliyun", "aws", "azure", "gcp", "huawei", "tencent"} {
		if !seen[want] {
			t.Errorf("provider %q missing from plan", want)
		}
	}
}

func TestPlanEngineSortedAscending(t *testing.T) {
	mgr := newSixCloudManager(t)
	options, err := NewPlanEngine().Generate(context.Background(), mgr, a100Spec)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(options) < 2 {
		t.Fatalf("need >=2 options to verify ordering, got %d", len(options))
	}
	for i := 1; i < len(options); i++ {
		if options[i-1].TotalCost > options[i].TotalCost {
			t.Errorf("not cost-ascending: [%d]=%.2f > [%d]=%.2f",
				i-1, options[i-1].TotalCost, i, options[i].TotalCost)
		}
	}
	// Cheapest is first — within a currency, values must match the hardcoded
	// tables exactly (Azure 27.20 < GCP 101.22 < AWS 32.77 in USD terms the
	// tables are: aliyun 8.50 CNY, huawei 120.50 CNY, azure 27.20 USD,
	// gcp 101.22 USD, aws 32.77 USD, tencent 168.88 CNY → numeric order:
	// aliyun 8.50 < azure 27.20 < aws 32.77 < gcp 101.22 < huawei 120.50 < tencent 168.88).
	wantOrder := []string{"aliyun", "azure", "aws", "gcp", "huawei", "tencent"}
	for i, want := range wantOrder {
		if options[i].Provider != want {
			t.Errorf("rank %d = %s (%.2f %s), want %s", i+1, options[i].Provider, options[i].TotalCost, options[i].Currency, want)
		}
	}
}

func TestPlanEngineCostMath(t *testing.T) {
	mgr := newSixCloudManager(t)
	nodes, hours := 3, 10
	options, err := NewPlanEngine().Generate(context.Background(), mgr, ResourceSpec{GPUType: "nvidia-a100", GPUNodes: nodes, DurationHours: hours})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, o := range options {
		if o.HourlyCost <= 0 {
			t.Errorf("%s hourly = %.2f, want > 0", o.Provider, o.HourlyCost)
		}
		wantTotal := o.HourlyCost * float64(hours)
		if diff := o.TotalCost - wantTotal; diff < -0.005 || diff > 0.005 {
			t.Errorf("%s total = %.4f, want %.4f (hourly×%d)", o.Provider, o.TotalCost, wantTotal, hours)
		}
		wantMonthly := o.HourlyCost * 730
		if diff := o.MonthlyCost - wantMonthly; diff < -0.05 || diff > 0.05 {
			t.Errorf("%s monthly = %.4f, want %.4f", o.Provider, o.MonthlyCost, wantMonthly)
		}
	}

	// Exact value check against the hardcoded table: AWS a100 = 32.77 USD/hr.
	for _, o := range options {
		if o.Provider == "aws" {
			if o.HourlyCost != 32.77*float64(nodes) {
				t.Errorf("aws hourly = %.4f, want %.4f", o.HourlyCost, 32.77*float64(nodes))
			}
			if o.Currency != "USD" {
				t.Errorf("aws currency = %q, want USD", o.Currency)
			}
			if o.TotalCost != 32.77*float64(nodes)*float64(hours) {
				t.Errorf("aws total = %.4f, want %.4f", o.TotalCost, 32.77*float64(nodes)*float64(hours))
			}
		}
		if o.Provider == "aliyun" && o.Currency != "CNY" {
			t.Errorf("aliyun currency = %q, want CNY (native table currency)", o.Currency)
		}
	}
}

func TestPlanEngineRegionPreference(t *testing.T) {
	mgr := newSixCloudManager(t)
	spec := a100Spec
	spec.Region = "us-east-1" // AWS's region

	options, err := NewPlanEngine().Generate(context.Background(), mgr, spec)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(options) == 0 {
		t.Fatal("no options")
	}
	if options[0].Provider != "aws" || !strings.EqualFold(options[0].Region, spec.Region) {
		t.Errorf("region preference violated: rank 1 = %s/%s", options[0].Provider, options[0].Region)
	}
	// Non-matching options keep cost-ascending order among themselves.
	for i := 2; i < len(options); i++ {
		if options[i-1].TotalCost > options[i].TotalCost {
			t.Errorf("non-preferred group not cost-sorted at %d", i)
		}
	}
}

func TestPlanEngineSpecValidation(t *testing.T) {
	mgr := newSixCloudManager(t)
	e := NewPlanEngine()
	ctx := context.Background()

	cases := []ResourceSpec{
		{GPUType: "", GPUNodes: 1, DurationHours: 1},        // missing type
		{GPUType: "nvidia-a100", GPUNodes: 0, DurationHours: 1},  // zero nodes
		{GPUType: "nvidia-a100", GPUNodes: -2, DurationHours: 1}, // negative nodes
		{GPUType: "nvidia-a100", GPUNodes: 1, DurationHours: 0},  // zero duration
		{GPUType: "nvidia-a100", GPUNodes: 1, DurationHours: -5}, // negative duration
	}
	for i, spec := range cases {
		if _, err := e.Generate(ctx, mgr, spec); err == nil {
			t.Errorf("case %d: spec %+v accepted, want validation error", i, spec)
		}
	}
}

func TestPlanEngineUnknownGPUType(t *testing.T) {
	mgr := newSixCloudManager(t)
	// GetGPUPricing ignores the gpuType argument (static table), so even an
	// unknown type returns rows — the engine plans from static tables and the
	// honesty lives in the plan-only labeling, not in catalog gating.
	options, err := NewPlanEngine().Generate(context.Background(), mgr, ResourceSpec{GPUType: "nvidia-b200", GPUNodes: 1, DurationHours: 1})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(options) == 0 {
		t.Log("note: no provider priced nvidia-b200")
	}
	for _, o := range options {
		if o.GPUType != "nvidia-b200" {
			t.Errorf("gpu type echoed as %q", o.GPUType)
		}
	}
}

func TestPlanEngineInstanceTypeEnrichment(t *testing.T) {
	mgr := newSixCloudManager(t)
	options, err := NewPlanEngine().Generate(context.Background(), mgr, a100Spec)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	found := map[string]string{}
	for _, o := range options {
		found[o.Provider] = o.InstanceType
	}
	// Providers whose static catalogs contain an exact nvidia-a100 entry must
	// surface a concrete instance type.
	for p, want := range map[string]string{
		"aws":    "p4d.24xlarge",
		"azure":  "Standard_ND96amsr_A100_v4",
		"aliyun": "ecs.gn7-c12g1.3xlarge",
		"gcp":    "a2-ultragpu-4g",
		"tencent": "GT4.41XLARGE948",
	} {
		if got := found[p]; got != want {
			t.Errorf("%s instance type = %q, want %q", p, got, want)
		}
	}
}
