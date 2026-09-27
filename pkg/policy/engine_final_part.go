package policy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

// ============================================================================
// Scheme and Codec Setup - Required for K8s Integration
// ============================================================================

var scheme = runtime.NewScheme()

func init() {
	// Register common Kubernetes types
	corev1.AddToScheme(scheme)
}

// ============================================================================
// Additional Test Helpers for Benchmarks
// ============================================================================

type TestQuery struct {
	Query string
	Input map[string]interface{}
}

type AWSTestCase struct {
	Name        string
	Query       string
	Input       map[string]interface{}
	ExpectAllow bool
}

func (tg *TestDataGenerator) generatePolicies() {
	tg.policies = make([]*PolicyBundle, tg.policyCount)
	
	for i := 0; i < tg.policyCount; i++ {
		nsIndex := i % len(tg.namespaces)
		
		tg.policies[i] = &PolicyBundle{
			Name:      fmt.Sprintf("policy-%d", i),
			Version:   "1.0.0",
			Namespace: tg.namespaces[nsIndex],
			RegoFiles: []*PolicyFile{{
				Content: fmt.Sprintf(`package policy.%d
				
allowed := true`, i),
			}},
			Metadata: map[string]interface{}{
				"id":          i,
				"namespace":   tg.namespaces[nsIndex],
				"created_at":  time.Now().UnixNano(),
			},
		}
	}
}

func (tg *TestDataGenerator) generatePayloads() {
	tg.payloads = make([]interface{}, tg.requestCount)
	
	for i := 0; i < tg.requestCount; i++ {
		payload := map[string]interface{}{
			"request_id": i,
			"timestamp":  time.Now().UnixNano(),
			"action":     []string{"create", "update", "delete"}[i%3],
			"resource":   []string{"pod", "service", "configmap", "secret"}[i%4],
			"namespace":  tg.namespaces[i%len(tg.namespaces)],
			"user":       map[string]string{
				"name":   fmt.Sprintf("user-%d", i%100),
				"groups": []string{"developers", "operators"}[i%2],
			},
		}
		
		if i%10 == 0 {
			payload["labels"] = map[string]string{
				"env":   []string{"prod", "staging", "dev"}[i%3],
				"tier":  []string{"frontend", "backend", "database"}[i%3],
			}
		}
		
		tg.payloads[i] = payload
	}
}

func (tg *TestDataGenerator) generateQueries() {
	tg.queries = make([]string, tg.requestCount)
	
	baselineQueries := []string{
		"data.kubernetes.authz.allow",
		"data.policies.check_resource_restrictions",
		"data.security.validate_labels",
		"data.networking.allowed_ingress",
		"data.cost.resource_budget_compliant",
	}
	
	for i := 0; i < tg.requestCount && i < len(baselineQueries)*100; i++ {
		queryIndex := i % len(baselineQueries)
		
		nsIndex := i % len(tg.namespaces)
		tg.queries[i] = fmt.Sprintf("%s[%q]", baselineQueries[queryIndex], tg.namespaces[nsIndex])
	}
}

func generateSentinelCompatiblePolicies() []*TestQuery {
	policies := []*TestQuery{
		{
			Query: "data.kubernetes.authz.allow",
			Input: map[string]interface{}{
				"action":    "create",
				"resource":  "pod",
				"namespace": "default",
			},
		},
		{
			Query: "data.kubernetes.labels.valid",
			Input: map[string]interface{}{
				"labels": map[string]string{
					"app": "myapp",
					"env": "production",
				},
			},
		},
		{
			Query: "data.policies.cpu_limits_set",
			Input: map[string]interface{}{
				"container": map[string]interface{}{
					"name":  "web",
					"limits": map[string]interface{}{
						"cpu":    "500m",
						"memory": "256Mi",
					},
				},
			},
		},
		{
			Query: "data.aws.root_access_denied",
			Input: map[string]interface{}{
				"user":     "root",
				"action":   "*",
				"resource": "*",
			},
		},
	}
	
	return policies
}

func generateAWSSCPCompatibilityTests() []*AWSTestCase {
	tests := []*AWSTestCase{
		{
			Name: "DenyRootAccess",
			Query: `data.aws.root_access_denied`,
			Input: map[string]interface{}{
				"user":     "root",
				"action":   "*",
				"resource": "*",
			},
			ExpectAllow: false,
		},
		{
			Name: "AllowStandardOperations",
			Query: `data.aws.operations_permitted`,
			Input: map[string]interface{}{
				"user":     "developer",
				"actions":  []string{"ec2:DescribeInstances"},
				"resource": "arn:*:*",
			},
			ExpectAllow: true,
		},
	}
	
	return tests
}

// ============================================================================
// String and Array Utilities
// ============================================================================

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	
	return result
}

func hashString(input string) uint64 {
	var hash uint64 = 0x6c62272e07601743
	prime := uint64(0x100000001b3)
	
	for i := 0; i < len(input); i++ {
		hash ^= uint64(input[i])
		hash *= prime
	}
	
	return hash
}

func getCacheKey(entry *cachedEntry) string {
	if entryMap, ok := entry.value.(map[string]interface{}); ok {
		if id, exists := entryMap["id"]; exists {
			return id.(string)
		}
	}
	return ""
}

func getOldestKey() string {
	return "oldest_key_placeholder"
}

// ============================================================================
// Resource Context Extraction
// ============================================================================

func extractResourceContext(obj runtime.Object) ResourceContext {
	metaObj, ok := obj.(metav1.Object)
	if !ok {
		return ResourceContext{}
	}
	
	typedMeta := metaObj.GetObjectKind().GroupVersionKind()
	
	rc := ResourceContext{
		APIVersion: typedMeta.GroupVersion().String(),
		Kind:       typedMeta.Kind,
		Name:       metaObj.GetName(),
		Namespace:  metaObj.GetNamespace(),
		UID:        metaObj.GetUID(),
		Labels:     metaObj.GetLabels(),
		Annotations: metaObj.GetAnnotations(),
	}
	
	return rc
}

func extractSpec(obj runtime.Object) (interface{}, error) {
	// Attempt to extract spec field from object
	specField := ""
	
	switch v := obj.(type) {
	case *corev1.Pod:
		specField = fmt.Sprintf("%+v", v.Spec)
	case *corev1.Service:
		specField = fmt.Sprintf("%+v", v.Spec)
	default:
		return nil, fmt.Errorf("unsupported type %T", obj)
	}
	
	return specField, nil
}

// ============================================================================
// Event Type Detection
// ============================================================================

func determineEventType(change PolicyChange) string {
	switch change.Type {
	case ChangeCreate:
		return "add"
	case ChangeUpdate:
		return "update"
	case ChangeDelete:
		return "delete"
	case ChangeRefresh:
		return "refresh"
	default:
		return "unknown"
	}
}

func extractNamespace(path string) string {
	// Extract namespace from file path or return default
	parts := splitPath(path)
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return "default"
}

func splitPath(path string) []string {
	result := make([]string, 0)
	current := ""
	
	for _, char := range path {
		if char == '/' || char == '\\' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	
	if current != "" {
		result = append(result, current)
	}
	
	return result
}
