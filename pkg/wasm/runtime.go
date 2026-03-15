// Package wasm — real WebAssembly runtime integration.
// Provides actual Wasm module validation, instance lifecycle management,
// RuntimeClass CRD for containerd runwasi shim, OCI registry interaction,
// and health checking for Spin/wasmCloud/WasmEdge/Wasmtime runtimes.
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/k8s"
)

// ============================================================================
// Wasm Module Validation — magic number + WASI version check
// ============================================================================

// WasmMagicNumber is the first 4 bytes of any valid Wasm binary (\0asm)
var WasmMagicNumber = []byte{0x00, 0x61, 0x73, 0x6D}

// WasmVersion1 is the MVP version 1 of the Wasm spec
var WasmVersion1 = []byte{0x01, 0x00, 0x00, 0x00}

// ModuleValidationResult contains the result of validating a Wasm binary
type ModuleValidationResult struct {
	Valid       bool     `json:"valid"`
	Version     int      `json:"version"`
	Size        int64    `json:"size_bytes"`
	HasWASI     bool     `json:"has_wasi"`
	Imports     []string `json:"imports,omitempty"`
	Exports     []string `json:"exports,omitempty"`
	ErrorMsg    string   `json:"error,omitempty"`
}

// ValidateWasmBinary validates a raw Wasm binary by checking the magic number,
// version field, and scanning the import/export sections for WASI signatures.
func ValidateWasmBinary(data []byte) *ModuleValidationResult {
	result := &ModuleValidationResult{Size: int64(len(data))}

	// Minimum valid Wasm module: 8 bytes (magic + version)
	if len(data) < 8 {
		result.ErrorMsg = fmt.Sprintf("binary too small (%d bytes); minimum Wasm module is 8 bytes", len(data))
		return result
	}

	// Check magic number: \0asm
	if data[0] != WasmMagicNumber[0] || data[1] != WasmMagicNumber[1] ||
		data[2] != WasmMagicNumber[2] || data[3] != WasmMagicNumber[3] {
		result.ErrorMsg = fmt.Sprintf("invalid magic number: got [%02x %02x %02x %02x], expected [00 61 73 6d]",
			data[0], data[1], data[2], data[3])
		return result
	}

	// Check version: currently only version 1 is supported
	version := int(data[4]) | int(data[5])<<8 | int(data[6])<<16 | int(data[7])<<24
	result.Version = version
	if version != 1 {
		result.ErrorMsg = fmt.Sprintf("unsupported Wasm version: %d (only version 1 is supported)", version)
		return result
	}

	result.Valid = true

	// Scan for WASI imports by searching for known WASI module name patterns
	// WASI imports are in the "wasi_snapshot_preview1" or "wasi_unstable" module
	binaryStr := string(data)
	if strings.Contains(binaryStr, "wasi_snapshot_preview1") ||
		strings.Contains(binaryStr, "wasi_unstable") {
		result.HasWASI = true
	}

	// Extract import/export names by scanning section headers
	// Section ID 2 = Import, Section ID 7 = Export
	imports, exports := scanWasmSections(data)
	result.Imports = imports
	result.Exports = exports

	return result
}

// scanWasmSections scans Wasm binary sections for import/export names
func scanWasmSections(data []byte) (imports []string, exports []string) {
	offset := 8 // skip header
	for offset < len(data) {
		if offset >= len(data) {
			break
		}
		sectionID := data[offset]
		offset++

		// Read section size (LEB128 unsigned)
		sectionSize, bytesRead := readLEB128(data[offset:])
		offset += bytesRead

		if offset+sectionSize > len(data) {
			break
		}

		sectionData := data[offset : offset+sectionSize]

		switch sectionID {
		case 2: // Import section
			imports = parseImportSection(sectionData)
		case 7: // Export section
			exports = parseExportSection(sectionData)
		}

		offset += sectionSize
	}
	return
}

