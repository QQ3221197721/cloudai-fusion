// cmd_cloud_test.go walks the real `cafctl cloud` developer journeys with no
// credentials configured: provider-list (all 6 clouds visible in stub mode),
// plan (cost-sorted multi-cloud comparison), estimate-cost (exact table math),
// and operations (empty state, then non-empty after a tracker write).
package main

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cloudPlanRank extracts a provider's rank from plan output (tabwriter expands
// tabs, so parse the rendered "  <rank>  <provider>  ..." rows).
func cloudPlanRank(output, provider string) int {
	for _, ln := range strings.Split(output, "\n") {
		f := strings.Fields(ln)
		if len(f) >= 2 {
			if n, err := strconv.Atoi(f[0]); err == nil && f[1] == provider {
				return n
			}
		}
	}
	return -1
}

// runCloudCmd executes one cloud subcommand and returns its captured output.
func runCloudCmd(t *testing.T, sub string, args ...string) string {
	t.Helper()
	var subCmd *cobra.Command
	switch sub {
	case "provider-list":
		subCmd = newCloudProviderListCmd()
	case "cluster-list":
		subCmd = newCloudClusterListCmd()
	case "ping":
		subCmd = newCloudPingCmd()
	case "plan":
		subCmd = newCloudPlanCmd()
	case "estimate-cost":
		subCmd = newCloudEstimateCostCmd()
	case "operations":
		subCmd = newCloudOperationsCmd()
	default:
		t.Fatalf("unknown subcommand: %q", sub)
	}
	buf := wireCmd(subCmd)
	subCmd.SetArgs(args)
	require.NoError(t, subCmd.Execute(), "%s must succeed: %v", sub, args)
	return buf.String()
}

// Journey 1: provider-list shows all 6 clouds in honest stub mode when no
// config file defines credentials.
func TestCloudProviderList_ShowsSixCloudsStubMode(t *testing.T) {
	out := runCloudCmd(t, "provider-list")

	for _, name := range []string{"aliyun", "aws", "azure", "gcp", "huawei", "tencent"} {
		assert.Contains(t, out, name, "provider %s missing from provider-list", name)
	}
	// All rows are stub (no credentials in the test environment config paths).
	assert.Contains(t, out, "stub", "stub MODE label missing")
	assert.Contains(t, out, "NAME", "table header missing")
	assert.Contains(t, out, "MODE", "table header missing")
	assert.Contains(t, out, "REGION", "table header missing")

	// JSON journey: machine-readable rows carry mode=stub.
	outJSON := runCloudCmd(t, "provider-list", "--output", "json")
	assert.Contains(t, outJSON, "\"mode\": \"stub\"")
	assert.Contains(t, outJSON, "\"type\": \"aws\"")
}

// Journey 2: plan ranks providers by cost ascending and declares plan-only.
func TestCloudPlan_CheapestFirst(t *testing.T) {
	store := t.TempDir()
	out := runCloudCmd(t, "plan",
		"--gpu-type", "nvidia-a100",
		"--gpu-nodes", "4",
		"--duration-hours", "24",
		"--store", store,
	)

	// The honesty declaration.
	assert.Contains(t, out, "plan-only, no cloud API calls")

	// Cost-ascending across the static tables (numeric, native currency):
	// aliyun 8.50 CNY < azure 27.20 USD < aws 32.77 USD < gcp 101.22 USD
	// < huawei 120.50 CNY < tencent 168.88 CNY.
	wantRanks := map[string]int{
		"aliyun": 1, "azure": 2, "aws": 3, "gcp": 4, "huawei": 5, "tencent": 6,
	}
	for provider, want := range wantRanks {
		got := cloudPlanRank(out, provider)
		require.GreaterOrEqual(t, got, 1, "provider %s missing from plan table", provider)
		assert.Equal(t, want, got, "%s must rank %d (cost-ascending)", provider, want)
	}

	// Recommendation names the cheapest provider.
	assert.Contains(t, out, "aliyun", "cheapest provider should be recommended")

	// The run is attested (default wiring, MemoryStore+EphemeralSigner ledger).
	assert.Contains(t, out, "Attestation:")
	assert.Contains(t, out, "cloud.plan")

	// Region preference hoists the matching provider to rank 1.
	outRegion := runCloudCmd(t, "plan",
		"--gpu-type", "nvidia-a100", "--gpu-nodes", "4", "--duration-hours", "24",
		"--region", "us-east-1", "--store", t.TempDir(),
	)
	assert.Equal(t, 1, cloudPlanRank(outRegion, "aws"), "aws should be hoisted to rank 1 for --region us-east-1")
}

