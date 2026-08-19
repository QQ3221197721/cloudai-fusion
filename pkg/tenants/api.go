// Package tenants - api.go wraps pkg/scheduler GPUSharingManager operations
// behind tenant-aware APIs (Module 11 Phase 2).
package tenants

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// Manager orchestrates tenant pool lifecycle on top of GPUSharingManager.
type Manager struct {
	store  *Store
	gpuMgr *scheduler.GPUSharingManager
	ledger *evidence.Ledger
	logger *logrus.Entry

	// mu guards last and LastAttestationHash; attest is called while store.mu
	// is held, so mu is always innermost.
	mu   sync.Mutex
	last *evidence.Evidence

	// AttestationEnabled reports whether a real evidence ledger is wired
	// (Phase 2: true iff a non-nil ledger was injected).
	AttestationEnabled bool
	// LastAttestationHash is the content hash of the most recent signed receipt
	// (empty when disabled or no write happened yet). Kept for CLI backward compat.
	LastAttestationHash string
}

// NewManager creates a tenant Manager with an FS store rooted at storePath
// (default "./.caf" when empty) and NO attestation ledger (degraded mode).
func NewManager(storePath string, gpuMgr *scheduler.GPUSharingManager) (*Manager, error) {
	return NewManagerWithLedger(storePath, gpuMgr, nil)
}

// NewManagerWithLedger creates a tenant Manager with an optional evidence
// ledger. A nil ledger disables attestation (all other behavior unchanged,
// mirroring pkg/elasticpool semantics); a real ledger signs every write
// operation under actor DefaultTenantActor.
func NewManagerWithLedger(storePath string, gpuMgr *scheduler.GPUSharingManager, ledger *evidence.Ledger) (*Manager, error) {
	if storePath == "" {
		storePath = "./.caf"
	}
	s := NewStore(storePath)
	if err := s.Load(); err != nil {
		return nil, err
	}
	return &Manager{
		store:              s,
		gpuMgr:             gpuMgr,
		ledger:             ledger,
		logger:             logrus.WithField("component", "tenants.manager"),
		AttestationEnabled: ledger != nil,
	}, nil
}

// DefaultTenantActor is the attestation actor for all tenant write operations.
const DefaultTenantActor = "cafctl-tenant"

// attest writes one receipt through the evidence ledger. With a nil ledger it
// is a no-op (degraded mode); otherwise the receipt is Ed25519-signed and
// hash-chained, and becomes the Manager's LastAttestation.
func (m *Manager) attest(ctx context.Context, action, subject string, input, output, payload map[string]any) error {
	if m.ledger == nil {
		return nil
	}
	ev, err := m.ledger.Record(ctx, evidence.RecordInput{
		Actor:   DefaultTenantActor,
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: payload,
	})
	if err != nil {
		return fmt.Errorf("tenants: attestation %s failed: %w", action, err)
	}
	m.mu.Lock()
	m.last = ev
	m.LastAttestationHash = ev.Hash
	m.mu.Unlock()
	return nil
}

// LastAttestation returns the receipt of the most recent attested write, or
// nil when attestation is disabled (nil ledger) or no write happened yet.
func (m *Manager) LastAttestation() *evidence.Evidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.last
}
func validatePoolInput(input PoolInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("pool name is required")
	}
	if len(input.GPUIndices) == 0 {
		return fmt.Errorf("at least one GPU index is required")
	}
	for i, idx := range input.GPUIndices {
		if idx < 0 {
			return fmt.Errorf("gpu index %d invalid at position %d", idx, i)
		}
	}
	if input.Mode != PoolModeMIG && input.Mode != PoolModeMPS {
		return fmt.Errorf("mode must be %q or %q, got %q", PoolModeMIG, PoolModeMPS, input.Mode)
	}
	return nil
}

