// Package k8s provides a Kubernetes client for CloudAI Fusion.
// Built on top of k8s.io/client-go (the official Kubernetes Go client library),
// it exposes a streamlined API surface using domain-specific types while
// internally delegating all cluster communication to the official clientset.
//
// Supports three initialization modes:
//   - NewClient:                Bearer-token + API server URL (service accounts)
//   - NewClientFromKubeconfig:  kubeconfig file content (multi-cluster, CI/CD)
//   - NewClientFromInCluster:   Auto-detected pod service account (production)
package k8s

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ============================================================================
// Client
// ============================================================================

// ClientConfig holds configuration for the K8s client
type ClientConfig struct {
	// APIServer is the K8s API server URL (e.g., https://10.0.0.1:6443)
	APIServer string
	// BearerToken is the service account or user token
	BearerToken string
	// CACert is the PEM-encoded CA certificate (optional, skips TLS verify if empty)
	CACert string
	// Insecure skips TLS certificate verification
	Insecure bool
}

// Client wraps the official k8s.io/client-go clientset and provides
// CloudAI Fusion's domain-specific API.
type Client struct {
	clientset  kubernetes.Interface
	restConfig *rest.Config
	httpClient *http.Client // cached transport for raw requests
	apiServer  string
}

// NewClient creates a K8s client from explicit credentials (bearer token + API server).
// This is the typical mode for service-account based authentication.
func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.APIServer == "" {
		return nil, fmt.Errorf("k8s API server address is required")
	}

	restCfg := &rest.Config{
		Host:        cfg.APIServer,
		BearerToken: cfg.BearerToken,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: cfg.Insecure || cfg.CACert == "",
			CAData:   []byte(cfg.CACert),
		},
		Timeout: 30 * time.Second,
	}

	return buildClient(restCfg, cfg.APIServer)
}

// NewClientFromKubeconfig creates a K8s client from raw kubeconfig YAML content.
// Supports multi-cluster management and CI/CD pipelines.
func NewClientFromKubeconfig(kubeconfig string) (*Client, error) {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}
	restCfg.Timeout = 30 * time.Second
	return buildClient(restCfg, restCfg.Host)
}

// NewClientFromInCluster creates a K8s client using the pod's service account.
// This is the recommended mode for production deployments inside a K8s cluster.
func NewClientFromInCluster() (*Client, error) {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}
	restCfg.Timeout = 30 * time.Second
	return buildClient(restCfg, restCfg.Host)
}

// buildClient is the internal constructor shared by all NewClient* functions.
func buildClient(restCfg *rest.Config, apiServer string) (*Client, error) {
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s clientset: %w", err)
	}

	// Create a cached HTTP client with the same TLS/auth for raw requests
	transport, err := rest.TransportFor(restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create K8s transport: %w", err)
	}

	return &Client{
		clientset:  clientset,
		restConfig: restCfg,
		httpClient: &http.Client{Transport: transport, Timeout: 30 * time.Second},
		apiServer:  strings.TrimRight(apiServer, "/"),
	}, nil
}

// APIServer returns the K8s API server URL
func (c *Client) APIServer() string { return c.apiServer }

// Clientset returns the underlying kubernetes.Interface for advanced usage.
// Callers can use this to access any K8s API group directly.
func (c *Client) Clientset() kubernetes.Interface { return c.clientset }

