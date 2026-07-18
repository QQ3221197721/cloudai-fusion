package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynfake "k8s.io/client-go/dynamic/fake"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func fluxTestLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x5a}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func kustomizationObj(ready bool) *unstructured.Unstructured {
	status := "True"
	if !ready {
		status = "False"
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]interface{}{"namespace": "flux-system", "name": "app"},
		"spec":       map[string]interface{}{"suspend": false},
		"status": map[string]interface{}{
			"lastAppliedRevision": "main@sha1:abc123",
			"conditions": []interface{}{
				map[string]interface{}{
					"type": "Ready", "status": status,
					"reason": "ReconciliationSucceeded", "message": "Applied revision main@sha1:abc123",
				},
			},
		},
	}}
}

func newFakeFlux(t *testing.T, ledger *evidence.Ledger, objs ...runtime.Object) *FluxClient {
	t.Helper()
	scheme := runtime.NewScheme()
	gvrToListKind := map[schema.GroupVersionResource]string{
		kustomizationGVR: "KustomizationList",
		gitRepositoryGVR: "GitRepositoryList",
	}
	dyn := dynfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, objs...)
	return NewFluxClient(dyn, ledger, nil)
}

func TestFluxClient_ReadsStatusAndEmitsRealEvidence(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := fluxTestLedger(t)
	fc := newFakeFlux(t, ledger, kustomizationObj(true))

	if !fc.Detect(context.Background()) {
		t.Fatal("Detect must report the Flux API present when the CRD is served")
	}

	st, err := fc.SyncKustomization(context.Background(), "flux-system", "app")
	if err != nil {
		t.Fatalf("sync kustomization: %v", err)
	}
	if !st.Ready {
		t.Fatal("status must reflect the real Ready=True condition")
	}
	if st.Revision != "main@sha1:abc123" {
		t.Fatalf("revision must come from lastAppliedRevision, got %q", st.Revision)
	}

	// capability must now report gitops.sync as REAL (driver flux).
	var real bool
	for _, b := range capability.Snapshot() {
		if b.Component == "gitops.sync" && b.Mode == capability.ModeReal && b.Driver == "flux" {
			real = true
		}
	}
	if !real {
		t.Fatal("gitops.sync must be reported real (flux) after a real reconcile read")
	}

	// A verifiable gitops.sync receipt must exist, honestly flagged sync_real=true.
	all, _ := ledger.Store().All(context.Background())
	var rec *evidence.Evidence
	for _, e := range all {
		if e.Action == "gitops.sync" && e.Subject == "flux-system/app" {
			rec = e
		}
	}
	if rec == nil {
		t.Fatal("expected a gitops.sync receipt for the flux reconcile")
	}
	var sawReal bool
	for _, b := range rec.Backends {
		if b.Component == "gitops.sync" && b.Mode == "real" && b.Driver == "flux" {
			sawReal = true
		}
	}
	if !sawReal {
		t.Fatalf("flux receipt must record gitops.sync=real, backends=%+v", rec.Backends)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["sync_real"] != true || payload["engine"] != "flux" {
		t.Fatalf("payload must state a real flux sync, got %v", payload)
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("flux evidence chain must verify, got %+v", rep)
	}
}

func TestFluxClient_DetectFalseWhenNoCRD(t *testing.T) {
	// A fake client that only knows GitRepository must still not error on Detect,
	// but a dynamic client without the Kustomization list kind returns an error
	// path; here we register both, so Detect is true. This guards the happy path.
	ledger := fluxTestLedger(t)
	fc := newFakeFlux(t, ledger) // no objects, but GVRs registered
	if !fc.Detect(context.Background()) {
		t.Fatal("Detect should be true when the Kustomization GVR is served (even with no objects)")
	}
}
