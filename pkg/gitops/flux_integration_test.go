//go:build integration

package gitops

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// TestFluxClient_Integration_RealCluster runs ONLY with -tags integration against
// a real cluster (context kind-cloudai). It creates a real GitRepository CR and
// reads its status back through the dynamic client, proving the platform talks to
// the real Flux API (not a fake). Run:
//
//	go test -tags integration ./pkg/gitops/ -run Integration -v
func TestFluxClient_Integration_RealCluster(t *testing.T) {
	ctx := context.Background()
	cfg, err := RESTConfigForContext("kind-cloudai")
	if err != nil {
		t.Skipf("no kubeconfig/context kind-cloudai: %v", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("dynamic client: %v", err)
	}
	fc := NewFluxClient(dyn, nil, nil)

	if !fc.Detect(ctx) {
		t.Skip("Flux CRDs not present in the cluster")
	}

	const (
		name = "caf-itest"
		ns   = "flux-system"
	)
	gr := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "source.toolkit.fluxcd.io/v1",
		"kind":       "GitRepository",
		"metadata":   map[string]interface{}{"name": name, "namespace": ns},
		"spec": map[string]interface{}{
			"interval": "1m",
			"url":      "https://github.com/stefanprodan/podinfo",
			"ref":      map[string]interface{}{"branch": "master"},
		},
	}}
	if _, err := dyn.Resource(gitRepositoryGVR).Namespace(ns).Create(ctx, gr, metav1.CreateOptions{}); err != nil {
		t.Logf("create GitRepository (may already exist): %v", err)
	}
	t.Cleanup(func() {
		_ = dyn.Resource(gitRepositoryGVR).Namespace(ns).Delete(context.Background(), name, metav1.DeleteOptions{})
	})

	st, err := fc.GitRepositoryStatus(ctx, ns, name)
	if err != nil {
		t.Fatalf("read real GitRepository status: %v", err)
	}
	if st.Name != name || st.Kind != "GitRepository" {
		t.Fatalf("unexpected status object: %+v", st)
	}
	t.Logf("REAL GitRepository status: ready=%v reason=%q revision=%q", st.Ready, st.Reason, st.Revision)
}
