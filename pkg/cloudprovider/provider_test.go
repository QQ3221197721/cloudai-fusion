package cloudprovider

import (
	"context"
	"errors"
	"testing"
)

// TestLocalMock_ProviderInterface verifies all required Provider methods compile
// and execute successfully on a real mock backend.
func TestLocalMock_ProviderInterface(t *testing.T) {
	p := NewLocalMockProvider(WithoutLatency())

	ctx := context.Background()

	t.Run("ListInstancesEmpty", func(t *testing.T) {
		items, err := p.ListInstances(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("expected empty list, got %d items", len(items))
		}
	})

	t.Run("CreateInstance", func(t *testing.T) {
		req := CreateInstanceRequest{
			Name: "test-instance",
			Type: "t3.medium",
		}
		id, err := p.CreateInstance(ctx, req)
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		if id == "" {
			t.Fatal("empty instance ID")
		}
		if id != "mock-000000" {
			t.Errorf("expected deterministic ID 'mock-000000', got '%s'", id)
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		err := p.DeleteInstance(ctx, "nonexistent-id")
		if err != ErrInstanceNotFound {
			t.Fatalf("expected ErrInstanceNotFound, got %v", err)
		}
	})

	t.Run("CreateThenDelete", func(t *testing.T) {
		id, err := p.CreateInstance(ctx, CreateInstanceRequest{Name: "test-delete", Type: "t3.micro"})
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}

		existing, err := p.InstanceByID(ctx, id)
		if err != nil {
			t.Fatalf("get by ID failed: %v", err)
		}
		if existing == nil || existing.Name != "test-delete" {
			t.Fatalf("instance not found after creation")
		}

		err = p.DeleteInstance(ctx, id)
		if err != nil {
			t.Fatalf("delete failed: %v", err)
		}

		_, err = p.InstanceByID(ctx, id)
		if err != ErrInstanceNotFound {
			t.Fatalf("after delete expected ErrInstanceNotFound, got %v", err)
		}
	})
}

// TestCloudAdapters_HonorCredentialsRequired validates that AWS/Azure/GCP adapters
// refuse to fake operations without credentials, always returning typed errors.
func TestCloudAdapters_HonorCredentialsRequired(t *testing.T) {
	ctx := context.Background()

	t.Run("AWS_NoCredentials_ReturnsErrCredentialsRequired", func(t *testing.T) {
		p := NewAWSProvider(Credentials{}) // empty credentials

		caps := p.Capabilities()
		if caps.CredentialStatus != CredentialsRequired {
			t.Errorf("expected CredentialsRequired status, got %q", caps.CredentialStatus)
		}

		_, err := p.ListInstances(ctx)
		if !errors.Is(err, ErrCredentialsRequired) {
			t.Errorf("expected ErrCredentialsRequired, got %v", err)
		}
	})

	t.Run("Azure_NoCredentials_ReturnsErrCredentialsRequired", func(t *testing.T) {
		p := NewAzureProvider(Credentials{})

		caps := p.Capabilities()
		if caps.CredentialStatus != CredentialsRequired {
			t.Errorf("expected CredentialsRequired status, got %q", caps.CredentialStatus)
		}

		_, err := p.CreateInstance(ctx, CreateInstanceRequest{})
		if err == nil {
			t.Error("expected error, got nil")
		} else if !errors.Is(err, ErrCredentialsRequired) && !errors.Is(err, ErrLiveBackendUnavailable) {
			t.Errorf("expected credentials/backend error, got %v", err)
		}
	})

	t.Run("GCP_NoCredentials_ReturnsErrCredentialsRequired", func(t *testing.T) {
		p := NewGCPProvider(Credentials{})

		caps := p.Capabilities()
		if caps.CredentialStatus != CredentialsRequired {
			t.Errorf("expected CredentialsRequired status, got %q", caps.CredentialStatus)
		}

		err := p.DeleteInstance(ctx, "any-id")
		if err == nil {
			t.Error("expected error, got nil")
		} else if !errors.Is(err, ErrCredentialsRequired) && !errors.Is(err, ErrLiveBackendUnavailable) {
			t.Errorf("expected credentials/backend error, got %v", err)
		}
	})
}

// TestRegistry_UnifiedDispatch verifies Registry dispatches calls correctly to
// registered backends and returns ErrProviderNotRegistered when unknown.
func TestRegistry_UnifiedDispatch(t *testing.T) {
	reg := NewRegistry()
	p := NewLocalMockProvider(WithoutLatency())
	reg.Register(ProviderLocalMock, p)

	ctx := context.Background()

	t.Run("RegisterAndGet", func(t *testing.T) {
		got := reg.Get(ProviderLocalMock)
		if got != p {
			t.Fatal("registry get did not return registered provider")
		}
	})

	t.Run("UnifiedListInstances", func(t *testing.T) {
		id, _ := p.CreateInstance(ctx, CreateInstanceRequest{Name: "registry-test", Type: "t3.small"})
		defer p.DeleteInstance(ctx, id) // cleanup

		items, err := reg.ListInstances(ctx, ProviderLocalMock)
		if err != nil {
			t.Fatalf("list through registry failed: %v", err)
		}
		if len(items) == 0 {
			t.Fatal("expected at least one instance from registry call")
		}
	})

	t.Run("UnknownKindReturnsError", func(t *testing.T) {
		_, err := reg.ListInstances(ctx, ProviderAWS)
		if err != ErrProviderNotRegistered {
			t.Errorf("expected ErrProviderNotRegistered, got %v", err)
		}
	})

	t.Run("CapabilitiesThroughRegistry", func(t *testing.T) {
		caps, err := reg.Capabilities(ProviderLocalMock)
		if err != nil {
			t.Fatalf("capabilities call failed: %v", err)
		}
		if !caps.Online {
			t.Error("expected localmock to be online")
		}
	})
}