// DoRawRequest performs an authenticated HTTP request to an arbitrary K8s API path.
// Used by compliance checks and CRD queries that don't have typed client methods.
func (c *Client) DoRawRequest(ctx context.Context, method, path string) ([]byte, int, error) {
	url := c.apiServer + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("K8s API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}
	return body, resp.StatusCode, nil
}

// DoRawRequestWithBody performs an authenticated HTTP request with a JSON body.
// Used for CRD create/update (POST/PUT) operations on custom K8s API paths
// such as CiliumNetworkPolicy, Istio PeerAuthentication, RuntimeClass, etc.
func (c *Client) DoRawRequestWithBody(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	url := c.apiServer + path
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("K8s API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}
	return respBody, resp.StatusCode, nil
}

// ============================================================================
// K8s API Types (domain-specific, stable API surface)
// ============================================================================

// NodeList represents the K8s /api/v1/nodes response
type NodeList struct {
	Items []Node `json:"items"`
}

// Node represents a K8s node
type Node struct {
	Metadata ObjectMeta `json:"metadata"`
	Status   NodeStatus `json:"status"`
	Spec     NodeSpec   `json:"spec"`
}

// ObjectMeta contains K8s object metadata
type ObjectMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	UID         string            `json:"uid,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// NodeSpec holds node spec
type NodeSpec struct {
	PodCIDR    string  `json:"podCIDR,omitempty"`
	ProviderID string  `json:"providerID,omitempty"`
	Taints     []Taint `json:"taints,omitempty"`
}

// Taint represents a K8s taint
type Taint struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

// NodeStatus holds node status
type NodeStatus struct {
	Conditions  []NodeCondition   `json:"conditions,omitempty"`
	Addresses   []NodeAddress     `json:"addresses,omitempty"`
	Capacity    map[string]string `json:"capacity,omitempty"`
	Allocatable map[string]string `json:"allocatable,omitempty"`
	NodeInfo    NodeSystemInfo    `json:"nodeInfo,omitempty"`
}

// NodeCondition represents a K8s node condition
type NodeCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// NodeAddress holds a node's address
type NodeAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

// NodeSystemInfo holds system info for a node
type NodeSystemInfo struct {
	MachineID               string `json:"machineID"`
	SystemUUID              string `json:"systemUUID"`
	BootID                  string `json:"bootID"`
	KernelVersion           string `json:"kernelVersion"`
	OSImage                 string `json:"osImage"`
	ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
	KubeletVersion          string `json:"kubeletVersion"`
	Architecture            string `json:"architecture"`
	OperatingSystem         string `json:"operatingSystem"`
}

// PodList represents the K8s /api/v1/pods response
type PodList struct {
	Items []Pod `json:"items"`
}

// Pod represents a K8s pod
type Pod struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     PodSpec    `json:"spec"`
	Status   PodStatus  `json:"status"`
}

// PodSpec holds pod spec
type PodSpec struct {
	NodeName   string      `json:"nodeName,omitempty"`
	Containers []Container `json:"containers,omitempty"`
}

// Container represents a K8s container
type Container struct {
	Name      string               `json:"name"`
	Image     string               `json:"image"`
	Command   []string             `json:"command,omitempty"`
	Resources ResourceRequirements `json:"resources,omitempty"`
}

// ResourceRequirements holds resource requests/limits
type ResourceRequirements struct {
	Requests map[string]string `json:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"`
}

// PodStatus holds pod status
type PodStatus struct {
	Phase  string `json:"phase"`
	HostIP string `json:"hostIP,omitempty"`
	PodIP  string `json:"podIP,omitempty"`
}

// Binding represents a K8s pod binding (for scheduler)
type Binding struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Metadata   ObjectMeta `json:"metadata"`
	Target     Target     `json:"target"`
}

// Target specifies the binding target
type Target struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// NodeResourceInfo holds parsed resource information for a node
type NodeResourceInfo struct {
	Name           string
	Ready          bool
	CPUCapacity    string
	CPUAllocatable string
	MemCapacity    string
	MemAllocatable string
	GPUCapacity    string
	GPUAllocatable string
	InternalIP     string
	OSImage        string
	KernelVersion  string
	ContainerRT    string
	Labels         map[string]string
}

// ============================================================================
// Node Operations (via client-go clientset.CoreV1().Nodes())
// ============================================================================

// ListNodes returns all nodes from the K8s cluster
func (c *Client) ListNodes(ctx context.Context) ([]Node, error) {
	nodeList, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	nodes := make([]Node, 0, len(nodeList.Items))
	for i := range nodeList.Items {
		nodes = append(nodes, convertNode(&nodeList.Items[i]))
	}
	return nodes, nil
}

// GetNode returns a specific node by name
func (c *Client) GetNode(ctx context.Context, name string) (*Node, error) {
	k8sNode, err := c.clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %q: %w", name, err)
	}

	node := convertNode(k8sNode)
	return &node, nil
}

