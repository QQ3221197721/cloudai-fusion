// Package fabricwiring provides production-time assembly of the VerifiableFabric
// from the platform's subsystems' real wells. It registers each well with its
// actual KeyOf function so the platform can prove completeness per-namespace
// (finops month / scheduler tenant / redteam engagement / delivery cluster).
//
// This is additive over pkg/evidence and pkg/fabric; no cryptography changes.
package fabricwiring

import (
	"github.com/cloudai-fusion/cloudai-fusion/pkg/delivery"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/finops"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// Well defines a fabric well with name, prefix, actor and key extraction function.
type Well struct {
	Name   string
	Prefix string
	Actor  string
	KeyOf  func(*evidence.Evidence) string
}

// AllWells returns all registered well definitions across pillars (production-ready).
func AllWells() []Well {
	return []Well{
		// Cloud-native pillar: scheduling & FinOps & GPU isolation
		{Name: "L10-compute", Prefix: "scheduler/tenant", Actor: "scheduler", KeyOf: scheduler.TenantKeyOf},
		{Name: "CN-3-gpu-isolation", Prefix: "gpu/isolation", Actor: "scheduler", KeyOf: scheduler.IsolationNodeKeyOf},
		{Name: "L15-finops", Prefix: "finops/month", Actor: "finops", KeyOf: finops.MonthKeyOf},
		// Red-team pillar
		{Name: "L14-redteam", Prefix: "redteam/exploit", Actor: "redteam", KeyOf: redteam.EngagementKeyOf},
		// Delivery pillar
		{Name: "DL-1-deploy", Prefix: "delivery/deploy", Actor: "delivery", KeyOf: delivery.ClusterKeyOf},
		{Name: "DL-2-failover", Prefix: "delivery/failover", Actor: "delivery", KeyOf: delivery.FailoverServiceKeyOf},
		{Name: "DL-3-edge", Prefix: "delivery/edge", Actor: "delivery", KeyOf: delivery.EdgeNodeKeyOf},
	}
}

// Build constructs a VerifiableFabric by registering all real wells over a ledger.
// Caller must provide a *evidence.Ledger which satisfies fabric.Ledger interface.
func Build(ledger fabric.Ledger) (*fabric.Fabric, error) {
	f := fabric.New(ledger)
	for _, w := range AllWells() {
		if err := f.Register(fabric.Well{Name: w.Name, Prefix: w.Prefix, Actor: w.Actor, KeyOf: w.KeyOf}); err != nil {
			return nil, err
		}
	}
	return f, nil
}