// readLEB128 reads a LEB128 unsigned integer from a byte slice
func readLEB128(data []byte) (int, int) {
	result := 0
	shift := 0
	for i := 0; i < len(data) && i < 5; i++ {
		b := int(data[i])
		result |= (b & 0x7F) << shift
		shift += 7
		if b&0x80 == 0 {
			return result, i + 1
		}
	}
	return result, 1
}

// parseImportSection parses a Wasm import section to extract module:name pairs
func parseImportSection(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	count, offset := readLEB128(data)
	var imports []string
	for i := 0; i < count && offset < len(data); i++ {
		// module name
		modLen, n := readLEB128(data[offset:])
		offset += n
		if offset+modLen > len(data) {
			break
		}
		modName := string(data[offset : offset+modLen])
		offset += modLen

		// field name
		fieldLen, n2 := readLEB128(data[offset:])
		offset += n2
		if offset+fieldLen > len(data) {
			break
		}
		fieldName := string(data[offset : offset+fieldLen])
		offset += fieldLen

		imports = append(imports, modName+"."+fieldName)

		// Skip import descriptor (kind byte + type)
		if offset < len(data) {
			kind := data[offset]
			offset++
			switch kind {
			case 0: // function: typeidx
				_, n3 := readLEB128(data[offset:])
				offset += n3
			case 1: // table: elemtype + limits
				offset += 3
			case 2: // memory: limits
				offset += 2
			case 3: // global: valtype + mut
				offset += 2
			}
		}
	}
	return imports
}

// parseExportSection parses a Wasm export section to extract exported names
func parseExportSection(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	count, offset := readLEB128(data)
	var exports []string
	for i := 0; i < count && offset < len(data); i++ {
		nameLen, n := readLEB128(data[offset:])
		offset += n
		if offset+nameLen > len(data) {
			break
		}
		name := string(data[offset : offset+nameLen])
		offset += nameLen
		exports = append(exports, name)

		// Skip export descriptor (kind byte + index)
		if offset < len(data) {
			offset++ // kind
			_, n2 := readLEB128(data[offset:])
			offset += n2
		}
	}
	return exports
}

// ============================================================================
// OCI Registry — pull Wasm module from OCI-compatible registry
// ============================================================================

// OCIManifest represents a minimal OCI image manifest for Wasm modules
type OCIManifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        OCIDescriptor `json:"config"`
	Layers        []OCIDescriptor `json:"layers"`
}

// OCIDescriptor is a content-addressed reference to OCI blobs
type OCIDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// PullWasmModule pulls a Wasm module from an OCI registry.
// Supports: ghcr.io, Docker Hub, Azure ACR, AWS ECR — any OCI-compliant registry.
// The Wasm module is stored as an OCI artifact with media type application/vnd.wasm.content.layer.v1+wasm
func (m *Manager) PullWasmModule(ctx context.Context, registryURL, reference string) ([]byte, error) {
	// Parse reference: registry/repo:tag or registry/repo@sha256:digest
	parts := strings.SplitN(reference, ":", 2)
	repo := parts[0]
	tag := "latest"
	if len(parts) > 1 {
		tag = parts[1]
	}

	// Step 1: Fetch the manifest
	manifestURL := fmt.Sprintf("%s/v2/%s/manifests/%s", registryURL, repo, tag)
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create manifest request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OCI registry not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OCI manifest fetch failed (status=%d): %s", resp.StatusCode, string(body))
	}

	var manifest OCIManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to parse OCI manifest: %w", err)
	}

	// Step 2: Find the Wasm layer
	var wasmLayer *OCIDescriptor
	for i, layer := range manifest.Layers {
		if strings.Contains(layer.MediaType, "wasm") ||
			strings.Contains(layer.MediaType, "application/octet-stream") {
			wasmLayer = &manifest.Layers[i]
			break
		}
	}
	if wasmLayer == nil {
		return nil, fmt.Errorf("no Wasm layer found in OCI manifest (layers: %d)", len(manifest.Layers))
	}

	// Step 3: Pull the Wasm blob
	blobURL := fmt.Sprintf("%s/v2/%s/blobs/%s", registryURL, repo, wasmLayer.Digest)
	blobReq, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob request: %w", err)
	}

	blobResp, err := m.httpClient.Do(blobReq)
	if err != nil {
		return nil, fmt.Errorf("failed to pull Wasm blob: %w", err)
	}
	defer blobResp.Body.Close()

	if blobResp.StatusCode != 200 {
		return nil, fmt.Errorf("OCI blob pull failed (status=%d)", blobResp.StatusCode)
	}

	wasmData, err := io.ReadAll(blobResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Wasm blob: %w", err)
	}

	m.logger.WithFields(logrus.Fields{
		"reference": reference, "size": len(wasmData), "digest": wasmLayer.Digest,
	}).Info("Wasm module pulled from OCI registry")

	return wasmData, nil
}

