// Package store - Extended repository methods for all domain entities.
// Provides GORM-based CRUD for vulnerability scans, mesh policies,
// Wasm modules/instances, edge nodes, and alert rules/events.
package store

import (
	"fmt"
	"time"
)

// ============================================================================
// Vulnerability Scan CRUD
// ============================================================================

// CreateVulnerabilityScan inserts a new scan record
func (s *Store) CreateVulnerabilityScan(scan *VulnerabilityScanModel) error {
	return s.db.Create(scan).Error
}

// GetVulnerabilityScanByID finds a scan by ID
func (s *Store) GetVulnerabilityScanByID(id string) (*VulnerabilityScanModel, error) {
	var scan VulnerabilityScanModel
	if err := s.db.Where("id = ?", id).First(&scan).Error; err != nil {
		return nil, err
	}
	return &scan, nil
}

// ListVulnerabilityScans returns scans for a cluster
func (s *Store) ListVulnerabilityScans(clusterID string, offset, limit int) ([]VulnerabilityScanModel, int64, error) {
	var scans []VulnerabilityScanModel
	var total int64
	q := s.db.Model(&VulnerabilityScanModel{})
	if clusterID != "" {
		q = q.Where("cluster_id = ?", clusterID)
	}
	q.Count(&total)
	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&scans).Error; err != nil {
		return nil, 0, err
	}
	return scans, total, nil
}

// UpdateVulnerabilityScan updates a scan record
func (s *Store) UpdateVulnerabilityScan(scan *VulnerabilityScanModel) error {
	return s.db.Save(scan).Error
}

// ============================================================================
// Mesh Policy CRUD
// ============================================================================

// CreateMeshPolicy inserts a new mesh network policy
func (s *Store) CreateMeshPolicy(p *MeshPolicyModel) error {
	return s.db.Create(p).Error
}

