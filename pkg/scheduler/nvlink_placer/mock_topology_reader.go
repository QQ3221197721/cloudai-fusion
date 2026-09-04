package nvlink_placer

// MockTopologyReader implements TopologyReader interface for testing without real GPU hardware
type MockTopologyReader struct {
	links []NVLinkConnection
}

// GetNVLinkConnections returns mocked NVLink connections
func (m *MockTopologyReader) GetNVLinkConnections() []NVLinkConnection {
	return m.links
}

// GetNUMAPolicy returns -1 for all GPUs (no NUMA affinity in mock)
func (m *MockTopologyReader) GetNUMAPolicy(int) (int, error) {
	return -1, nil
}

// HasNVSwitch returns false for mock (no NVSwitch fabric)
func (m *MockTopologyReader) HasNVSwitch() bool {
	return false
}