// CreatePool creates a tenant pool. For MIG mode it enables MIG and pre-creates
// slices per profile limits via GPUSharingManager. Note: on hosts without
// nvidia-smi/hardware MIG support, EnableMIG will fail — use --mode mps with a
// stub nvidia-smi or accept the error in dev environments.
func (m *Manager) CreatePool(ctx context.Context, input PoolInput) (*TenantPool, error) {
	if err := validatePoolInput(input); err != nil {
		return nil, err
	}
	if input.Mode == PoolModeMIG && input.MigProfile == "" {
		input.MigProfile = "1g.5gb"
	}

	now := time.Now().UTC()
	pool := &TenantPool{
		ID:          common.NewUUID(),
		Name:        input.Name,
		GPUType:     input.GPUType,
		MigProfile:  input.MigProfile,
		Mode:        input.Mode,
		NodeIndex:   input.NodeIndex,
		GPUIndices:  append([]int(nil), input.GPUIndices...),
		Status:      statusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
		Members:     make([]TenantMember, 0),
		TotalSlices: input.TotalSlices,
	}

	if input.Mode == PoolModeMIG {
		if pool.TotalSlices == 0 {
			// derive default capacity from profile max instances per GPU
			perGPU := 0
			for _, p := range scheduler.SupportedMIGProfiles(strings.ToLower(input.GPUType)) {
				if p.Name == input.MigProfile {
					perGPU = p.MaxInstances
					break
				}
			}
			if perGPU == 0 {
				perGPU = 7 // safest universal max for 1g profiles
			}
			pool.TotalSlices = perGPU * len(pool.GPUIndices)
		}

		for _, gpuIdx := range pool.GPUIndices {
			if err := m.gpuMgr.EnableMIG(ctx, gpuIdx); err != nil {
				return nil, fmt.Errorf("enable MIG on GPU %d: %w (hint: requires nvidia-smi and MIG-capable hardware)", gpuIdx, err)
			}
		}
	} else if input.Mode == PoolModeMPS && pool.TotalSlices == 0 {
		pool.TotalSlices = len(pool.GPUIndices)
	}

	m.store.mu.Lock()
	m.store.pools[pool.ID] = pool
	err := m.store.saveLocked()
	m.store.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("persist pool: %w", err)
	}

	if err := m.attest(ctx, "tenant.pool.create", pool.ID,
		map[string]any{"name": input.Name, "gpu_type": input.GPUType, "mode": string(input.Mode), "gpu_indices": pool.GPUIndices, "mig_profile": pool.MigProfile, "total_slices": pool.TotalSlices},
		map[string]any{"pool_id": pool.ID, "status": pool.Status},
		map[string]any{"created_at": pool.CreatedAt.Format(time.RFC3339)}); err != nil {
		return nil, err
	}

	m.logger.WithFields(logrus.Fields{
		"pool_id": pool.ID, "name": pool.Name, "mode": pool.Mode, "gpus": pool.GPUIndices,
	}).Info("tenant pool created")
	return pool, nil
}

// GetPool retrieves a specific pool by ID.
func (m *Manager) GetPool(poolID string) (*TenantPool, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()
	pool, ok := m.store.pools[poolID]
	if !ok {
		return nil, fmt.Errorf("pool %q not found", poolID)
	}
	cp := *pool
	cp.Members = append([]TenantMember(nil), pool.Members...)
	return &cp, nil
}

// ListPools returns all pools (optionally filtered; pass zero values to list all).
func (m *Manager) ListPools() []*TenantPool {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()
	result := make([]*TenantPool, 0, len(m.store.pools))
	for _, p := range m.store.pools {
		result = append(result, p)
	}
	return result
}

// ListMembers returns all tenant members of a pool.
func (m *Manager) ListMembers(poolID string) ([]TenantMember, error) {
	pool, err := m.GetPool(poolID)
	if err != nil {
		return nil, err
	}
	return pool.Members, nil
}

// usedSlices counts allocated MIG slices across all members.
func usedSlices(pool *TenantPool) int {
	n := 0
	for i := range pool.Members {
		n += len(pool.Members[i].MIGSlices)
	}
	return n
}

// findMember returns index of tenant in pool, or -1.
func findMember(pool *TenantPool, tenantID string) int {
	for i := range pool.Members {
		if pool.Members[i].ID == tenantID {
			return i
		}
	}
	return -1
}

