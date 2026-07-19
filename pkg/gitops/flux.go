package gitops

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// flux.go is the REAL Flux GitOps client. Unlike the ArgoCD REST path in
// manager.go, Flux is pull-based: its controllers reconcile GitRepository ->
// Kustomization objects in-cluster. This client talks to the real Flux CRDs via
// client-go's dynamic client (no need to vendor the whole fluxcd module), reads
// the actual reconcile status (Ready condition + applied revision), can trigger a
// real reconcile, reports gitops.sync=real when the Flux API is present, and
// emits a signed, verifiable receipt of what it observed.

var (
	// gitRepositoryGVR / kustomizationGVR are the Flux v1 CRD coordinates.
	gitRepositoryGVR = schema.GroupVersionResource{Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"}
	kustomizationGVR = schema.GroupVersionResource{Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"}
)

// reconcileRequestAnnotation is the standard annotation Flux watches to trigger
// an on-demand reconcile (the mechanism `flux reconcile` uses).
const reconcileRequestAnnotation = "reconcile.fluxcd.io/requestedAt"

// FluxStatus is the observed reconcile state of a Flux resource.
type FluxStatus struct {
	Kind       string    `json:"kind"`
	Namespace  string    `json:"namespace"`
	Name       string    `json:"name"`
	Ready      bool      `json:"ready"`
	Reason     string    `json:"reason,omitempty"`
	Message    string    `json:"message,omitempty"`
	Revision   string    `json:"revision,omitempty"` // lastAppliedRevision / artifact revision
	Suspended  bool      `json:"suspended"`
	ObservedAt time.Time `json:"observed_at"`
}

// FluxClient reads and reconciles real Flux resources via the dynamic client.
type FluxClient struct {
	dyn      dynamic.Interface
	recorder evidence.Recorder
	logger   *logrus.Logger
}

// NewFluxClient builds a Flux client over a dynamic.Interface (unit-testable with
// a fake dynamic client). recorder may be nil (evidence emission is skipped).
func NewFluxClient(dyn dynamic.Interface, recorder evidence.Recorder, logger *logrus.Logger) *FluxClient {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	return &FluxClient{dyn: dyn, recorder: recorder, logger: logger}
}

// NewFluxClientFromRESTConfig builds a Flux client from a Kubernetes rest.Config.
func NewFluxClientFromRESTConfig(cfg *rest.Config, recorder evidence.Recorder, logger *logrus.Logger) (*FluxClient, error) {
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("gitops: build dynamic client: %w", err)
	}
	return NewFluxClient(dyn, recorder, logger), nil
}

// RESTConfigForContext loads a rest.Config from the default kubeconfig using the
// given context (empty = current context). Used to target a specific kind cluster.
func RESTConfigForContext(contextName string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		overrides.CurrentContext = contextName
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	return cc.ClientConfig()
}

// Detect reports whether the Flux API (Kustomization CRD) is served by the cluster.
func (c *FluxClient) Detect(ctx context.Context) bool {
	_, err := c.dyn.Resource(kustomizationGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{Limit: 1})
	return err == nil
}

// KustomizationStatus reads the reconcile status of a Flux Kustomization.
func (c *FluxClient) KustomizationStatus(ctx context.Context, namespace, name string) (*FluxStatus, error) {
	return c.status(ctx, kustomizationGVR, "Kustomization", namespace, name, "lastAppliedRevision")
}

// GitRepositoryStatus reads the reconcile status of a Flux GitRepository.
func (c *FluxClient) GitRepositoryStatus(ctx context.Context, namespace, name string) (*FluxStatus, error) {
	return c.status(ctx, gitRepositoryGVR, "GitRepository", namespace, name, "")
}

// status fetches the object and extracts the Ready condition + revision.
func (c *FluxClient) status(ctx context.Context, gvr schema.GroupVersionResource, kind, namespace, name, revisionField string) (*FluxStatus, error) {
	obj, err := c.dyn.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("gitops: get %s %s/%s: %w", kind, namespace, name, err)
	}
	st := &FluxStatus{Kind: kind, Namespace: namespace, Name: name, ObservedAt: time.Now().UTC()}

	if suspended, found, _ := unstructured.NestedBool(obj.Object, "spec", "suspend"); found {
		st.Suspended = suspended
	}
	if conds, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions"); found {
		for _, ci := range conds {
			cm, ok := ci.(map[string]interface{})
			if !ok {
				continue
			}
			if fmt.Sprintf("%v", cm["type"]) == "Ready" {
				st.Ready = fmt.Sprintf("%v", cm["status"]) == "True"
				st.Reason = asString(cm["reason"])
				st.Message = asString(cm["message"])
			}
		}
	}
	// Revision: Kustomization -> status.lastAppliedRevision; GitRepository -> status.artifact.revision.
	if revisionField != "" {
		if rev, found, _ := unstructured.NestedString(obj.Object, "status", revisionField); found {
			st.Revision = rev
		}
	} else {
		if rev, found, _ := unstructured.NestedString(obj.Object, "status", "artifact", "revision"); found {
			st.Revision = rev
		}
	}
	return st, nil
}

// RequestReconcile annotates the resource to trigger an on-demand Flux reconcile
// (a REAL mutation the Flux controllers act on).
func (c *FluxClient) RequestReconcile(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) error {
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, reconcileRequestAnnotation, time.Now().UTC().Format(time.RFC3339Nano))
	_, err := c.dyn.Resource(gvr).Namespace(namespace).Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("gitops: request reconcile %s/%s: %w", namespace, name, err)
	}
	return nil
}

// SyncKustomization triggers a reconcile, reads the resulting status, reports
// gitops.sync=real (Flux is a real backend), and emits a signed receipt of the
// observed reconcile state. This is the Flux analogue of Manager.SyncApplication.
func (c *FluxClient) SyncKustomization(ctx context.Context, namespace, name string) (*FluxStatus, error) {
	if err := c.RequestReconcile(ctx, kustomizationGVR, namespace, name); err != nil {
		return nil, err
	}
	st, err := c.KustomizationStatus(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	// Talking to the real Flux CRD API is a real GitOps backend.
	_ = capability.Report("gitops.sync", "flux", capability.ModeReal, "Flux Kustomization reconcile via CRD API")

	_, err = c.recorder.Record(ctx, evidence.RecordInput{
		Actor:   "gitops",
		Action:  "gitops.sync",
		Subject: namespace + "/" + name,
		Input:   map[string]any{"engine": "flux", "kind": "Kustomization", "namespace": namespace, "name": name},
		Output:  map[string]any{"ready": st.Ready, "revision": st.Revision, "reason": st.Reason},
		Payload: map[string]any{
			"engine":      "flux",
			"kind":        st.Kind,
			"namespace":   st.Namespace,
			"name":        st.Name,
			"ready":       st.Ready,
			"revision":    st.Revision,
			"reason":      st.Reason,
			"message":     st.Message,
			"suspended":   st.Suspended,
			"sync_real":   true,
			"observed_at": st.ObservedAt,
		},
		Backends: []evidence.BackendFact{{Component: "gitops.sync", Mode: "real", Driver: "flux"}},
	})
	if err != nil {
		c.logger.WithError(err).Warn("gitops: failed to emit flux sync evidence")
	}
	return st, nil
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
