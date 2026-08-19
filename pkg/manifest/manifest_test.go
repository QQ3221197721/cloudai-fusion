// Package manifest tests - validates the Evidence Manifest format standard (CAF-SPEC-001).
package manifest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// validManifestYAML is a complete, spec-compliant manifest used across tests.
const validManifestYAML = `apiVersion: caf.io/v1
kind: EvidenceManifest
metadata:
  name: production-cluster
  namespace: default
spec:
  chain:
    algorithm: groth16-bn254-poseidon2
    signer: ed25519-pem
    storage: append-only-merkle
  subjects:
    - type: deployment
      selector: "app=*"
      events: [created, updated, deleted, scaled]
    - type: security-scan
      selector: "pipeline=*"
      events: [passed, failed, remediated]
  policy:
    min-signers: 2
    max-age: 720h
    require-zkp: true
  export:
    formats: [sarif, slsa-provenance]
`

// mockStore is an in-memory EvidenceStore used to assert Apply behavior.
type mockStore struct {
	policies []SubjectRule
}

func (m *mockStore) AppendPolicy(_ context.Context, policy SubjectRule) error {
	m.policies = append(m.policies, policy)
	return nil
}

func (m *mockStore) ListPolicies(_ context.Context) ([]SubjectRule, error) {
	return m.policies, nil
}

func writeTempManifest(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence-manifest.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp manifest: %v", err)
	}
	return path
}

// TestParseValidManifest confirms a spec-compliant manifest parses into the
// expected typed structure.
func TestParseValidManifest(t *testing.T) {
	path := writeTempManifest(t, validManifestYAML)

	m, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse valid manifest: %v", err)
	}

	if m.APIVersion != ManifestVersion {
		t.Errorf("APIVersion = %q, want %q", m.APIVersion, ManifestVersion)
	}
	if m.Kind != "EvidenceManifest" {
		t.Errorf("Kind = %q, want EvidenceManifest", m.Kind)
	}
	if m.Metadata.Name != "production-cluster" {
		t.Errorf("Metadata.Name = %q, want production-cluster", m.Metadata.Name)
	}
	if m.Spec.Chain.Algorithm != AlgorithmGroth16BN254Poseidon2 {
		t.Errorf("Chain.Algorithm = %q, want %q", m.Spec.Chain.Algorithm, AlgorithmGroth16BN254Poseidon2)
	}
	if len(m.Spec.Subjects) != 2 {
		t.Fatalf("Subjects = %d, want 2", len(m.Spec.Subjects))
	}
	if m.Spec.Subjects[0].Type != "deployment" {
		t.Errorf("Subjects[0].Type = %q, want deployment", m.Spec.Subjects[0].Type)
	}
	if len(m.Spec.Subjects[0].Events) != 4 {
		t.Errorf("Subjects[0].Events = %v, want 4 events", m.Spec.Subjects[0].Events)
	}
	if m.Spec.Policy.MinSigners != 2 {
		t.Errorf("Policy.MinSigners = %d, want 2", m.Spec.Policy.MinSigners)
	}
	if !m.Spec.Policy.RequireZKP {
		t.Error("Policy.RequireZKP = false, want true")
	}
	if m.Spec.Policy.MaxAge.Hours() != 720 {
		t.Errorf("Policy.MaxAge = %v, want 720h", m.Spec.Policy.MaxAge.Duration)
	}
	if m.Source != path {
		t.Errorf("Source = %q, want %q", m.Source, path)
	}

	// A valid manifest must produce no error-level validation issues.
	for _, e := range m.Validate() {
		if e.Severity == SeverityError {
			t.Errorf("unexpected validation error: %v", e)
		}
	}
}

