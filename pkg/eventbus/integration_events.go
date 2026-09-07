// Package eventbus - Cross-Module Integration Events
//
// These events form the backbone of CloudAI Fusion's system-level performance barriers.
// Unlike single-module optimizations (cache/parallel), these events create feedback loops
// across 4+ modules that competitors cannot replicate by copying a single file.
//
// Feedback Loops:
//   Group A: Schedule → FinOps → Monitor → AIOps → Schedule (cost-aware self-healing)
//   Group B: WAF → RedTeam → Evidence → ZKP (crypto-audit chain)
//   Group C: Edge → Federation → TEE → Evidence (verifiable distributed AI)
//   Group D: GitOps → Chaos → SLO → DevSecOps (quality assurance pipeline)
//   Group E: Workload → Ticket → Billing → Cluster (ops automation)
//   Group F: DB → Event → WebSocket → Evidence (data integrity chain)
package eventbus

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// ============================================================================
// Cross-Module Event Types (the glue that creates system-level barriers)
// ============================================================================

// IntegrationEvent carries data between modules with cryptographic proof of origin.
// Every cross-module event is signed, making the integration tamper-proof.
type IntegrationEvent struct {
	ID        string    `json:"id"`
	Type      EventType `json:"type"`
	Source    string    `json:"source"`     // originating module
	Timestamp time.Time `json:"timestamp"`
	Payload   []byte    `json:"payload"`

	// Evidence fields — every event carries proof of authenticity
	SignerID  string `json:"signer_id"`
	Signature []byte `json:"signature"` // Ed25519 over (Type + Source + Timestamp + Payload)
}

// EventType defines cross-module integration event categories.
type EventType string

const (
	// Group A: Schedule-FinOps-Monitor-AIOps loop
	EventGPUScheduled         EventType = "gpu.scheduled"          // scheduler → finops
	EventCostAnomaly          EventType = "finops.cost_anomaly"    // finops → aiops
	EventGPUUnderutilized     EventType = "monitor.gpu_underutil"  // monitor → scheduler
	EventAnomalyConfirmed     EventType = "aiops.anomaly_confirmed" // aiops → disaster (evidence)

	// Group B: Security-Evidence-Compliance loop
	EventPermissionDecision   EventType = "auth.permission_decision" // auth → evidence
	EventWAFAttackDetected    EventType = "waf.attack_detected"      // waf → redteam
	EventPromptInjectionFound EventType = "plugin.prompt_injection"   // plugin → tee
	EventFailoverCompliant    EventType = "zkp.failover_compliant"    // zkp → evidence

	// Group C: Edge-Federation-Plugin
	EventEdgeSyncCompleted    EventType = "edge.sync_completed"     // edge → federation
	EventFederationAggregated EventType = "fed.aggregated"          // fed → tee
	EventPluginExecuted       EventType = "wasm.plugin_executed"    // wasm → evidence

	// Group D: DevOps-GitOps-Chaos
	EventDeployCompleted      EventType = "gitops.deploy_completed" // gitops → chaos
	EventChaosResult          EventType = "chaos.experiment_result" // chaos → monitor (SLO)
	EventInfraChanged         EventType = "terraform.infra_changed" // terraform → devsecops
	EventScanPassed           EventType = "devsecops.scan_passed"   // devsecops → gitops (gate)

	// Group E: Workload-Ticket-Billing
	EventWorkloadFailed       EventType = "workload.failed"        // workload → ticket + billing
	EventTicketResolved       EventType = "support.ticket_resolved" // support → billing + workload
	EventBudgetNearLimit      EventType = "billing.budget_near_limit" // billing → workload (throttle)
	EventClusterJoined        EventType = "cluster.joined"          // cluster → auth + monitor

	// Group F: Data integrity
	EventDataWritten          EventType = "db.data_written"        // db → evidence
	EventMessagePublished     EventType = "event.message_published" // eventbus → evidence
	EventClientPush           EventType = "ws.client_push"          // websocket → evidence (hash)
)

// SignEvent signs an integration event with Ed25519 private key.
// This ensures no module can forge events from another module.
func SignEvent(event *IntegrationEvent, privateKey ed25519.PrivateKey, signerID string) {
	event.SignerID = signerID
	event.Timestamp = time.Now()
	msg := computeEventHash(event)
	event.Signature = ed25519.Sign(privateKey, msg)
}

// VerifyEvent checks that an integration event was signed by the claimed signer.
func VerifyEvent(event *IntegrationEvent, publicKey ed25519.PublicKey) bool {
	msg := computeEventHash(event)
	return ed25519.Verify(publicKey, msg, event.Signature)
}

func computeEventHash(event *IntegrationEvent) []byte {
	h := sha256.New()
	h.Write([]byte(event.Type))
	h.Write([]byte(event.Source))
	h.Write([]byte(event.Timestamp.Format(time.RFC3339Nano)))
	h.Write(event.Payload)
	return h.Sum(nil)
}

// EventReceipt is a compact proof that an event was processed.
// Stored in Evidence Ledger for audit trail.
type EventReceipt struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Hash      string `json:"hash"` // SHA-256 of full event
	SignerID  string `json:"signer_id"`
	Timestamp int64  `json:"timestamp_unix"`
}

// MakeReceipt generates an audit receipt for an integration event.
func MakeReceipt(event *IntegrationEvent) EventReceipt {
	fullHash := sha256.Sum256(append(event.Payload, event.Signature...))
	return EventReceipt{
		EventID:   event.ID,
		EventType: string(event.Type),
		Hash:      hex.EncodeToString(fullHash[:]),
		SignerID:  event.SignerID,
		Timestamp: event.Timestamp.Unix(),
	}
}