// GetNodeResources returns parsed resource info for all nodes
func (c *Client) GetNodeResources(ctx context.Context) ([]NodeResourceInfo, error) {
	nodes, err := c.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]NodeResourceInfo, 0, len(nodes))
	for _, n := range nodes {
		info := NodeResourceInfo{
			Name:           n.Metadata.Name,
			CPUCapacity:    n.Status.Capacity["cpu"],
			CPUAllocatable: n.Status.Allocatable["cpu"],
			MemCapacity:    n.Status.Capacity["memory"],
			MemAllocatable: n.Status.Allocatable["memory"],
			GPUCapacity:    n.Status.Capacity["nvidia.com/gpu"],
			GPUAllocatable: n.Status.Allocatable["nvidia.com/gpu"],
			OSImage:        n.Status.NodeInfo.OSImage,
			KernelVersion:  n.Status.NodeInfo.KernelVersion,
			ContainerRT:    n.Status.NodeInfo.ContainerRuntimeVersion,
			Labels:         n.Metadata.Labels,
		}

		for _, cond := range n.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				info.Ready = true
				break
			}
		}
		for _, addr := range n.Status.Addresses {
			if addr.Type == "InternalIP" {
				info.InternalIP = addr.Address
				break
			}
		}
		result = append(result, info)
	}
	return result, nil
}

// ============================================================================
// Pod Operations (via client-go clientset.CoreV1().Pods())
// ============================================================================

// ListPods lists pods in a namespace. Pass "" for all namespaces.
func (c *Client) ListPods(ctx context.Context, namespace string) ([]Pod, error) {
	var podList *corev1.PodList
	var err error

	if namespace == "" {
		podList, err = c.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	} else {
		podList, err = c.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	pods := make([]Pod, 0, len(podList.Items))
	for i := range podList.Items {
		pods = append(pods, convertPod(&podList.Items[i]))
	}
	return pods, nil
}

// CreatePod creates a new pod in the specified namespace
func (c *Client) CreatePod(ctx context.Context, namespace string, pod *Pod) (*Pod, error) {
	k8sPod := convertToK8sPod(pod)

	created, err := c.clientset.CoreV1().Pods(namespace).Create(ctx, k8sPod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	result := convertPod(created)
	return &result, nil
}

// BindPod binds a pod to a specific node (core scheduling operation).
// Uses the official Binding subresource via client-go.
func (c *Client) BindPod(ctx context.Context, namespace, podName, nodeName string) error {
	binding := &corev1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Target: corev1.ObjectReference{
			Kind:       "Node",
			Name:       nodeName,
			APIVersion: "v1",
		},
	}

	err := c.clientset.CoreV1().Pods(namespace).Bind(ctx, binding, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to bind pod %q to node %q: %w", podName, nodeName, err)
	}
	return nil
}

// DeletePod deletes a pod
func (c *Client) DeletePod(ctx context.Context, namespace, name string) error {
	err := c.clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete pod %q: %w", name, err)
	}
	return nil
}

// ============================================================================
// Health Check & Cluster Info
// ============================================================================

// Healthy returns true if the K8s API server is responsive
func (c *Client) Healthy(ctx context.Context) bool {
	body, err := c.clientset.Discovery().RESTClient().Get().AbsPath("/healthz").DoRaw(ctx)
	return err == nil && string(body) == "ok"
}

// Version returns the K8s server version (e.g., "v1.29.2")
func (c *Client) Version(ctx context.Context) (string, error) {
	info, err := c.clientset.Discovery().ServerVersion()
	if err != nil {
		return "", fmt.Errorf("failed to get server version: %w", err)
	}
	return info.GitVersion, nil
}

// ============================================================================
// corev1 -> domain type converters (anti-corruption layer)
// ============================================================================