// Journey 3: estimate-cost prints exact table math (aws a100 = 32.77 USD/hr;
// 4 nodes × 24 h = 3145.92 total).
func TestCloudEstimateCost_ExactValues(t *testing.T) {
	out := runCloudCmd(t, "estimate-cost",
		"--gpu-type", "nvidia-a100",
		"--gpu-nodes", "4",
		"--duration-hours", "24",
	)
	assert.Contains(t, out, "3145.92", "aws total 32.77×4×24 must render exactly")
	assert.Contains(t, out, "131.08", "aws hourly 32.77×4 must render exactly")
	assert.Contains(t, out, "aliyun")
	assert.Contains(t, out, "tencent")

	// JSON journey exposes machine-checkable numbers.
	outJSON := runCloudCmd(t, "estimate-cost",
		"--gpu-type", "nvidia-a100", "--gpu-nodes", "4", "--duration-hours", "24",
		"--output", "json",
	)
	assert.Contains(t, outJSON, "\"total_cost\": 3145.92")
	assert.Contains(t, outJSON, "\"currency\": \"CNY\"")
	assert.Contains(t, outJSON, "\"availability\": \"plan-only\"")

	// Invalid specs fail loudly instead of coercing.
	var bad *cobra.Command = newCloudEstimateCostCmd()
	wireCmd(bad)
	bad.SetArgs([]string{"--gpu-type", "nvidia-a100", "--gpu-nodes", "0", "--duration-hours", "24"})
	require.Error(t, bad.Execute(), "gpu-nodes=0 must be rejected")
}

// Journey 4: operations shows an honest empty state, then a real row after an
// OperationTracker write (the same store layout the CLI reads).
func TestCloudOperations_EmptyThenNonEmpty(t *testing.T) {
	store := t.TempDir()

	// Empty state.
	out := runCloudCmd(t, "operations", "--store", store)
	assert.Contains(t, out, "No lifecycle operations recorded yet")

	// Write one operation through the public package API (exactly what a
	// provisioning flow would do), using a real stub-mode provider.
	mgr, err := cloud.NewManager(cloud.ManagerConfig{
		Providers: []config.CloudProviderConfig{
			{Name: "aws", Type: string(common.CloudProviderAWS), Region: "us-east-1"},
		},
	})
	require.NoError(t, err)
	awsProv, err := mgr.GetProvider("aws")
	require.NoError(t, err)

	tracker, err := cloud.NewOperationTracker(store, nil)
	require.NoError(t, err)
	ctx := context.Background()
	op, err := tracker.Start(ctx, awsProv, &cloud.CreateClusterRequest{
		Name: "cli-test", NodeCount: 4, NodeType: "p4d.24xlarge", GPUNodeCount: 4, GPUNodeType: "nvidia-a100",
	})
	require.NoError(t, err)
	require.NoError(t, tracker.MarkProvisioning(ctx, op.ID))

	// Non-empty state: the newest row shows the op prefix and FSM state.
	out2 := runCloudCmd(t, "operations", "--store", store)
	assert.Contains(t, out2, op.ID[:8], "operation ID prefix missing")
	assert.Contains(t, out2, "provisioning", "FSM state missing")

	// JSON journey round-trips the LWW-merged record.
	outJSON := runCloudCmd(t, "operations", "--store", store, "--output", "json")
	assert.Contains(t, outJSON, "\"state\": \"provisioning\"")
	assert.Contains(t, outJSON, "\"id\": \""+op.ID+"\"")

	// --limit trims the view (write a second op; limit=1 shows only the newest).
	op2, err := tracker.Start(ctx, awsProv, &cloud.CreateClusterRequest{
		Name: "second", NodeCount: 1, NodeType: "t3.large",
	})
	require.NoError(t, err)
	out3 := runCloudCmd(t, "operations", "--store", store, "--limit", "1")
	assert.Contains(t, out3, op2.ID[:8], "newest op must be shown with limit=1")
	assert.NotContains(t, out3, op.ID[:8], "older op must be trimmed with limit=1")
}

// Journey 2b: cluster-list degrades honestly in stub mode (empty, no error).
func TestCloudClusterList_StubEmptyAndPing_Degraded(t *testing.T) {
	out := runCloudCmd(t, "cluster-list")
	assert.Contains(t, out, "No clusters found")

	outPing := runCloudCmd(t, "ping")
	// Stub providers must show degraded — not error — without credentials.
	assert.Contains(t, outPing, "degraded")
	assert.Contains(t, outPing, "stub mode")
	assert.NotContains(t, outPing, "\terror\t", "stub ping must not surface as error")
}
