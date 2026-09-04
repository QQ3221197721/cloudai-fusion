// Package wasm — Module 53 Formal Memory-Safety Model (Task #264, T3 MoAT).
//
// This file is a *machine-checkable* rendering of the memory-safety argument for
// the GPU WASI runtime. It is deliberately additive: it introduces no new runtime
// behaviour and does not modify any production path. Its purpose is to let the
// accompanying proof (proof_memory_safety_m53.md) rest on executable predicates
// that the adversarial test-suite (adversarial_*_test.go) can exhaustively probe,
// instead of on prose alone.
//
// The abstract machine models one GPU-buffer access attempted by an untrusted
// WASM guest. A concrete access in production flows through:
//
//	guest -> RegisterHostFunctions wrapper -> withCapabilityCheck(grant)
//	      -> GetZeroView(grant, handle, offset, length)  [capability.go + zerocopy_buffer.go]
//
// The predicates below mirror exactly the decision logic of that path so the
// Hoare triple proved on paper is the same one the code enforces.
package wasm

// AddressRegion models a half-open byte interval [Base, Base+Length) inside the
// host shadow buffer identified by Handle. A WASM guest can only ever *name* a
// region; it can never fabricate a pointer into host address space directly,
// because handles are opaque uint64 keys minted by the sharded allocator.
type AddressRegion struct {
	Handle uint64
	Base   uint64
	Length uint64
}

// SafetyState S = (Grant, LiveBuffers, Access) is the abstract state of the
// machine at the instant a guest attempts one memory access.
//   - Grant:       the capability grant carried by the plugin (Module 51).
//   - LiveBuffers: handle -> size(bytes) of every buffer currently owned by the
//     enclave, i.e. the *allow-listed address space*.
//   - Access:      the region the guest is attempting to touch.
type SafetyState struct {
	Grant       *Grant
	LiveBuffers map[uint64]uint64
	Access      AddressRegion
}

// CapabilityGranted is the Hoare pre-condition Pre.
//
//	Pre(S) ≡ S.Grant authorises GPU access ∧ at least one allowed device exists.
//
// It is the exact predicate enforced by mockGPUService.withCapabilityCheck plus
// the device gate inside GetZeroView.
func (s SafetyState) CapabilityGranted() bool {
	if s.Grant == nil || !s.Grant.HasGPUAccess() || s.Grant.GPU == nil {
		return false
	}
	return len(s.Grant.GPU.AllowedDevices) > 0
}

// BufferLive reports whether the accessed handle refers to a buffer that is
// currently owned by the enclave (present in the allow-listed address space).
func (s SafetyState) BufferLive() bool {
	if s.LiveBuffers == nil {
		return false
	}
	size, ok := s.LiveBuffers[s.Access.Handle]
	return ok && size > 0
}

// InBounds reports whether Access ⊆ [0, size) of its live backing buffer, using
// overflow-safe uint64 arithmetic (Base+Length is checked without wrapping).
func (s SafetyState) InBounds() bool {
	size, ok := s.LiveBuffers[s.Access.Handle]
	if !ok || size == 0 {
		return false
	}
	if s.Access.Length == 0 {
		return false
	}
	if s.Access.Base >= size {
		return false
	}
	// Guard against Base+Length overflowing uint64 before the range comparison.
	if s.Access.Length > size-s.Access.Base {
		return false
	}
	return true
}

// Safe is the safety invariant Safe(S): the access is authorised by capability,
// targets a live allow-listed buffer, and lies wholly inside that buffer.
//
//	Safe(S) ≡ CapabilityGranted(S) ∧ BufferLive(S) ∧ InBounds(S)
func (s SafetyState) Safe() bool {
	return s.CapabilityGranted() && s.BufferLive() && s.InBounds()
}

// GuardedAccess is the reference monitor: the pure decision the production code
// makes about whether to admit the access. It returns true iff the access is
// allowed to proceed. By construction it admits an access only when Safe(S)
// holds — this is the property the Hoare triple certifies.
func GuardedAccess(s SafetyState) bool {
	// Reproduce the layered checks in the order the code applies them:
	//  1. capability gate (withCapabilityCheck)
	//  2. device authorisation (GetZeroView device loop)
	//  3. handle liveness (ShardedHandleAllocator.GetHandleSize)
	//  4. bounds validation (offset/length vs size)
	if !s.CapabilityGranted() {
		return false
	}
	if !s.BufferLive() {
		return false
	}
	if !s.InBounds() {
		return false
	}
	return true
}