// AddTenant adds a tenant member to an existing pool and allocates resources.
func (m *Manager) AddTenant(ctx context.Context, poolID string, input MemberInput) (*TenantMember, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("tenant name is required")
	}
	if input.ResourceMode == "" {
		if input.Slices > 0 {
			input.ResourceMode = ResourceModeMIGSlice
		} else {
			input.ResourceMode = ResourceModeMPSShare
		}
	}
	if input.Slices < 0 {
		return nil, fmt.Errorf("slices must be >= 0, got %d", input.Slices)
	}

	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	pool, ok := m.store.pools[poolID]
	if !ok {
		return nil, fmt.Errorf("pool %q not found", poolID)
	}
	// Guard: tenants can join pools that are pending (provisioning) or active.
	if pool.Status == statusSuspended || pool.Status == statusDeleted {
		return nil, fmt.Errorf("pool %q is %q; tenants can only join pending or active pools", poolID, pool.Status)
	}
	if pool.Mode == PoolModeMIG {
		if input.Slices <= 0 {
			return nil, fmt.Errorf("MIG pool requires at least 1 slice (--slices)")
		}
		free := pool.TotalSlices - usedSlices(pool)
		if input.Slices > free {
			return nil, fmt.Errorf("insufficient slices: requested %d, free %d of %d", input.Slices, free, pool.TotalSlices)
		}
	}

	now := time.Now().UTC()
	member := TenantMember{
		ID:           common.NewUUID(),
		PoolID:       poolID,
		Name:         input.Name,
		UID:          input.UID,
		Status:       TenantStatusActive,
		ResourceMode: input.ResourceMode,
		MIGSlices:    make([]MIGSlice, 0),
		MaxClients:   input.MaxClients,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if pool.Mode == PoolModeMIG {
		for allocated := 0; allocated < input.Slices; {
			placed := false
			for _, gpuIdx := range pool.GPUIndices {
				inst, err := m.gpuMgr.CreateMIGInstance(ctx, gpuIdx, pool.MigProfile)
				if err != nil {
					continue // try next GPU
				}
				member.MIGSlices = append(member.MIGSlices, MIGSlice{
					ID:            common.NewUUID(),
					MIGInstanceID: inst.ID,
					GPUIndex:      gpuIdx,
					Profile:       pool.MigProfile,
					MemoryMB:      inst.MemoryMB,
					InUse:         true,
				})
				placed = true
				allocated++
				if allocated >= input.Slices {
					break
				}
			}
			if !placed {
				return nil, fmt.Errorf("could only allocate %d of %d MIG slices (nvidia-smi/MIG unavailable)", allocated, input.Slices)
			}
		}
	}

	pool.Members = append(pool.Members, member)
	pool.UpdatedAt = now
	if err := m.store.saveLocked(); err != nil {
		return nil, fmt.Errorf("persist tenant: %w", err)
	}

	if err := m.attest(ctx, "tenant.add", member.ID,
		map[string]any{"pool_id": poolID, "name": input.Name, "resource_mode": input.ResourceMode, "slices": len(member.MIGSlices), "max_clients": input.MaxClients},
		map[string]any{"tenant_id": member.ID, "status": string(member.Status)},
		map[string]any{"mig_slices": len(member.MIGSlices)}); err != nil {
		return nil, err
	}

	m.logger.WithFields(logrus.Fields{
		"pool": poolID, "tenant": member.ID, "slices": len(member.MIGSlices),
	}).Info("tenant added to pool")
	return &member, nil
}