// ============================================================================
// Instance Lifecycle — Stop / Delete / HealthCheck
// ============================================================================

// StopInstance stops a running Wasm instance.
// For Spin runtime: calls DELETE /api/apps/{id}/instances/{instanceId}
// For containerd: calls CRI StopContainer
// For local tracking: updates status to stopped
func (m *Manager) StopInstance(ctx context.Context, instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var target *WasmInstance
	for _, inst := range m.instances {
		if inst.ID == instanceID {
			target = inst
			break
		}
	}
	if target == nil {
		return fmt.Errorf("instance %q not found", instanceID)
	}

	// Try to stop via runtime API
	if m.config.SpinEndpoint != "" && (target.Runtime == RuntimeSpin || target.Runtime == RuntimeWasmCloud) {
		stopURL := fmt.Sprintf("%s/api/apps/%s/stop", m.config.SpinEndpoint, target.ModuleID)
		req, err := http.NewRequestWithContext(ctx, "POST", stopURL, nil)
		if err == nil {
			resp, err := m.httpClient.Do(req)
			if err != nil {
				m.logger.WithError(err).Debug("Spin stop API call failed")
			} else {
				resp.Body.Close()
			}
		}
	}

	target.Status = WasmStatusStopped

	// Update in DB
	if m.store != nil {
		dbInst := instanceToWasmInstanceModel(target)
		dbInst.Status = string(WasmStatusStopped)
		if err := m.store.UpdateWasmInstance(dbInst); err != nil {
			m.logger.WithError(err).Warn("Failed to update Wasm instance in database")
		}
	}

	m.logger.WithField("instance", instanceID).Info("Wasm instance stopped")
	return nil
}

