package k8s

import "context"

// KubeClient defines the contract for Kubernetes cluster interactions.
// All subsystems that need K8s access depend on this interface rather than
// the concrete *Client, enabling mock implementations for unit testing.
type KubeClient interface {
	// Cluster information
	APIServer() string
	Healthy(ctx context.Context) bool
	Version(ctx context.Context) (string, error)

	// Node operations
	ListNodes(ctx context.Context) ([]Node, error)
	GetNode(ctx context.Context, name string) (*Node, error)
	GetNodeResources(ctx context.Context) ([]NodeResourceInfo, error)

	// Pod operations
	ListPods(ctx context.Context, namespace string) ([]Pod, error)
	CreatePod(ctx context.Context, namespace string, pod *Pod) (*Pod, error)
	DeletePod(ctx context.Context, namespace, name string) error
	BindPod(ctx context.Context, namespace, podName, nodeName string) error

	// Raw API access
	DoRawRequest(ctx context.Context, method, path string) ([]byte, int, error)
	DoRawRequestWithBody(ctx context.Context, method, path string, body []byte) ([]byte, int, error)
}

// Compile-time interface satisfaction check.
var _ KubeClient = (*Client)(nil)