// AllocateToTenant allocates additional slices to an existing tenant.
func (m *Manager) AllocateToTenant(ctx context.Context, poolID, tenantID string, additionalSlices int) (*TenantMember, error) {
	if additionalSlices <= 0 {
		return nil, fmt.Errorf("additional slices must be positive, got %d", additionalSlices)
	}

	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	pool, ok := m.store.pools[poolID]
	if !ok {
		return nil, fmt.Errorf("pool %q not found", poolID)
	}
	// Guard: allocation requires an ACTIVE pool (no capacity expansion on suspended pools).
	if pool.Status != statusActive {
		return nil, fmt.Errorf("pool %q is %q; allocation requires an active pool", poolID, pool.Status)
	}
	idx := findMember(pool, tenantID)
	if idx < 0 {
		return nil, fmt.Errorf("tenant %q not found in pool %q", tenantID, poolID)
	}
	member := &pool.Members[idx]

	free := pool.TotalSlices - usedSlices(pool)
	if additionalSlices > free {
		return nil, fmt.Errorf("insufficient slices: requested %d more, free %d of %d", additionalSlices, free, pool.TotalSlices)
	}

	if pool.Mode == PoolModeMIG {
		for allocated := 0; allocated < additionalSlices; {
			placed := false
			for _, gpuIdx := range pool.GPUIndices {
				inst, err := m.gpuMgr.CreateMIGInstance(ctx, gpuIdx, pool.MigProfile)
				if err != nil {
					continue
				}
				member.MIGSlices = append(member.MIGSlices, MIGSlice{
					ID:            common.NewUUID(),
					MIGInstanceID: inst.ID,
					GPUIndex:      gpuIdx,
					Profile:       pool.MigProfile,
					MemoryMB:      inst.MemoryMB,
					InUse:         true,
				})
				placed = true
				allocated++
				if allocated >= additionalSlices {
					break
				}
			}
			if !placed {
				return nil, fmt.Errorf("could only allocate %d of %d additional MIG slices", allocated, additionalSlices)
			}
		}
	} else {
		// MPS pool: slices map to client capacity
		member.MaxClients += additionalSlices
	}

	member.UpdatedAt = time.Now().UTC()
	pool.UpdatedAt = member.UpdatedAt
	if err := m.store.saveLocked(); err != nil {
		return nil, fmt.Errorf("persist allocation: %w", err)
	}

	if err := m.attest(ctx, "tenant.allocate", tenantID,
		map[string]any{"pool_id": poolID, "additional_slices": additionalSlices},
		map[string]any{"tenant_id": tenantID, "status": string(pool.Members[idx].Status)},
		map[string]any{"mig_slices_after": len(pool.Members[idx].MIGSlices), "max_clients_after": pool.Members[idx].MaxClients}); err != nil {
		return nil, err
	}

	m.logger.WithFields(logrus.Fields{
		"pool": poolID, "tenant": tenantID, "additional": additionalSlices,
	}).Info("allocated additional slices")
	return member, nil
}

// RemoveTenant removes a tenant from a pool and destroys its MIG instances.
// The member transitions through the FSM to "deleted" before removal.
func (m *Manager) RemoveTenant(ctx context.Context, poolID, tenantID string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	pool, ok := m.store.pools[poolID]
	if !ok {
		return fmt.Errorf("pool %q not found", poolID)
	}
	// Guard: cannot operate on members of a deleted pool.
	if pool.Status == statusDeleted {
		return fmt.Errorf("pool %q is deleted; no further operations allowed", poolID)
	}
	idx := findMember(pool, tenantID)
	if idx < 0 {
		return fmt.Errorf("tenant %q not found in pool %q", tenantID, poolID)
	}
	member := &pool.Members[idx]

	// Validate transition to "deleted" before destroying hardware.
	from := string(member.Status)
	if err := validateLifecycleTransition("tenant", tenantID, from, statusDeleted); err != nil {
		return err
	}

	destroyed := 0
	if pool.Mode == PoolModeMIG {
		for _, slice := range member.MIGSlices {
			if err := m.gpuMgr.DestroyMIGInstance(ctx, slice.GPUIndex, slice.MIGInstanceID); err != nil {
				// Log but continue: bookkeeping removal must proceed even if
				// hardware instance already gone (e.g. after host reboot).
				m.logger.WithFields(logrus.Fields{
					"gpu": slice.GPUIndex, "instance": slice.MIGInstanceID,
				}).WithError(err).Warn("failed to destroy MIG instance during tenant removal")
			}
			destroyed++
		}
		member.Status = TenantStatus(statusDeleted)
	}

	pool.Members = append(pool.Members[:idx], pool.Members[idx+1:]...)
	pool.UpdatedAt = time.Now().UTC()
	if err := m.store.saveLocked(); err != nil {
		return fmt.Errorf("persist removal: %w", err)
	}

	if err := m.attest(ctx, "tenant.remove", tenantID,
		map[string]any{"pool_id": poolID, "from_status": from},
		map[string]any{"removed": true},
		map[string]any{"mig_instances_destroyed": destroyed}); err != nil {
		return err
	}

	m.logger.WithFields(logrus.Fields{
		"pool": poolID, "tenant": tenantID,
	}).Info("tenant removed from pool")
	return nil
}
