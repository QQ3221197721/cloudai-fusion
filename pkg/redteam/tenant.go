package redteam

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// tenant.go adds multi-tenant isolation (M5). Engagements are owned by a tenant;
// cross-tenant access is refused (a not-found-style error, so existence does not
// leak across tenants). The binding is recorded so the ledger shows which tenant
// authorized which engagement.

// ActionTenantBind is the receipt binding an engagement to a tenant.
const ActionTenantBind = "redteam.tenant.bind"

// CreateForTenant creates an engagement owned by tenantID and records the binding.
func (m *Manager) CreateForTenant(ctx context.Context, tenantID string, scope Scope, principal string) (*Engagement, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("redteam: tenantID is required")
	}
	e, err := m.Create(ctx, scope, principal)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.TenantID = tenantID
	e.mu.Unlock()

	_ = capability.Report("redteam.tenant", "namespaced", capability.ModeReal,
		"per-tenant engagement ownership and isolation")
	emit(ctx, m.recorder, m.logger, evidence.RecordInput{
		Actor:    "redteam",
		Action:   ActionTenantBind,
		Subject:  e.ID,
		Input:    map[string]any{"tenant_id": tenantID, "principal": principal},
		Output:   map[string]any{"engagement_id": e.ID},
		Payload:  map[string]any{"engagement_id": e.ID, "tenant_id": tenantID},
		Backends: []evidence.BackendFact{{Component: "redteam.tenant", Mode: "real", Driver: "namespaced"}},
	})
	return e, nil
}

// GetForTenant returns an engagement only if it belongs to tenantID. A mismatch
// yields a not-found error (no cross-tenant existence disclosure).
func (m *Manager) GetForTenant(tenantID, id string) (*Engagement, error) {
	e, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	if e.TenantID != tenantID {
		return nil, fmt.Errorf("redteam: engagement %q not found for tenant %q", id, tenantID)
	}
	return e, nil
}

// ListByTenant returns only the engagements owned by tenantID.
func (m *Manager) ListByTenant(tenantID string) []*Engagement {
	var out []*Engagement
	for _, e := range m.List() {
		if e.TenantID == tenantID {
			out = append(out, e)
		}
	}
	return out
}