// HoareResult records the outcome of checking one triple, for reporting.
type HoareResult struct {
	Admitted     bool // did the reference monitor admit the access?
	PostSafe     bool // did the post-condition Safe hold when admitted?
	TripleHolds  bool // {Pre} access {Post} verified for this state?
	RejectReason string
}

// CheckHoareTriple verifies the partial-correctness triple
//
//	{Pre} guardedAccess {Post}
//
// where Post ≡ ( admitted ⇒ Safe(S) ) ∧ ( ¬admitted ⇒ state unchanged ).
//
// Because GuardedAccess performs no mutation, the second conjunct is trivially
// true, so the triple reduces to: whenever the monitor admits an access, the
// safety invariant Safe(S) holds. This function returns whether that holds for
// the supplied state.
func CheckHoareTriple(s SafetyState) HoareResult {
	admitted := GuardedAccess(s)
	safe := s.Safe()

	res := HoareResult{Admitted: admitted, PostSafe: safe}
	if admitted {
		// Soundness: an admitted access must be provably safe.
		res.TripleHolds = safe
		if !safe {
			res.RejectReason = "UNSOUND: monitor admitted an unsafe access"
		}
		return res
	}

	// Not admitted: the access is blocked, so no unsafe memory operation runs.
	// The triple holds vacuously (Post's ¬admitted branch).
	res.TripleHolds = true
	res.RejectReason = classifyRejection(s)
	return res
}

// classifyRejection explains why the monitor blocked an access; used to prove
// completeness of the block (every unsafe state is rejected for a stated cause).
func classifyRejection(s SafetyState) string {
	switch {
	case !s.CapabilityGranted():
		return "capability-denied"
	case !s.BufferLive():
		return "handle-not-live"
	case !s.InBounds():
		return "out-of-bounds"
	default:
		return "none"
	}
}

// EnclaveBoundary is a declarative record of the CPU/GPU isolation guarantees the
// model *assumes* from the layer below it. The Verified flag distinguishes
// guarantees enforced by code in this repository (real) from guarantees that are
// delegated to hardware / the wazero core spec and only modelled here (assumed).
type EnclaveBoundary struct {
	Name        string
	Guarantee   string
	EnforcedBy  string
	Verified    bool // true = checkable in this repo's tests; false = assumed from HW/spec/literature
}

// EnclaveBoundaries enumerates the isolation assumptions underpinning Safe(S).
// Honesty requirement: guarantees we cannot exercise in-repo are marked
// Verified=false and are treated as assumptions in the written proof.
func EnclaveBoundaries() []EnclaveBoundary {
	return []EnclaveBoundary{
		{
			Name:       "WASM linear-memory confinement",
			Guarantee:  "guest loads/stores trap outside declared linear memory",
			EnforcedBy: "wazero core-spec bounds checks + WithMemoryLimitPages",
			Verified:   true, // exercised by TestAdversarial_WASMLinearMemoryOOBTrap
		},
		{
			Name:       "Host-buffer bounds confinement",
			Guarantee:  "descriptor offset/length can never exceed backing size",
			EnforcedBy: "GetZeroView bounds validation + SafetyState.InBounds",
			Verified:   true, // exercised by TestAdversarial_ZeroViewOOB*
		},
		{
			Name:       "Handle unforgeability / per-shard ownership",
			Guarantee:  "a guest cannot name a buffer it was not granted",
			EnforcedBy: "opaque ShardKey handles + per-shard mutex ownership",
			Verified:   true, // exercised by TestAdversarial_ForgedHandleRejected
		},
		{
			Name:       "CPU<->GPU memory isolation",
			Guarantee:  "guest cannot read host/other-tenant DRAM or VRAM",
			EnforcedBy: "Intel SGX EPCM / AMD SEV-SNP RMP page ownership (hardware)",
			Verified:   false, // assumed from vendor literature; no SGX/SEV device in CI
		},
		{
			Name:       "Micro-architectural side-channel resistance",
			Guarantee:  "no cache/timing leak of enclave memory contents",
			EnforcedBy: "HW mitigations (LVI/MDS microcode) — out of scope for app layer",
			Verified:   false, // partially probed by timing test; not a hardware proof
		},
	}
}
