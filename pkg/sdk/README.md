# CloudAI Fusion Go SDK

The official Go client library for the CloudAI Fusion platform. External developers use this package to interact with CloudAI Fusion — verifying evidence chains, scheduling GPU workloads, running security campaigns, and recording billable usage.

## Installation

```bash
go get github.com/cloudai-fusion/cloudai-fusion/pkg/sdk@latest
```

## Quick Start

```go
import sdk "github.com/cloudai-fusion/cloudai-fusion/pkg/sdk"

// Create a client
client := sdk.New("https://api.cloudai.io", sdk.WithAPIKey("caf_live_xxx"))

// Verify an evidence chain
result, err := client.Evidence.Verify(context.Background(), "production")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Chain valid: %v (%d entries)\n", result.Valid, result.EntryCount)

// Submit a GPU workload
job, err := client.GPU.SubmitJob(context.Background(), &sdk.GPUJob{
    Name:     "train-bert",
    GPUCount: 4,
    Image:    "nvcr.io/nvidia/pytorch:24.01",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Job scheduled: %s\n", job.ID)

// Start a red team campaign
campaign, err := client.Security.RunCampaign(context.Background(), &sdk.CampaignConfig{
    Name:       "Q4 exercise",
    Frameworks: []string{"MITRE-ATT&CK"},
    Scope:      []string{"api", "edge"},
})
if err != nil {
    log.Fatal(err)
}

// Record billable usage
receipt, err := client.Billing.RecordUsage(context.Background(), &sdk.UsageRecord{
    ResourceID: "gpu-h100-node-a",
    Category:   "gpu",
    Amount:     1.5,
    Unit:       "hour",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Billing receipt: %s (hash: %.8s...)\n", receipt.ID, receipt.ReceiptHash)
```

## Authentication

All requests are authenticated using an API key passed as a Bearer token:

```go
client := sdk.New("https://api.cloudai.io", sdk.WithAPIKey("caf_live_xxx"))
```

Keys can be scoped to namespaces for multi-tenant access control.

## Error Handling

Non-2xx responses return `*sdk.APIError` which includes the HTTP status code, a machine-readable error code, and a human-readable message:

```go
result, err := client.Evidence.Verify(ctx, "prod")
if err != nil {
    if apiErr, ok := err.(*sdk.APIError); ok {
        fmt.Printf("%d %s: %s\n", apiErr.StatusCode, apiErr.Code, apiErr.Message)
    }
    log.Fatal(err)
}
```

## Configuration Options

SDK clients support flexible configuration through option chaining:

```go
// Custom timeout
client := sdk.New(baseURL, sdk.WithTimeout(10*time.Second))

// Custom transport
transport := &http.Transport{Proxy: http.ProxyFromEnvironment}
client := sdk.New(baseURL, sdk.WithHTTPClient(&http.Client{Transport: transport}))

// Both together
client := sdk.New(baseURL,
    sdk.WithAPIKey(key),
    sdk.WithTimeout(30*time.Second),
    sdk.WithHTTPClient(customClient),
)
```

## Sub-Client Modules

Each module is accessed via its field on the Client:

### Evidence Chain (`Evidence`)

Tamper-evident, hash-chained ledger for verifiable control-plane events:

```go
// Verify integrity
res, err := client.Evidence.Verify(ctx, namespace)

// Add signed attestation
attest, err := client.Evidence.Attest(ctx, "model deployed")

// List recent entries
entries, err := client.Evidence.List(ctx, &sdk.ListOptions{Namespace: ns, Limit: 10})
```

### GPU Scheduling (`GPU`)

Submit workloads and inspect accelerator topology:

```go
// Schedule a job
job, err := client.GPU.SubmitJob(ctx, &sdk.GPUJob{
    Name:     "train",
    GPUCount: 8,
    Image:    "nvcr.io/pytorch",
})

// List available GPUs
gpus, err := client.GPU.ListGPUs(ctx)

// Get topology map
topo, err := client.GPU.GetTopology(ctx)
for _, link := range topo.Links {
    fmt.Printf("%s ↔️ %s (%.1f GB/s %s)\n", link.Source, link.Target, link.BandwidthGBps, link.Type)
}
```

### Security (`Security`)

Red team campaigns and ATT&CK coverage analysis:

```go
// Run campaign
camp, err := client.Security.RunCampaign(ctx, &sdk.CampaignConfig{
    Name:       "Red Team Q4",
    Frameworks: []string{"MITRE-ATT&CK"},
    Scope:      []string{"api", "edge", "mesh"},
})

// Check coverage stats
coverage, err := client.Security.GetCoverage(ctx)
fmt.Printf("Health score: %d/100 (%d frameworks tracked)\n", coverage.HealthScore, coverage.TotalFrameworks)
```

### Billing (`Billing`)

Record usage with cryptographic receipts:

```go
// Bill a resource
receipt, err := client.Billing.RecordUsage(ctx, &sdk.UsageRecord{
    ResourceID: "gpu-h100-node-a",
    Category:   "gpu",
    Amount:     1.5,
    Unit:       "hour",
})
// receipt.ReceiptHash allows independent verification of billing correctness
```

## Testing

Tests use standard-library `httptest.Server` mocks. To run:

```bash
go test ./pkg/sdk/... -v
```

Example assertions:

```bash
=== RUN   TestNewClient/wires_sub-clients
--- PASS: TestNewClient/wires_sub-clients
=== RUN   TestEvidenceVerifyPathEscaping
--- PASS: TestEvidenceVerifyPathEscaping
PASS
ok      github.com/cloudai-fusion/cloudai-fusion/pkg/sdk    0.035s
```

## Design Philosophy

This SDK follows patterns established by Docker, AWS, and Kubernetes client libraries:

- **Single Client instance** shared across goroutines holds credentials and transport
- **Option pattern** for fluent construction and testing overrides
- **Sub-client fields** isolate concerns while sharing configuration
- **JSON encoding** for all payloads with typed errors
- **Context-aware** methods throughout for cancellation and timeouts

External developers can build against this stable surface and rely on our versioning promises.

## Versioning

CloudAI Fusion uses semantic versioning for this SDK. Breaking changes will bump the major version; all methods are backwards compatible unless noted.

To upgrade:

```bash
go get github.com/cloudai-fusion/cloudai-fusion/pkg/sdk@v1.0.0
```