// GetMeshPolicyByID finds a mesh policy by ID
func (s *Store) GetMeshPolicyByID(id string) (*MeshPolicyModel, error) {
	var p MeshPolicyModel
	if err := s.db.Where("id = ?", id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// ListMeshPolicies returns all active mesh policies
func (s *Store) ListMeshPolicies(offset, limit int) ([]MeshPolicyModel, int64, error) {
	var policies []MeshPolicyModel
	var total int64
	s.db.Model(&MeshPolicyModel{}).Count(&total)
	if err := s.db.Offset(offset).Limit(limit).Order("created_at DESC").Find(&policies).Error; err != nil {
		return nil, 0, err
	}
	return policies, total, nil
}

// DeleteMeshPolicy soft-deletes a mesh policy
func (s *Store) DeleteMeshPolicy(id string) error {
	return s.db.Where("id = ?", id).Delete(&MeshPolicyModel{}).Error
}

// ============================================================================
// Wasm Module CRUD
// ============================================================================

// CreateWasmModule inserts a new Wasm module
func (s *Store) CreateWasmModule(m *WasmModuleModel) error {
	return s.db.Create(m).Error
}

// GetWasmModuleByID finds a Wasm module by ID
func (s *Store) GetWasmModuleByID(id string) (*WasmModuleModel, error) {
	var m WasmModuleModel
	if err := s.db.Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListWasmModules returns all Wasm modules
func (s *Store) ListWasmModules(offset, limit int) ([]WasmModuleModel, int64, error) {
	var modules []WasmModuleModel
	var total int64
	s.db.Model(&WasmModuleModel{}).Count(&total)
	if err := s.db.Offset(offset).Limit(limit).Order("created_at DESC").Find(&modules).Error; err != nil {
		return nil, 0, err
	}
	return modules, total, nil
}

// DeleteWasmModule soft-deletes a Wasm module
func (s *Store) DeleteWasmModule(id string) error {
	return s.db.Where("id = ?", id).Delete(&WasmModuleModel{}).Error
}

// ============================================================================
// Wasm Instance CRUD
// ============================================================================

// CreateWasmInstance inserts a new Wasm instance
func (s *Store) CreateWasmInstance(inst *WasmInstanceModel) error {
	return s.db.Create(inst).Error
}

// ListWasmInstances returns Wasm instances with optional filter
func (s *Store) ListWasmInstances(status string, offset, limit int) ([]WasmInstanceModel, int64, error) {
	var instances []WasmInstanceModel
	var total int64
	q := s.db.Model(&WasmInstanceModel{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	q.Count(&total)
	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&instances).Error; err != nil {
		return nil, 0, err
	}
	return instances, total, nil
}

// UpdateWasmInstance updates a Wasm instance
func (s *Store) UpdateWasmInstance(inst *WasmInstanceModel) error {
	return s.db.Save(inst).Error
}

// DeleteWasmInstance soft-deletes a Wasm instance
func (s *Store) DeleteWasmInstance(id string) error {
	return s.db.Where("id = ?", id).Delete(&WasmInstanceModel{}).Error
}

// ============================================================================
// Edge Node CRUD
// ============================================================================

// CreateEdgeNode inserts a new edge node
func (s *Store) CreateEdgeNode(node *EdgeNodeModel) error {
	return s.db.Create(node).Error
}

// GetEdgeNodeByID finds an edge node by ID
func (s *Store) GetEdgeNodeByID(id string) (*EdgeNodeModel, error) {
	var node EdgeNodeModel
	if err := s.db.Where("id = ?", id).First(&node).Error; err != nil {
		return nil, err
	}
	return &node, nil
}

// ListEdgeNodes returns edge nodes with optional tier filter
func (s *Store) ListEdgeNodes(tier string, offset, limit int) ([]EdgeNodeModel, int64, error) {
	var nodes []EdgeNodeModel
	var total int64
	q := s.db.Model(&EdgeNodeModel{})
	if tier != "" {
		q = q.Where("tier = ?", tier)
	}
	q.Count(&total)
	if err := q.Offset(offset).Limit(limit).Order("created_at DESC").Find(&nodes).Error; err != nil {
		return nil, 0, err
	}
	return nodes, total, nil
}

// UpdateEdgeNode updates an edge node
func (s *Store) UpdateEdgeNode(node *EdgeNodeModel) error {
	return s.db.Save(node).Error
}

// DeleteEdgeNode soft-deletes an edge node
func (s *Store) DeleteEdgeNode(id string) error {
	return s.db.Where("id = ?", id).Delete(&EdgeNodeModel{}).Error
}

// UpdateEdgeNodeHeartbeat updates heartbeat timestamp and resource usage
func (s *Store) UpdateEdgeNodeHeartbeat(id string, status string, powerWatts float64, resourceUsage string) error {
	return s.db.Model(&EdgeNodeModel{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":              status,
		"current_power_watts": powerWatts,
		"resource_usage":      resourceUsage,
		"last_heartbeat_at":   time.Now().UTC(),
		"updated_at":          time.Now().UTC(),
	}).Error
}

// ============================================================================
// Alert Rule CRUD
// ============================================================================

// CreateAlertRule inserts a new alert rule
func (s *Store) CreateAlertRule(rule *AlertRuleModel) error {
	return s.db.Create(rule).Error
}

// GetAlertRuleByID finds an alert rule by ID
func (s *Store) GetAlertRuleByID(id string) (*AlertRuleModel, error) {
	var rule AlertRuleModel
	if err := s.db.Where("id = ?", id).First(&rule).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

// ListAlertRules returns all alert rules
func (s *Store) ListAlertRules(offset, limit int) ([]AlertRuleModel, int64, error) {
	var rules []AlertRuleModel
	var total int64
	s.db.Model(&AlertRuleModel{}).Count(&total)
	if err := s.db.Offset(offset).Limit(limit).Order("created_at DESC").Find(&rules).Error; err != nil {
		return nil, 0, err
	}
	return rules, total, nil
}

// UpdateAlertRule updates an alert rule
func (s *Store) UpdateAlertRule(rule *AlertRuleModel) error {
	return s.db.Save(rule).Error
}

// DeleteAlertRule soft-deletes an alert rule
func (s *Store) DeleteAlertRule(id string) error {
	return s.db.Where("id = ?", id).Delete(&AlertRuleModel{}).Error
}

// ============================================================================
// Alert Event CRUD
// ============================================================================

// CreateAlertEvent inserts a new alert event
func (s *Store) CreateAlertEvent(event *AlertEventModel) error {
	return s.db.Create(event).Error
}

// ListAlertEvents returns recent alert events
func (s *Store) ListAlertEvents(limit int) ([]AlertEventModel, error) {
	var events []AlertEventModel
	if err := s.db.Order("fired_at DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// UpdateAlertEventStatus updates an alert event status (e.g., firing -> resolved)
func (s *Store) UpdateAlertEventStatus(id, status string) error {
	updates := map[string]interface{}{"status": status}
	if status == "resolved" {
		now := time.Now().UTC()
		updates["resolved_at"] = &now
	}
	return s.db.Model(&AlertEventModel{}).Where("id = ?", id).Updates(updates).Error
}

// ============================================================================
// Upsert Security Policy (extend existing CRUD with update)
// ============================================================================

// UpdateSecurityPolicy updates a security policy
func (s *Store) UpdateSecurityPolicy(p *SecurityPolicyModel) error {
	return s.db.Save(p).Error
}

// ============================================================================
// Cluster: update status helper
// ============================================================================

// UpdateClusterStatus updates cluster status and health-check timestamp
func (s *Store) UpdateClusterStatus(id, status string, nodeCount, gpuCount int) error {
	return s.db.Model(&ClusterModel{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":          status,
		"node_count":      nodeCount,
		"gpu_count":       gpuCount,
		"health_check_at": time.Now().UTC(),
		"updated_at":      time.Now().UTC(),
	}).Error
}

// CountClusters returns total cluster count (non-deleted)
func (s *Store) CountClusters() int64 {
	var count int64
	s.db.Model(&ClusterModel{}).Count(&count)
	return count
}

// ============================================================================
// Utility: GenerateUUID (avoids circular import with common)
// ============================================================================

// GenerateUUID creates a v4-style UUID
func GenerateUUID() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		time.Now().UnixNano()&0xFFFFFFFF,
		time.Now().UnixNano()>>32&0xFFFF,
		0x4000|time.Now().UnixNano()>>48&0x0FFF,
		0x8000|time.Now().UnixNano()>>60&0x3FFF,
		time.Now().UnixNano()&0xFFFFFFFFFFFF)
}
