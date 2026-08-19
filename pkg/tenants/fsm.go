// Package tenants - fsm.go implements the Phase 2 lifecycle state machine for
// tenant pools and tenant members (Module 11).
//
// Status flow (identical for TenantPool.Status and TenantMember.Status):
//
//	pending → active ⇄ suspended
//	             \       /
//	              deleted (terminal)
//
// Central semantics (deliberate choices, enforced and tested):
//   - pending → active requires an explicit Activate; pools are CREATED as
//     "pending" so every activation is a conscious operator action.
//   - pending → deleted is REJECTED: activate the pool before deleting it, so
//     the attested history always shows a deliberate activation before any
//     deletion.
//   - suspended → deleted is ALLOWED: cleaning up a suspended pool without a
//     forced resume round-trip is operationally sane; the transition (and its
//     receipt) still records that the pool was suspended when deleted.
//   - deleted is terminal: no transitions out; revival is rejected with an
//     error that names the terminal state.
//
// Write guards layered on top of the FSM (enforced in api.go):
//   - AddTenant:      pool must be pending or active (registration may happen
//                     during provisioning; suspended/deleted pools admit nobody).
//   - AllocateToTenant: pool must be active (capacity only grows on live pools).
//   - RemoveTenant:   pool must not be deleted; the member itself must reach
//                     "deleted" through this FSM (active|suspended → deleted).
//   - DeletePool:     every member must individually reach "deleted" through
//                     the same FSM, then the pool transitions to terminal.
package tenants

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Canonical lifecycle statuses, shared by pools and members. Values equal the
// TenantStatus constants (kept for backward compatibility).
const (
	statusPending   = string(TenantStatusPending)
	statusActive    = string(TenantStatusActive)
	statusSuspended = string(TenantStatusSuspended)
	statusDeleted   = string(TenantStatusDeleted)
)

// lifecycleTransitions is the single source of truth for legal status
// transitions. An absent entry means "not allowed".
var lifecycleTransitions = map[string]map[string]bool{
	statusPending:   {statusActive: true},
	statusActive:    {statusSuspended: true, statusDeleted: true},
	statusSuspended: {statusActive: true, statusDeleted: true},
	statusDeleted:   {}, // terminal — nothing leaves "deleted"
}

// nextLifecycleStatuses returns the sorted set of statuses reachable from
// `from`; used to build actionable error messages.
func nextLifecycleStatuses(from string) []string {
	next := make([]string, 0, len(lifecycleTransitions[from]))
	for s := range lifecycleTransitions[from] {
		next = append(next, s)
	}
	sort.Strings(next)
	return next
}

// validateLifecycleTransition is the central FSM guard for BOTH pools and
// members. kind is "pool" or "tenant" (error context only). An illegal
// transition returns an error that lists the allowed next statuses, e.g.:
//
//	tenants: invalid pool lifecycle transition for "p1":
//	pending -> suspended (allowed next statuses from pending: active)
func validateLifecycleTransition(kind, id, from, to string) error {
	if from == to {
		return fmt.Errorf("tenants: %s %q is already %s; no transition to apply", kind, id, to)
	}
	allowed, known := lifecycleTransitions[from]
	if !known {
		return fmt.Errorf("tenants: unknown %s status %q for %q", kind, from, id)
	}
	if !allowed[to] {
		hint := "none — deleted is terminal"
		if next := nextLifecycleStatuses(from); len(next) > 0 {
			hint = strings.Join(next, ", ")
		}
		return fmt.Errorf("tenants: invalid %s lifecycle transition for %q: %s -> %s (allowed next statuses from %s: %s)",
			kind, id, from, to, from, hint)
	}
	return nil
}

// ----------------------------------------------------------------------------
// Pool lifecycle
// ----------------------------------------------------------------------------

// ActivatePool transitions a pending pool to active (pending → active).
// Writes attestation action "tenant.pool.activate".
func (m *Manager) ActivatePool(ctx context.Context, poolID string) (*TenantPool, error) {
	return m.setPoolStatus(ctx, poolID, statusActive, "tenant.pool.activate")
}

// SuspendPool transitions an active pool to suspended (active → suspended).
// Writes attestation action "tenant.pool.suspend".
func (m *Manager) SuspendPool(ctx context.Context, poolID string) (*TenantPool, error) {
	return m.setPoolStatus(ctx, poolID, statusSuspended, "tenant.pool.suspend")
}

// ResumePool transitions a suspended pool back to active (suspended → active).
// Writes attestation action "tenant.pool.resume".
func (m *Manager) ResumePool(ctx context.Context, poolID string) (*TenantPool, error) {
	return m.setPoolStatus(ctx, poolID, statusActive, "tenant.pool.resume")
}

// setPoolStatus validates the pool's current status → to through the central
// FSM, applies it, persists the store, and attests the transition under the
// given action name. Returns a copy of the updated pool.
func (m *Manager) setPoolStatus(ctx context.Context, poolID, to, action string) (*TenantPool, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	pool, ok := m.store.pools[poolID]
	if !ok {
		return nil, fmt.Errorf("pool %q not found", poolID)
	}
	from := pool.Status
	if err := validateLifecycleTransition("pool", poolID, from, to); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	pool.Status = to
	pool.UpdatedAt = now
	if err := m.store.saveLocked(); err != nil {
		return nil, fmt.Errorf("persist pool transition %s -> %s: %w", from, to, err)
	}
	if err := m.attest(ctx, action, poolID,
		map[string]any{"pool_id": poolID, "from_status": from, "to_status": to},
		map[string]any{"pool_id": poolID, "status": to},
		map[string]any{"members": len(pool.Members), "transitioned_at": now.Format(time.RFC3339)}); err != nil {
		return nil, err
	}
	m.logger.WithFields(logrus.Fields{
		"pool_id": poolID, "from": from, "to": to,
	}).Info("tenant pool lifecycle transition")

	cp := *pool
	cp.Members = append([]TenantMember(nil), pool.Members...)
	return &cp, nil
}