// convertNode converts an official corev1.Node to our domain Node type
func convertNode(n *corev1.Node) Node {
	node := Node{
		Metadata: ObjectMeta{
			Name:        n.Name,
			Namespace:   n.Namespace,
			UID:         string(n.UID),
			Labels:      n.Labels,
			Annotations: n.Annotations,
		},
		Spec: NodeSpec{
			PodCIDR:    n.Spec.PodCIDR,
			ProviderID: n.Spec.ProviderID,
		},
		Status: NodeStatus{
			Capacity:    resourceListToStringMap(n.Status.Capacity),
			Allocatable: resourceListToStringMap(n.Status.Allocatable),
			NodeInfo: NodeSystemInfo{
				MachineID:               n.Status.NodeInfo.MachineID,
				SystemUUID:              n.Status.NodeInfo.SystemUUID,
				BootID:                  n.Status.NodeInfo.BootID,
				KernelVersion:           n.Status.NodeInfo.KernelVersion,
				OSImage:                 n.Status.NodeInfo.OSImage,
				ContainerRuntimeVersion: n.Status.NodeInfo.ContainerRuntimeVersion,
				KubeletVersion:          n.Status.NodeInfo.KubeletVersion,
				Architecture:            n.Status.NodeInfo.Architecture,
				OperatingSystem:         n.Status.NodeInfo.OperatingSystem,
			},
		},
	}

	for _, t := range n.Spec.Taints {
		node.Spec.Taints = append(node.Spec.Taints, Taint{
			Key: t.Key, Value: t.Value, Effect: string(t.Effect),
		})
	}
	for _, cond := range n.Status.Conditions {
		node.Status.Conditions = append(node.Status.Conditions, NodeCondition{
			Type: string(cond.Type), Status: string(cond.Status),
			Reason: cond.Reason, Message: cond.Message,
		})
	}
	for _, addr := range n.Status.Addresses {
		node.Status.Addresses = append(node.Status.Addresses, NodeAddress{
			Type: string(addr.Type), Address: addr.Address,
		})
	}
	return node
}

// convertPod converts an official corev1.Pod to our domain Pod type
func convertPod(p *corev1.Pod) Pod {
	pod := Pod{
		Metadata: ObjectMeta{
			Name:        p.Name,
			Namespace:   p.Namespace,
			UID:         string(p.UID),
			Labels:      p.Labels,
			Annotations: p.Annotations,
		},
		Spec: PodSpec{
			NodeName: p.Spec.NodeName,
		},
		Status: PodStatus{
			Phase:  string(p.Status.Phase),
			HostIP: p.Status.HostIP,
			PodIP:  p.Status.PodIP,
		},
	}

	for _, c := range p.Spec.Containers {
		pod.Spec.Containers = append(pod.Spec.Containers, Container{
			Name:    c.Name,
			Image:   c.Image,
			Command: c.Command,
			Resources: ResourceRequirements{
				Requests: resourceListToStringMap(c.Resources.Requests),
				Limits:   resourceListToStringMap(c.Resources.Limits),
			},
		})
	}
	return pod
}

// convertToK8sPod converts our domain Pod type back to an official corev1.Pod
func convertToK8sPod(p *Pod) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        p.Metadata.Name,
			Namespace:   p.Metadata.Namespace,
			Labels:      p.Metadata.Labels,
			Annotations: p.Metadata.Annotations,
		},
		Spec: corev1.PodSpec{
			NodeName: p.Spec.NodeName,
		},
	}

	for _, c := range p.Spec.Containers {
		container := corev1.Container{
			Name:    c.Name,
			Image:   c.Image,
			Command: c.Command,
		}
		if len(c.Resources.Requests) > 0 {
			container.Resources.Requests = parseResourceList(c.Resources.Requests)
		}
		if len(c.Resources.Limits) > 0 {
			container.Resources.Limits = parseResourceList(c.Resources.Limits)
		}
		pod.Spec.Containers = append(pod.Spec.Containers, container)
	}
	return pod
}

// ============================================================================
// Utility helpers
// ============================================================================

// resourceListToStringMap converts corev1.ResourceList to map[string]string
func resourceListToStringMap(rl corev1.ResourceList) map[string]string {
	if len(rl) == 0 {
		return nil
	}
	m := make(map[string]string, len(rl))
	for k, v := range rl {
		m[string(k)] = v.String()
	}
	return m
}

// parseResourceList converts map[string]string back to corev1.ResourceList
func parseResourceList(m map[string]string) corev1.ResourceList {
	if len(m) == 0 {
		return nil
	}
	rl := make(corev1.ResourceList, len(m))
	for k, v := range m {
		q, err := resource.ParseQuantity(v)
		if err != nil {
			continue
		}
		rl[corev1.ResourceName(k)] = q
	}
	return rl
}
