package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// ============================================================================
// Helper: build a Client with fake clientset
// ============================================================================

func newFakeClient(objects ...runtime.Object) *Client {
	fakeCS := fake.NewSimpleClientset(objects...)
	return &Client{
		clientset: fakeCS,
		apiServer: "https://fake-k8s:6443",
	}
}

func makeNode(name string, gpus int, ready bool) *corev1.Node {
	conditions := []corev1.NodeCondition{}
	if ready {
		conditions = append(conditions, corev1.NodeCondition{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionTrue,
		})
	}

	cap := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("64"),
		corev1.ResourceMemory: resource.MustParse("256Gi"),
	}
	alloc := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("60"),
		corev1.ResourceMemory: resource.MustParse("240Gi"),
	}
	if gpus > 0 {
		cap[corev1.ResourceName("nvidia.com/gpu")] = *resource.NewQuantity(int64(gpus), resource.DecimalSI)
		alloc[corev1.ResourceName("nvidia.com/gpu")] = *resource.NewQuantity(int64(gpus), resource.DecimalSI)
	}

	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"nvidia.com/gpu.product": "A100-SXM-80GB"},
		},
		Spec: corev1.NodeSpec{
			PodCIDR:    "10.244.0.0/24",
			ProviderID: "aws:///us-east-1a/i-abc123",
		},
		Status: corev1.NodeStatus{
			Conditions: conditions,
			Capacity:   cap,
			Allocatable: alloc,
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
			},
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion:          "v1.30.0",
				OSImage:                 "Ubuntu 22.04",
				KernelVersion:           "5.15.0",
				ContainerRuntimeVersion: "containerd://1.7.0",
				Architecture:            "amd64",
				OperatingSystem:         "linux",
			},
		},
	}
}

func makePod(ns, name, nodeName string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{Name: "main", Image: "pytorch:latest"},
			},
		},
		Status: corev1.PodStatus{
			Phase:  phase,
			HostIP: "10.0.0.1",
			PodIP:  "10.244.0.5",
		},
	}
}

// ============================================================================
// Node Operation Tests (table-driven)
// ============================================================================

func TestListNodes(t *testing.T) {
	client := newFakeClient(
		makeNode("gpu-node-01", 8, true),
		makeNode("gpu-node-02", 4, true),
		makeNode("gpu-node-03", 0, false),
	)

	nodes, err := client.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}

	// Verify domain type conversion
	n := nodes[0]
	if n.Metadata.Name == "" {
		t.Error("node name should not be empty")
	}
	if n.Status.Capacity == nil {
		t.Error("node capacity should not be nil")
	}
}

func TestListNodes_Empty(t *testing.T) {
	client := newFakeClient()
	nodes, err := client.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestGetNode(t *testing.T) {
	client := newFakeClient(makeNode("test-node", 4, true))

	node, err := client.GetNode(context.Background(), "test-node")
	if err != nil {
		t.Fatalf("GetNode failed: %v", err)
	}
	if node.Metadata.Name != "test-node" {
		t.Errorf("expected 'test-node', got %q", node.Metadata.Name)
	}
	if node.Spec.PodCIDR != "10.244.0.0/24" {
		t.Errorf("PodCIDR mismatch: %s", node.Spec.PodCIDR)
	}
}

func TestGetNode_NotFound(t *testing.T) {
	client := newFakeClient()
	_, err := client.GetNode(context.Background(), "nonexistent")
	if err == nil {
		t.Error("GetNode should fail for nonexistent node")
	}
}

func TestGetNodeResources(t *testing.T) {
	client := newFakeClient(
		makeNode("gpu-node-01", 8, true),
		makeNode("cpu-node-01", 0, true),
		makeNode("unhealthy-node", 4, false),
	)

	resources, err := client.GetNodeResources(context.Background())
	if err != nil {
		t.Fatalf("GetNodeResources failed: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 resources, got %d", len(resources))
	}

	// Find the GPU node
	var gpuNode *NodeResourceInfo
	for i := range resources {
		if resources[i].Name == "gpu-node-01" {
			gpuNode = &resources[i]
			break
		}
	}
	if gpuNode == nil {
		t.Fatal("gpu-node-01 not found in results")
	}
	if !gpuNode.Ready {
		t.Error("gpu-node-01 should be ready")
	}
	if gpuNode.GPUCapacity != "8" {
		t.Errorf("expected GPU capacity '8', got %q", gpuNode.GPUCapacity)
	}
	if gpuNode.InternalIP != "10.0.0.1" {
		t.Errorf("expected internal IP '10.0.0.1', got %q", gpuNode.InternalIP)
	}

	// Find unhealthy node
	for _, r := range resources {
		if r.Name == "unhealthy-node" {
			if r.Ready {
				t.Error("unhealthy-node should not be ready")
			}
		}
	}
}

// ============================================================================
// Pod Operation Tests
// ============================================================================

func TestListPods(t *testing.T) {
	client := newFakeClient(
		makePod("default", "pod-1", "node-1", corev1.PodRunning),
		makePod("default", "pod-2", "node-1", corev1.PodPending),
		makePod("kube-system", "coredns", "node-1", corev1.PodRunning),
	)

	tests := []struct {
		name      string
		namespace string
		wantCount int
	}{
		{"all namespaces", "", 3},
		{"default namespace", "default", 2},
		{"kube-system", "kube-system", 1},
		{"empty namespace", "nonexistent", 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pods, err := client.ListPods(context.Background(), tc.namespace)
			if err != nil {
				t.Fatalf("ListPods failed: %v", err)
			}
			if len(pods) != tc.wantCount {
				t.Errorf("expected %d pods, got %d", tc.wantCount, len(pods))
			}
		})
	}
}