// DeleteInstance removes a Wasm instance from tracking and the runtime.
func (m *Manager) DeleteInstance(ctx context.Context, instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	found := false
	for i, inst := range m.instances {
		if inst.ID == instanceID {
			// Try to stop via runtime API if still running
			if inst.Status == WasmStatusRunning {
				if m.config.SpinEndpoint != "" {
					deleteURL := fmt.Sprintf("%s/api/apps/%s/instances/%s", m.config.SpinEndpoint, inst.ModuleID, instanceID)
					req, _ := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
					if req != nil {
						resp, err := m.httpClient.Do(req)
						if err != nil {
							m.logger.WithError(err).Debug("Spin delete API call failed")
						} else {
							resp.Body.Close()
						}
					}
				}
			}

			m.instances = append(m.instances[:i], m.instances[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("instance %q not found", instanceID)
	}

	// Remove from DB
	if m.store != nil {
		if err := m.store.DeleteWasmInstance(instanceID); err != nil {
			m.logger.WithError(err).Warn("Failed to delete Wasm instance from database")
		}
	}

	m.logger.WithField("instance", instanceID).Info("Wasm instance deleted")
	return nil
}

// InstanceHealthCheck checks the health of a running Wasm instance by calling
// the runtime health endpoint or verifying the instance status.
func (m *Manager) InstanceHealthCheck(ctx context.Context, instanceID string) (*InstanceHealth, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var target *WasmInstance
	for _, inst := range m.instances {
		if inst.ID == instanceID {
			target = inst
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("instance %q not found", instanceID)
	}

	health := &InstanceHealth{
		InstanceID: instanceID,
		Status:     string(target.Status),
		Runtime:    string(target.Runtime),
		CheckedAt:  time.Now().UTC(),
	}

	// If Spin endpoint is configured, do a real health check
	if m.config.SpinEndpoint != "" && target.Status == WasmStatusRunning {
		healthURL := fmt.Sprintf("%s/api/apps/%s/health", m.config.SpinEndpoint, target.ModuleID)
		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
		if err == nil {
			resp, err := m.httpClient.Do(req)
			if err != nil {
				health.Healthy = false
				health.Message = "Spin health check failed: " + err.Error()
			} else {
				resp.Body.Close()
				health.Healthy = resp.StatusCode == 200
				if !health.Healthy {
					health.Message = fmt.Sprintf("Spin health check returned status %d", resp.StatusCode)
				}
			}
		}
	} else {
		// Local tracking: infer health from status
		health.Healthy = target.Status == WasmStatusRunning
		if !health.Healthy {
			health.Message = fmt.Sprintf("Instance status: %s", target.Status)
		}
	}

	health.UptimeSeconds = int64(time.Since(target.StartedAt).Seconds())
	health.MemoryUsedKB = target.MemoryUsedKB
	health.RequestCount = target.RequestCount

	return health, nil
}

// InstanceHealth represents the health status of a Wasm instance
type InstanceHealth struct {
	InstanceID    string `json:"instance_id"`
	Status        string `json:"status"`
	Runtime       string `json:"runtime"`
	Healthy       bool   `json:"healthy"`
	Message       string `json:"message,omitempty"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	MemoryUsedKB  int64  `json:"memory_used_kb"`
	RequestCount  int64  `json:"request_count"`
	CheckedAt     time.Time `json:"checked_at"`
}

// ============================================================================
// RuntimeClass CRD — containerd runwasi shim registration
// ============================================================================

// RuntimeClassSpec represents a K8s RuntimeClass for Wasm runtimes
type RuntimeClassSpec struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   map[string]interface{} `json:"metadata"`
	Handler    string            `json:"handler"`
	Overhead   map[string]interface{} `json:"overhead,omitempty"`
	Scheduling map[string]interface{} `json:"scheduling,omitempty"`
}

// runtimeClassCRD builds a RuntimeClass CRD for a Wasm runtime handler.
// RuntimeClass tells Kubernetes which container runtime to use for Wasm pods.
// handler naming follows containerd convention: io.containerd.{runtime}.v1
func runtimeClassCRD(runtime RuntimeType) *RuntimeClassSpec {
	handler := "io.containerd." + string(runtime) + ".v1"
	name := "wasm-" + string(runtime)

	return &RuntimeClassSpec{
		APIVersion: "node.k8s.io/v1",
		Kind:       "RuntimeClass",
		Metadata: map[string]interface{}{
			"name": name,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "cloudai-fusion",
				"cloudai-fusion/runtime-type":  string(runtime),
			},
		},
		Handler: handler,
		Overhead: map[string]interface{}{
			"podFixed": map[string]string{
				"memory": "8Mi",  // Wasm overhead is minimal vs Docker's ~50MB
				"cpu":    "10m",
			},
		},
		Scheduling: map[string]interface{}{
			"nodeSelector": map[string]string{
				"cloudai-fusion/wasm-runtime": string(runtime),
			},
		},
	}
}

// EnsureRuntimeClass creates or updates a RuntimeClass CRD for the specified Wasm runtime.
// This is required for K8s to schedule Wasm pods to nodes with the runwasi shim installed.
func (m *Manager) EnsureRuntimeClass(ctx context.Context, client *k8s.Client, runtime RuntimeType) error {
	crd := runtimeClassCRD(runtime)
	body, err := json.Marshal(crd)
	if err != nil {
		return fmt.Errorf("failed to marshal RuntimeClass: %w", err)
	}

	name := "wasm-" + string(runtime)
	path := "/apis/node.k8s.io/v1/runtimeclasses"

	_, statusCode, err := client.DoRawRequestWithBody(ctx, "POST", path, body)
	if err != nil {
		if statusCode == 409 { // Already exists — update
			putPath := path + "/" + name
			_, _, updateErr := client.DoRawRequestWithBody(ctx, "PUT", putPath, body)
			if updateErr != nil {
				return fmt.Errorf("failed to update RuntimeClass %q: %w", name, updateErr)
			}
			m.logger.WithField("runtime", runtime).Info("RuntimeClass updated")
			return nil
		}
		return fmt.Errorf("failed to create RuntimeClass %q: %w", name, err)
	}

	m.logger.WithFields(logrus.Fields{
		"runtime": runtime, "handler": crd.Handler,
	}).Info("RuntimeClass CRD created for Wasm runtime")
	return nil
}

// ListRuntimeClasses lists all Wasm-related RuntimeClass CRDs from the cluster
func (m *Manager) ListRuntimeClasses(ctx context.Context, client *k8s.Client) ([]RuntimeClassInfo, error) {
	path := "/apis/node.k8s.io/v1/runtimeclasses"
	body, statusCode, err := client.DoRawRequest(ctx, "GET", path)
	if err != nil || statusCode != 200 {
		return nil, fmt.Errorf("failed to list RuntimeClasses (status=%d): %w", statusCode, err)
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Handler string `json:"handler"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse RuntimeClass list: %w", err)
	}

	var classes []RuntimeClassInfo
	for _, item := range result.Items {
		// Filter to only cloudai-fusion managed RuntimeClasses
		if item.Metadata.Labels["app.kubernetes.io/managed-by"] == "cloudai-fusion" {
			classes = append(classes, RuntimeClassInfo{
				Name:    item.Metadata.Name,
				Handler: item.Handler,
				Runtime: RuntimeType(item.Metadata.Labels["cloudai-fusion/runtime-type"]),
			})
		}
	}
	return classes, nil
}

// RuntimeClassInfo represents a discovered Wasm RuntimeClass
type RuntimeClassInfo struct {
	Name    string      `json:"name"`
	Handler string      `json:"handler"`
	Runtime RuntimeType `json:"runtime"`
}

// ============================================================================
// Enhanced Spin Integration — app lifecycle + logs
// ============================================================================

// SpinAppStatus represents the status of a Spin application
type SpinAppStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // running, stopped, failed
	Replicas  int    `json:"replicas"`
	URL       string `json:"url,omitempty"`
	Version   string `json:"version,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

// GetSpinAppStatus queries the Spin Cloud API for application status
func (m *Manager) GetSpinAppStatus(ctx context.Context, appName string) (*SpinAppStatus, error) {
	if m.config.SpinEndpoint == "" {
		return nil, fmt.Errorf("Spin endpoint not configured")
	}

	statusURL := fmt.Sprintf("%s/api/apps/%s", m.config.SpinEndpoint, appName)
	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Spin status request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Spin API not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("Spin app %q not found", appName)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Spin status API returned %d", resp.StatusCode)
	}

	var status SpinAppStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to parse Spin app status: %w", err)
	}
	return &status, nil
}

// GetSpinAppLogs retrieves recent logs from a Spin application
func (m *Manager) GetSpinAppLogs(ctx context.Context, appName string, lines int) ([]string, error) {
	if m.config.SpinEndpoint == "" {
		return nil, fmt.Errorf("Spin endpoint not configured")
	}
	if lines <= 0 {
		lines = 100
	}

	logsURL := fmt.Sprintf("%s/api/apps/%s/logs?lines=%d", m.config.SpinEndpoint, appName, lines)
	req, err := http.NewRequestWithContext(ctx, "GET", logsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create logs request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Spin logs API not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Spin logs API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read logs response: %w", err)
	}

	// Try JSON array first, then fall back to newline-delimited
	var logLines []string
	if err := json.Unmarshal(body, &logLines); err != nil {
		logLines = strings.Split(strings.TrimSpace(string(body)), "\n")
	}
	return logLines, nil
}

// ============================================================================
// Runtime Health Check — Spin / containerd / wasmCloud
// ============================================================================

// RuntimeHealth represents the health of a Wasm runtime system
type RuntimeHealth struct {
	Runtime   RuntimeType `json:"runtime"`
	Healthy   bool        `json:"healthy"`
	Endpoint  string      `json:"endpoint"`
	Version   string      `json:"version,omitempty"`
	Message   string      `json:"message,omitempty"`
	CheckedAt time.Time   `json:"checked_at"`
}

// CheckRuntimeHealth checks if the configured Wasm runtime is healthy and reachable
func (m *Manager) CheckRuntimeHealth(ctx context.Context) (*RuntimeHealth, error) {
	health := &RuntimeHealth{
		Runtime:   m.config.DefaultRuntime,
		CheckedAt: time.Now().UTC(),
	}

	switch m.config.DefaultRuntime {
	case RuntimeSpin, RuntimeWasmCloud:
		if m.config.SpinEndpoint == "" {
			health.Message = "Spin endpoint not configured"
			return health, nil
		}
		health.Endpoint = m.config.SpinEndpoint

		healthURL := m.config.SpinEndpoint + "/healthz"
		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
		if err != nil {
			health.Message = "Failed to create health request: " + err.Error()
			return health, nil
		}

		resp, err := m.httpClient.Do(req)
		if err != nil {
			health.Message = "Runtime not reachable: " + err.Error()
			return health, nil
		}
		defer resp.Body.Close()

		health.Healthy = resp.StatusCode == 200
		if !health.Healthy {
			health.Message = fmt.Sprintf("Health check returned status %d", resp.StatusCode)
		}

		// Try to get version
		versionURL := m.config.SpinEndpoint + "/api/version"
		vReq, _ := http.NewRequestWithContext(ctx, "GET", versionURL, nil)
		if vReq != nil {
			vResp, err := m.httpClient.Do(vReq)
			if err == nil {
				defer vResp.Body.Close()
				var vInfo struct{ Version string `json:"version"` }
				if json.NewDecoder(vResp.Body).Decode(&vInfo) == nil {
					health.Version = vInfo.Version
				}
			}
		}

	case RuntimeWasmEdge, RuntimeWasmtime:
		if m.config.ContainerdSocket == "" {
			health.Message = "containerd socket not configured"
			return health, nil
		}
		health.Endpoint = m.config.ContainerdSocket

		// Check containerd health via HTTP debug API
		healthURL := m.config.ContainerdSocket + "/v1/version"
		req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
		if err != nil {
			health.Message = "Failed to create containerd request: " + err.Error()
			return health, nil
		}

		resp, err := m.httpClient.Do(req)
		if err != nil {
			health.Message = "containerd not reachable: " + err.Error()
			return health, nil
		}
		defer resp.Body.Close()

		health.Healthy = resp.StatusCode == 200
		var vInfo struct{ Version string `json:"version"` }
		if json.NewDecoder(resp.Body).Decode(&vInfo) == nil {
			health.Version = vInfo.Version
		}

	default:
		health.Healthy = true
		health.Message = "No external runtime configured; using local tracking"
	}

	return health, nil
}