// TestValidateInvalidManifests exercises each error path of Validate().
func TestValidateInvalidManifests(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Manifest)
		wantField string
	}{
		{
			name:      "bad api version",
			mutate:    func(m *Manifest) { m.APIVersion = "caf.io/v99" },
			wantField: "apiVersion",
		},
		{
			name:      "wrong kind",
			mutate:    func(m *Manifest) { m.Kind = "Pod" },
			wantField: "kind",
		},
		{
			name:      "missing name",
			mutate:    func(m *Manifest) { m.Metadata.Name = "" },
			wantField: "metadata.name",
		},
		{
			name:      "no subjects",
			mutate:    func(m *Manifest) { m.Spec.Subjects = nil },
			wantField: "spec.subjects",
		},
		{
			name:      "unsupported algorithm",
			mutate:    func(m *Manifest) { m.Spec.Chain.Algorithm = "rot13" },
			wantField: "spec.chain.algorithm",
		},
		{
			name: "unknown subject type",
			mutate: func(m *Manifest) {
				m.Spec.Subjects[0].Type = "wizardry"
			},
			wantField: "spec.subjects[0].type",
		},
		{
			name: "min-signers below one",
			mutate: func(m *Manifest) {
				m.Spec.Policy.MinSigners = 0
			},
			wantField: "spec.policy.min-signers",
		},
		{
			name: "zkp without groth16",
			mutate: func(m *Manifest) {
				m.Spec.Chain.Algorithm = AlgorithmEd25519Secp256k1
				m.Spec.Policy.RequireZKP = true
			},
			wantField: "spec.policy.require-zkp",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := freshValidManifest(t)
			tc.mutate(m)

			errs := m.Validate()
			found := false
			for _, e := range errs {
				if e.Field == tc.wantField && e.Severity == SeverityError {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error on field %q; got %+v", tc.wantField, errs)
			}
		})
	}
}

// freshValidManifest parses the canonical valid manifest into a typed struct.
func freshValidManifest(t *testing.T) *Manifest {
	t.Helper()
	m, err := ParseFromBytes([]byte(validManifestYAML), "<test>")
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return m
}

// TestApplyCreatesChainConfiguration verifies Apply registers every subject as a
// stored policy on the injected EvidenceStore.
func TestApplyCreatesChainConfiguration(t *testing.T) {
	m := freshValidManifest(t)
	store := &mockStore{}

	if err := m.Apply(context.Background(), store); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	policies, err := store.ListPolicies(context.Background())
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(policies) != len(m.Spec.Subjects) {
		t.Fatalf("stored %d policies, want %d", len(policies), len(m.Spec.Subjects))
	}
	if policies[0].Type != "deployment" || policies[1].Type != "security-scan" {
		t.Errorf("unexpected stored policy order: %+v", policies)
	}
}

// TestApplyRejectsInvalidManifest ensures Apply refuses error-level manifests
// before touching the store.
func TestApplyRejectsInvalidManifest(t *testing.T) {
	m := freshValidManifest(t)
	m.Spec.Subjects = nil // triggers a SeverityError

	store := &mockStore{}
	if err := m.Apply(context.Background(), store); err == nil {
		t.Fatal("expected Apply to reject an invalid manifest")
	}
	if len(store.policies) != 0 {
		t.Errorf("store mutated on invalid manifest: %+v", store.policies)
	}
}

// TestNewDefaultManifestIsWellFormed checks the template generated by
// `cafctl manifest init` round-trips through Save/Parse.
func TestNewDefaultManifestIsWellFormed(t *testing.T) {
	m := NewDefaultManifest("demo", "team-a")
	if m.APIVersion != ManifestVersion || m.Kind != "EvidenceManifest" {
		t.Fatalf("default manifest header wrong: %+v", m)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "out.yaml")
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Parse(path)
	if err != nil {
		t.Fatalf("re-parse saved manifest: %v", err)
	}
	if reloaded.Metadata.Name != "demo" || reloaded.Metadata.Namespace != "team-a" {
		t.Errorf("round-trip lost metadata: %+v", reloaded.Metadata)
	}
}