func TestCreatePod(t *testing.T) {
	client := newFakeClient()
	pod := &Pod{
		Metadata: ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: PodSpec{
			Containers: []Container{
				{Name: "main", Image: "pytorch:2.0"},
			},
		},
	}

	created, err := client.CreatePod(context.Background(), "default", pod)
	if err != nil {
		t.Fatalf("CreatePod failed: %v", err)
	}
	if created.Metadata.Name != "test-pod" {
		t.Errorf("expected name 'test-pod', got %q", created.Metadata.Name)
	}

	// Verify it can be listed
	pods, err := client.ListPods(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListPods failed: %v", err)
	}
	if len(pods) != 1 {
		t.Errorf("expected 1 pod, got %d", len(pods))
	}
}

func TestDeletePod(t *testing.T) {
	client := newFakeClient(
		makePod("default", "to-delete", "node-1", corev1.PodRunning),
	)

	err := client.DeletePod(context.Background(), "default", "to-delete")
	if err != nil {
		t.Fatalf("DeletePod failed: %v", err)
	}

	pods, _ := client.ListPods(context.Background(), "default")
	if len(pods) != 0 {
		t.Errorf("pod should be deleted, got %d pods", len(pods))
	}
}

func TestDeletePod_NotFound(t *testing.T) {
	client := newFakeClient()
	err := client.DeletePod(context.Background(), "default", "nonexistent")
	if err == nil {
		t.Error("DeletePod should fail for nonexistent pod")
	}
}

func TestBindPod(t *testing.T) {
	pod := makePod("default", "unscheduled-pod", "", corev1.PodPending)
	client := newFakeClient(pod)

	// Add reactor to track Bind calls (fake clientset supports Bind via reactor)
	bindCalled := false
	client.clientset.(*fake.Clientset).PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "binding" {
			bindCalled = true
			return true, nil, nil
		}
		return false, nil, nil
	})

	err := client.BindPod(context.Background(), "default", "unscheduled-pod", "gpu-node-01")
	if err != nil {
		t.Fatalf("BindPod failed: %v", err)
	}
	if !bindCalled {
		t.Error("Bind reactor should have been called")
	}
}

// ============================================================================
// Health & Version Tests
// ============================================================================

func TestAPIServer(t *testing.T) {
	client := newFakeClient()
	if client.APIServer() != "https://fake-k8s:6443" {
		t.Errorf("unexpected API server: %s", client.APIServer())
	}
}

func TestClientset(t *testing.T) {
	client := newFakeClient()
	if client.Clientset() == nil {
		t.Error("Clientset should not be nil")
	}
}

// ============================================================================
// Converter Tests (table-driven)
// ============================================================================

func TestConvertNode(t *testing.T) {
	k8sNode := makeNode("converter-test", 4, true)
	node := convertNode(k8sNode)

	if node.Metadata.Name != "converter-test" {
		t.Errorf("name mismatch: %s", node.Metadata.Name)
	}
	if node.Status.NodeInfo.KubeletVersion != "v1.30.0" {
		t.Errorf("kubelet version: %s", node.Status.NodeInfo.KubeletVersion)
	}
	if node.Status.NodeInfo.Architecture != "amd64" {
		t.Errorf("architecture: %s", node.Status.NodeInfo.Architecture)
	}
	if node.Spec.ProviderID != "aws:///us-east-1a/i-abc123" {
		t.Errorf("provider ID: %s", node.Spec.ProviderID)
	}

	// Check conditions
	foundReady := false
	for _, c := range node.Status.Conditions {
		if c.Type == "Ready" && c.Status == "True" {
			foundReady = true
		}
	}
	if !foundReady {
		t.Error("should have Ready=True condition")
	}

	// Check addresses
	foundIP := false
	for _, a := range node.Status.Addresses {
		if a.Type == "InternalIP" && a.Address == "10.0.0.1" {
			foundIP = true
		}
	}
	if !foundIP {
		t.Error("should have InternalIP address")
	}
}

