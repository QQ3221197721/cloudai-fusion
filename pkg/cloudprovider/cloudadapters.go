package cloudprovider

import (
	"context"
	"fmt"
)

// Credentials carries the minimal credential material an adapter needs to
// determine whether it can reach a live cloud backend. Fields are provider
// specific; each adapter validates only the ones it requires.
type Credentials struct {
	// AWS
	AccessKeyID     string
	SecretAccessKey string
	// Azure
	SubscriptionID string
	TenantID       string
	ClientID       string
	ClientSecret   string
	// GCP
	ProjectID          string
	ServiceAccountJSON string
}

// cloudAdapter is the shared implementation for the AWS/Azure/GCP skeletons.
//
// It is deliberately honest: this build does NOT link any real cloud SDK
// transport, so every live operation returns a typed error explaining exactly
// why. Without credentials that error is ErrCredentialsRequired; with
// credentials it is ErrLiveBackendUnavailable (the SDK is not wired into this
// build). It NEVER fabricates a successful result.
//
// The commented "// LIVE SDK:" markers pinpoint where a production integration
// would swap in the vendor SDK call.
type cloudAdapter struct {
	kind         ProviderKind
	regions      []string
	hasCreds     bool
	credNote     string // human-readable list of which credentials are missing
	displayName  string
}

func (a *cloudAdapter) Capabilities() Capabilities {
	status := CredentialsRequired
	notes := fmt.Sprintf(
		"%s adapter is in credentials-required mode: %s. No live cloud SDK backend is linked in this build; all operations refuse honestly rather than faking success.",
		a.displayName, a.credNote,
	)
	if a.hasCreds {
		status = CredentialsSatisfied
		notes = fmt.Sprintf(
			"%s adapter has credentials configured, but no live cloud SDK backend is linked in this build; operations return ErrLiveBackendUnavailable rather than faking success.",
			a.displayName,
		)
	}
	return Capabilities{
		Provider:         a.kind,
		CredentialStatus: status,
		Online:           false, // no live SDK transport is linked in this build
		SupportedRegions: append([]string(nil), a.regions...),
		SupportsPricing:  false,
		Notes:            notes,
	}
}

// liveErr returns the honest reason this operation cannot proceed offline.
func (a *cloudAdapter) liveErr(op string) error {
	if !a.hasCreds {
		return fmt.Errorf("%s %s: %w", a.kind, op, ErrCredentialsRequired)
	}
	return fmt.Errorf("%s %s: %w", a.kind, op, ErrLiveBackendUnavailable)
}

func (a *cloudAdapter) ListInstances(_ context.Context) ([]Instance, error) {
	// LIVE SDK: e.g. ec2.DescribeInstances / compute.InstancesClient.NewListPager.
	return nil, a.liveErr("ListInstances")
}

func (a *cloudAdapter) CreateInstance(_ context.Context, _ CreateInstanceRequest) (string, error) {
	// LIVE SDK: e.g. ec2.RunInstances / compute.InstancesClient.BeginCreateOrUpdate.
	return "", a.liveErr("CreateInstance")
}

func (a *cloudAdapter) DeleteInstance(_ context.Context, _ string) error {
	// LIVE SDK: e.g. ec2.TerminateInstances / compute.InstancesClient.BeginDelete.
	return a.liveErr("DeleteInstance")
}

func (a *cloudAdapter) GetPricing(_, _ string) (*Pricing, error) {
	// LIVE SDK: e.g. AWS Price List API / Azure Retail Prices / GCP Cloud Billing Catalog.
	return nil, a.liveErr("GetPricing")
}

// NewAWSProvider builds an AWS EC2 adapter. Without AccessKeyID/SecretAccessKey
// it degrades honestly to credentials-required mode.
func NewAWSProvider(creds Credentials) Provider {
	has := creds.AccessKeyID != "" && creds.SecretAccessKey != ""
	note := "missing AWS AccessKeyID/SecretAccessKey"
	if has {
		note = "AWS credentials present"
	}
	return &cloudAdapter{
		kind:        ProviderAWS,
		displayName: "AWS EC2",
		regions:     []string{"us-east-1", "us-west-2", "eu-central-1", "ap-northeast-1"},
		hasCreds:    has,
		credNote:    note,
	}
}

// NewAzureProvider builds an Azure Virtual Machines adapter. It requires the
// four service-principal fields; otherwise it degrades to credentials-required.
func NewAzureProvider(creds Credentials) Provider {
	has := creds.SubscriptionID != "" && creds.TenantID != "" &&
		creds.ClientID != "" && creds.ClientSecret != ""
	note := "missing one or more of Azure SubscriptionID/TenantID/ClientID/ClientSecret"
	if has {
		note = "Azure service-principal credentials present"
	}
	return &cloudAdapter{
		kind:        ProviderAzure,
		displayName: "Azure VM",
		regions:     []string{"eastus", "westeurope", "southeastasia"},
		hasCreds:    has,
		credNote:    note,
	}
}

// NewGCPProvider builds a Google Compute Engine adapter. It requires ProjectID
// and a service-account JSON; otherwise it degrades to credentials-required.
func NewGCPProvider(creds Credentials) Provider {
	has := creds.ProjectID != "" && creds.ServiceAccountJSON != ""
	note := "missing GCP ProjectID and/or ServiceAccountJSON"
	if has {
		note = "GCP service-account credentials present"
	}
	return &cloudAdapter{
		kind:        ProviderGCP,
		displayName: "GCP Compute Engine",
		regions:     []string{"us-central1", "europe-west1", "asia-northeast1"},
		hasCreds:    has,
		credNote:    note,
	}
}

// Compile-time assertions that every backend satisfies the unified Provider
// surface. This is what makes the interface "unified".
var (
	_ Provider = (*LocalMockProvider)(nil)
	_ Provider = (*cloudAdapter)(nil)
)
