// Package patent - Self-Evolving Attack Graph Engine (Patent #1)
// Implements Q-learning reinforcement learning for autonomous attack chain optimization
package patent

import (
	"encoding/binary"
	"hash/fnv"
	"math"
)

// StateID represents discrete encoding of attack state for tabular Q-learning
type StateID uint64

// ActionID represents discrete action encoding
type ActionID int

const (
	// InvalidActionID sentinel value when no actions available
	InvalidActionID ActionID = -1
	
	// InitialAttackStateSeed starting hash seed
	InitialAttackStateSeed = 1234567890
)

// Initialize invalid state/action IDs
var (
	// InvalidStateID sentinel value for terminal states (max uint64)
	InvalidStateID = StateID(^uint64(0))
)

// EncodeState converts concrete State into discrete state ID for Q-table lookup
// Uses FNV-1a hash function for uniform distribution across state space
func EncodeState(state State) StateID {
	h := fnv.New64a()
	
	// Hash all state components
	h.Write([]byte(state.NetworkPosition))
	h.Write(binary.LittleEndian.AppendUint32(nil, uint32(state.PrivilegeLevel)))
	h.Write(binary.LittleEndian.AppendUint64(nil, math.Float64bits(state.StealthScore)))
	h.Write([]byte{0x01, 0x00}) // UnderDetection boolean flag
	h.Write(binary.LittleEndian.AppendUint32(nil, uint32(state.TTL)))
	
	// Hash ActiveCVEs list
	h.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(state.ActiveCVEs))))
	for _, cve := range state.ActiveCVEs {
		h.Write([]byte(cve))
	}
	
	return StateID(h.Sum64())
}

// DecodeState reverses state encoding (for debugging/tracing only)
func DecodeState(stateID StateID) State {
	// This is lossy decode (hash→state is one-way)
	// For full reconstruction, store state snapshots separately
	return State{
		NetworkPosition: "reconstructed",
		PrivilegeLevel:  int((uint64(stateID) >> 48) % 4),
		StealthScore:    float64(uint64(stateID)&0x0000FFFFFFFFFFFF) / 10000.0,
		UnderDetection:  (stateID & 0x0100000000000000) != 0,
		TTL:             int((uint64(stateID) >> 16) % 256),
		ActiveCVEs:      nil, // Not recoverable from hash
	}
}

// EncodeAction converts concrete Action into discrete action ID
func EncodeAction(action Action) ActionID {
	h := fnv.New32a()
	
	// Hash action type and target CVE
	h.Write([]byte(action.Type))
	h.Write([]byte(action.TargetCVE))
	
	return ActionID(int(h.Sum32()))
}

// DecodeAction reverses action encoding
func DecodeAction(actionID ActionID) Action {
	// Lossy decode - reconstruct based on type heuristics
	action := Action{
		Type:              "unknown",
		TargetCVE:         "UNKNOWN",
		RequiredPrivilege: 0,
		StealthImpact:     0.0,
		DetectionRisk:     0.0,
		ExpectedReward:    0.0,
	}
	
	// Use actionID as heuristic for action type selection
	switch actionID % 5 {
	case 0:
		action.Type = "inject_payload"
	case 1:
		action.Type = "pivot"
	case 2:
		action.Type = "escalate"
	case 3:
		action.Type = "persist"
	case 4:
		action.Type = "exfiltrate"
	}
	
	return action
}

// TransitionState computes next state given current state and action
// This is the deterministic policy extraction function
func TransitionState(currentState StateID, action ActionID) StateID {
	// Simplified transition model (in production would use simulator)
	// Here we just increment state ID to simulate progression
	return StateID(uint64(currentState) + uint64(action)+1)
}

// AvailableActionsForState returns list of valid actions for a given state
// In real implementation, this would be domain-specific constraints
func AvailableActionsForState(state StateID) []ActionID {
	actions := []ActionID{
		ActionID(0), // inject_payload
		ActionID(1), // pivot
		ActionID(2), // escalate
		ActionID(3), // persist
		ActionID(4), // exfiltrate
	}
	
	return actions
}