func TestConvertPod(t *testing.T) {
	k8sPod := makePod("test-ns", "test-pod", "node-1", corev1.PodRunning)
	pod := convertPod(k8sPod)

	if pod.Metadata.Name != "test-pod" {
		t.Errorf("name: %s", pod.Metadata.Name)
	}
	if pod.Metadata.Namespace != "test-ns" {
		t.Errorf("namespace: %s", pod.Metadata.Namespace)
	}
	if pod.Spec.NodeName != "node-1" {
		t.Errorf("nodeName: %s", pod.Spec.NodeName)
	}
	if pod.Status.Phase != "Running" {
		t.Errorf("phase: %s", pod.Status.Phase)
	}
	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(pod.Spec.Containers))
	}
	if pod.Spec.Containers[0].Image != "pytorch:latest" {
		t.Errorf("image: %s", pod.Spec.Containers[0].Image)
	}
}

func TestConvertToK8sPod(t *testing.T) {
	pod := &Pod{
		Metadata: ObjectMeta{
			Name:      "roundtrip",
			Namespace: "ml",
			Labels:    map[string]string{"app": "trainer"},
		},
		Spec: PodSpec{
			NodeName: "gpu-node",
			Containers: []Container{
				{
					Name:    "train",
					Image:   "pytorch:2.0",
					Command: []string{"python", "train.py"},
					Resources: ResourceRequirements{
						Requests: map[string]string{"nvidia.com/gpu": "2", "cpu": "4"},
						Limits:   map[string]string{"nvidia.com/gpu": "2", "memory": "16Gi"},
					},
				},
			},
		},
	}

	k8sPod := convertToK8sPod(pod)
	if k8sPod.Name != "roundtrip" {
		t.Errorf("name: %s", k8sPod.Name)
	}
	if k8sPod.Namespace != "ml" {
		t.Errorf("namespace: %s", k8sPod.Namespace)
	}
	if k8sPod.Labels["app"] != "trainer" {
		t.Error("label missing")
	}
	if len(k8sPod.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container")
	}
	c := k8sPod.Spec.Containers[0]
	if c.Name != "train" || c.Image != "pytorch:2.0" {
		t.Errorf("container: %s / %s", c.Name, c.Image)
	}
	gpuReq := c.Resources.Requests[corev1.ResourceName("nvidia.com/gpu")]
	if gpuReq.String() != "2" {
		t.Errorf("GPU request: %s", gpuReq.String())
	}
}

// ============================================================================
// Utility Tests (table-driven)
// ============================================================================

func TestResourceListToStringMap(t *testing.T) {
	tests := []struct {
		name string
		rl   corev1.ResourceList
		want int // expected map size
	}{
		{"nil", nil, 0},
		{"empty", corev1.ResourceList{}, 0},
		{"with resources", corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		}, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := resourceListToStringMap(tc.rl)
			if tc.want == 0 {
				if m != nil && len(m) > 0 {
					t.Errorf("expected nil/empty map, got %v", m)
				}
			} else if len(m) != tc.want {
				t.Errorf("expected %d entries, got %d", tc.want, len(m))
			}
		})
	}
}

func TestParseResourceList(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]string
		want  int
	}{
		{"nil", nil, 0},
		{"empty", map[string]string{}, 0},
		{"valid", map[string]string{"cpu": "4", "memory": "16Gi"}, 2},
		{"with invalid", map[string]string{"cpu": "4", "bad": "not-a-quantity"}, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl := parseResourceList(tc.input)
			if tc.want == 0 {
				if rl != nil && len(rl) > 0 {
					t.Errorf("expected nil/empty, got %v", rl)
				}
			} else if len(rl) != tc.want {
				t.Errorf("expected %d resources, got %d", tc.want, len(rl))
			}
		})
	}
}

// ============================================================================
// NewClient Validation Tests
// ============================================================================

func TestNewClient_MissingAPIServer(t *testing.T) {
	_, err := NewClient(ClientConfig{APIServer: ""})
	if err == nil {
		t.Error("NewClient should fail without API server")
	}
}
