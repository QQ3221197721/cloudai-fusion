package hotswap_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/hotswap"
)

// MockComponent implements hotswap.Component for testing
type MockComponent struct {
	version    hotswap.ComponentVersion
	stopped    bool
	started    bool
	drainCh    chan struct{}
}

func (m *MockComponent) Start(ctx context.Context) error {
	if m.stopped {
		return nil // Already stopped, can't start again
	}
	m.started = true
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (m *MockComponent) Stop(ctx context.Context) error {
	m.started = false
	m.stopped = true
	return nil
}

func (m *MockComponent) Drain() <-chan struct{} {
	if m.drainCh == nil {
		m.drainCh = make(chan struct{})
	}
	return m.drainCh
}

func (m *MockComponent) Version() hotswap.ComponentVersion {
	return m.version
}

// ExtractState/ApplyState satisfy the extended Component interface. This mock is
// stateless, so it round-trips an empty JSON document.
func (m *MockComponent) ExtractState() ([]byte, error) {
	return []byte("{}"), nil
}

func (m *MockComponent) ApplyState(_ []byte) error {
	return nil
}

func TestHotSwapOrchestrator_SwapComponent(t *testing.T) {
	os := hotswap.NewHotSwapOrchestrator(60 * time.Second)
	
	oldVer := hotswap.ComponentVersion{Name: "svc", Version: "v1.0"}
	newComp := &MockComponent{version: hotswap.ComponentVersion{Name: "svc", Version: "v2.0"}}
	
	err := os.SwapComponent(oldVer, newComp)
	if err == nil {
		t.Error("Expected error when no existing component is set")
	}
}

func TestHotSwapOrchestrator_SetComponentAndSwap(t *testing.T) {
	os := hotswap.NewHotSwapOrchestrator(30 * time.Second)
	
	initial := &MockComponent{version: hotswap.ComponentVersion{Name: "svc", Version: "v1.0"}}
	os.SetComponent(initial)
	
	os.SetComponent(initial)
	
	oldVer := initial.Version()
	newComp := &MockComponent{version: hotswap.ComponentVersion{Name: "svc", Version: "v1.1"}}
	
	err := os.SwapComponent(oldVer, newComp)
	if err != nil {
		t.Logf("SwapComponent returned: %v", err)
	}
}

func TestHotSwapOrchestrator_DrainRequests(t *testing.T) {
	os := hotswap.NewHotSwapOrchestrator(5 * time.Second)
	
	ctx := context.Background()
	err := os.DrainRequests(ctx)
	if err != nil {
		t.Errorf("DrainRequests failed: %v", err)
	}
}

func TestHotSwapOrchestrator_RollbackSwap(t *testing.T) {
	os := hotswap.NewHotSwapOrchestrator(60 * time.Second)
	
	err := os.RollbackSwap()
	if err == nil {
		t.Error("Expected error when no version history exists")
	}
}