// DeletePool transitions an active or suspended pool to the terminal deleted
// state. Every member must individually reach "deleted" through the member FSM
// (a pending member blocks deletion with an explicit error); member MIG
// instances are destroyed best-effort exactly like RemoveTenant. The pool
// record is kept on disk for audit; all later write operations on it are
// rejected. Writes attestation action "tenant.pool.delete".
func (m *Manager) DeletePool(ctx context.Context, poolID string) (*TenantPool, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	pool, ok := m.store.pools[poolID]
	if !ok {
		return nil, fmt.Errorf("pool %q not found", poolID)
	}
	from := pool.Status
	if err := validateLifecycleTransition("pool", poolID, from, statusDeleted); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	destroyed := 0
	for i := range pool.Members {
		member := &pool.Members[i]
		if err := validateLifecycleTransition("tenant", member.ID, string(member.Status), statusDeleted); err != nil {
			return nil, fmt.Errorf("pool delete blocked by member: %w", err)
		}
		for _, slice := range member.MIGSlices {
			if err := m.gpuMgr.DestroyMIGInstance(ctx, slice.GPUIndex, slice.MIGInstanceID); err != nil {
				// Log but continue: bookkeeping deletion must proceed even if
				// the hardware instance is already gone (e.g. host reboot).
				m.logger.WithFields(logrus.Fields{
					"gpu": slice.GPUIndex, "instance": slice.MIGInstanceID,
				}).WithError(err).Warn("failed to destroy MIG instance during pool deletion")
				continue
			}
			destroyed++
		}
		member.Status = TenantStatus(statusDeleted)
		member.UpdatedAt = now
	}

	pool.Status = statusDeleted
	pool.UpdatedAt = now
	if err := m.store.saveLocked(); err != nil {
		return nil, fmt.Errorf("persist pool deletion: %w", err)
	}
	if err := m.attest(ctx, "tenant.pool.delete", poolID,
		map[string]any{"pool_id": poolID, "from_status": from},
		map[string]any{"pool_id": poolID, "status": statusDeleted},
		map[string]any{"members_deleted": len(pool.Members), "mig_instances_destroyed": destroyed, "deleted_at": now.Format(time.RFC3339)}); err != nil {
		return nil, err
	}
	m.logger.WithFields(logrus.Fields{
		"pool_id": poolID, "members": len(pool.Members), "destroyed": destroyed,
	}).Info("tenant pool deleted")

	cp := *pool
	cp.Members = append([]TenantMember(nil), pool.Members...)
	return &cp, nil
}

// ----------------------------------------------------------------------------
// Member lifecycle
// ----------------------------------------------------------------------------

// ActivateTenant transitions a pending tenant member to active
// (pending → active). Writes attestation action "tenant.activate".
func (m *Manager) ActivateTenant(ctx context.Context, poolID, tenantID string) (*TenantMember, error) {
	return m.setMemberStatus(ctx, poolID, tenantID, statusActive, "tenant.activate")
}

// SuspendTenant transitions an active tenant member to suspended
// (active → suspended). Writes attestation action "tenant.suspend".
func (m *Manager) SuspendTenant(ctx context.Context, poolID, tenantID string) (*TenantMember, error) {
	return m.setMemberStatus(ctx, poolID, tenantID, statusSuspended, "tenant.suspend")
}

// ResumeTenant transitions a suspended tenant member back to active
// (suspended → active). Writes attestation action "tenant.resume".
func (m *Manager) ResumeTenant(ctx context.Context, poolID, tenantID string) (*TenantMember, error) {
	return m.setMemberStatus(ctx, poolID, tenantID, statusActive, "tenant.resume")
}

// setMemberStatus validates the member's current status → to through the
// central FSM, applies it, persists the store, and attests the transition.
// Operating on a member of a deleted pool is rejected. Returns a copy of the
// updated member.
func (m *Manager) setMemberStatus(ctx context.Context, poolID, tenantID, to, action string) (*TenantMember, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()

	pool, ok := m.store.pools[poolID]
	if !ok {
		return nil, fmt.Errorf("pool %q not found", poolID)
	}
	if pool.Status == statusDeleted {
		return nil, fmt.Errorf("pool %q is deleted; member operations are rejected", poolID)
	}
	idx := findMember(pool, tenantID)
	if idx < 0 {
		return nil, fmt.Errorf("tenant %q not found in pool %q", tenantID, poolID)
	}

	member := &pool.Members[idx]
	from := string(member.Status)
	if err := validateLifecycleTransition("tenant", tenantID, from, to); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	member.Status = TenantStatus(to)
	member.UpdatedAt = now
	pool.UpdatedAt = now
	if err := m.store.saveLocked(); err != nil {
		return nil, fmt.Errorf("persist tenant transition %s -> %s: %w", from, to, err)
	}
	if err := m.attest(ctx, action, tenantID,
		map[string]any{"pool_id": poolID, "tenant_id": tenantID, "from_status": from, "to_status": to},
		map[string]any{"tenant_id": tenantID, "status": to},
		map[string]any{"transitioned_at": now.Format(time.RFC3339)}); err != nil {
		return nil, err
	}
	m.logger.WithFields(logrus.Fields{
		"pool": poolID, "tenant": tenantID, "from": from, "to": to,
	}).Info("tenant member lifecycle transition")

	cp := *member
	return &cp, nil
}

