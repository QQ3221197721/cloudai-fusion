package training

import (
	"strings"
	"testing"
	"time"
)

// fixedSeed produces a deterministic signer so signature-related assertions are reproducible.
func testSigner(t testing.TB) *ReceiptSigner {
	t.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	s, err := NewReceiptSignerFromSeed(seed)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return s
}

// bigScheduler builds a scheduler with capacity large enough to admit typical test gangs, with a frozen clock.
func bigScheduler(t testing.TB) *GangScheduler {
	t.Helper()
	s, err := NewGangScheduler(ClusterCapacity{GPUs: 64, CPUCores: 512, MemoryGB: 2048}, testSigner(t))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	s.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })
	return s
}

func validSpec(name string) GangJobSpec {
	return GangJobSpec{
		Name:      name,
		Image:     "pytorch:2.3",
		Replicas:  4,
		Priority:  10,
		Resources: ResourceRequest{GPUs: 2, CPUCores: 8, MemoryGB: 32},
		Command:   "torchrun --nproc_per_node=2 train.py",
		Queue:     "research",
	}
}

// ----------------------------------------------------------------------------
// FSM: transition table completeness and edge legality
// ----------------------------------------------------------------------------

// TestFSM_TableIsExhaustive asserts every state (including terminals) is a key in gangTransitions,
// so canGangTransition never encounters an "unknown state" for a legitimate GangState.
func TestFSM_TableIsExhaustive(t *testing.T) {
	allStates := []GangState{GangPending, GangReady, GangRunning, GangSucceeded, GangFailed}
	for _, st := range allStates {
		if _, ok := gangTransitions[st]; !ok {
			t.Fatalf("state %q missing from gangTransitions (unhandled state)", st)
		}
	}
	if len(gangTransitions) != len(allStates) {
		t.Fatalf("gangTransitions has %d entries, expected %d — extra/undocumented state", len(gangTransitions), len(allStates))
	}
}

// TestFSM_AllEdges verifies canGangTransition against the full cross-product of states.
func TestFSM_AllEdges(t *testing.T) {
	all := []GangState{GangPending, GangReady, GangRunning, GangSucceeded, GangFailed}
	legal := map[GangState]map[GangState]bool{
		GangPending: {GangReady: true, GangFailed: true},
		GangReady:   {GangRunning: true, GangFailed: true},
		GangRunning: {GangSucceeded: true, GangFailed: true},
	}
	for _, from := range all {
		for _, to := range all {
			allowed, known := canGangTransition(from, to)
			if !known {
				t.Fatalf("state %q reported unknown", from)
			}
			want := legal[from][to]
			if allowed != want {
				t.Errorf("transition %q→%q: allowed=%v want=%v", from, to, allowed, want)
			}
		}
	}
}

// TestFSM_UnknownStateRejected ensures a corrupt state is reported as unknown, not silently allowed.
func TestFSM_UnknownStateRejected(t *testing.T) {
	allowed, known := canGangTransition(GangState("bogus"), GangReady)
	if known || allowed {
		t.Fatalf("bogus state should be unknown & disallowed, got allowed=%v known=%v", allowed, known)
	}
}

// TestFSM_TerminalStatesAreSinks confirms succeeded/failed have no outgoing edges.
func TestFSM_TerminalStatesAreSinks(t *testing.T) {
	for _, term := range []GangState{GangSucceeded, GangFailed} {
		if edges := gangTransitions[term]; len(edges) != 0 {
			t.Errorf("terminal state %q should have no edges, has %v", term, edges)
		}
		if !isGangTerminal(term) {
			t.Errorf("isGangTerminal(%q) should be true", term)
		}
	}
	for _, live := range []GangState{GangPending, GangReady, GangRunning} {
		if isGangTerminal(live) {
			t.Errorf("isGangTerminal(%q) should be false", live)
		}
	}
}

// ----------------------------------------------------------------------------
// Happy path: submit → admit → start → succeed
// ----------------------------------------------------------------------------

func TestLifecycle_HappyPath(t *testing.T) {
	s := bigScheduler(t)
	job, err := s.Submit(validSpec("bert-pretrain"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if job.State != GangPending {
		t.Fatalf("post-submit state = %q, want pending", job.State)
	}
	if len(job.Events) != 1 || job.Events[0].To != GangPending {
		t.Fatalf("submit should record exactly one pending event, got %+v", job.Events)
	}

	res, err := s.Admit(job.ID)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !res.Admitted || res.Members != 4 {
		t.Fatalf("admit result = %+v, want admitted with 4 members", res)
	}

	if err := s.Start(job.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Succeed(job.ID); err != nil {
		t.Fatalf("succeed: %v", err)
	}

	got, err := s.Get(job.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != GangSucceeded {
		t.Fatalf("final state = %q, want succeeded", got.State)
	}
	wantChain := []GangState{GangPending, GangReady, GangRunning, GangSucceeded}
	if len(got.Events) != len(wantChain) {
		t.Fatalf("event count = %d, want %d", len(got.Events), len(wantChain))
	}
	for i, ev := range got.Events {
		if ev.To != wantChain[i] {
			t.Errorf("event[%d].To = %q, want %q", i, ev.To, wantChain[i])
		}
		if err := VerifyReceipt(ev.Receipt); err != nil {
			t.Errorf("event[%d] receipt failed verification: %v", i, err)
		}
	}
	// Sequence numbers must be strictly increasing across the lifecycle.
	for i := 1; i < len(got.Events); i++ {
		if got.Events[i].Receipt.Seq <= got.Events[i-1].Receipt.Seq {
			t.Errorf("receipt seq not increasing: %d then %d", got.Events[i-1].Receipt.Seq, got.Events[i].Receipt.Seq)
		}
	}
	// Resources must be fully released after success.
	if avail := s.Available(); avail.GPUs != 64 || avail.CPUCores != 512 || avail.MemoryGB != 2048 {
		t.Fatalf("resources not released after success: %+v", avail)
	}
}

// ----------------------------------------------------------------------------
// Failure edges: Fail from every non-terminal state
// ----------------------------------------------------------------------------

func TestFail_FromPending_NoReservation(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))
	if err := s.Fail(job.ID, "user cancelled before admission"); err != nil {
		t.Fatalf("fail from pending: %v", err)
	}
	got, _ := s.Get(job.ID)
	if got.State != GangFailed {
		t.Fatalf("state = %q, want failed", got.State)
	}
	if avail := s.Available(); avail.GPUs != 64 {
		t.Fatalf("no reservation should have been released, avail=%+v", avail)
	}
}

func TestFail_FromGangReady_ReleasesResources(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))
	if _, err := s.Admit(job.ID); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if avail := s.Available(); avail.GPUs != 64-8 { // 4 replicas * 2 GPU
		t.Fatalf("expected 8 GPUs reserved, avail=%+v", avail)
	}
	if err := s.Fail(job.ID, "pre-launch abort"); err != nil {
		t.Fatalf("fail from gang_ready: %v", err)
	}
	if avail := s.Available(); avail.GPUs != 64 {
		t.Fatalf("resources not released after fail: %+v", avail)
	}
}

func TestFail_FromRunning_ReleasesResources(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))
	if _, err := s.Admit(job.ID); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := s.Start(job.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Fail(job.ID, "OOM on replica 3"); err != nil {
		t.Fatalf("fail from running: %v", err)
	}
	got, _ := s.Get(job.ID)
	if got.State != GangFailed {
		t.Fatalf("state = %q, want failed", got.State)
	}
	if avail := s.Available(); avail.GPUs != 64 {
		t.Fatalf("resources not released: %+v", avail)
	}
}

// ----------------------------------------------------------------------------
// Illegal transitions are rejected without corrupting state
// ----------------------------------------------------------------------------

func TestIllegalTransitions_Rejected(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))

	// Start requires GangReady; from Pending it must fail.
	if err := s.Start(job.ID); err == nil {
		t.Fatal("Start from pending should fail")
	}
	// Succeed requires Running.
	if err := s.Succeed(job.ID); err == nil {
		t.Fatal("Succeed from pending should fail")
	}
	// State must be untouched.
	got, _ := s.Get(job.ID)
	if got.State != GangPending {
		t.Fatalf("state corrupted by illegal transition: %q", got.State)
	}

	// Admit, then illegal double-admit.
	if _, err := s.Admit(job.ID); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, err := s.Admit(job.ID); err == nil {
		t.Fatal("double admit should fail")
	}
}

func TestTerminal_RejectsFurtherTransitions(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))
	if _, err := s.Admit(job.ID); err != nil {
		t.Fatalf("admit: %v", err)
	}
	if err := s.Start(job.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := s.Succeed(job.ID); err != nil {
		t.Fatalf("succeed: %v", err)
	}
	// From succeeded, all further transitions must be rejected.
	if err := s.Start(job.ID); err == nil {
		t.Fatal("Start from succeeded should fail")
	}
	if err := s.Fail(job.ID, "late"); err == nil {
		t.Fatal("Fail from succeeded should fail")
	}
	if err := s.Succeed(job.ID); err == nil {
		t.Fatal("Succeed from succeeded should fail")
	}
}

// ----------------------------------------------------------------------------
// Gang all-or-nothing admission semantics
// ----------------------------------------------------------------------------

func TestAdmission_AllOrNothing_Rejected(t *testing.T) {
	// Capacity fits 3 replicas but the gang needs 4 → whole gang must be rejected, nothing reserved.
	s, err := NewGangScheduler(ClusterCapacity{GPUs: 7, CPUCores: 1000, MemoryGB: 10000}, testSigner(t))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	spec := validSpec("big") // 4 replicas * 2 GPU = 8 GPU needed, only 7 available
	job, _ := s.Submit(spec)

	res, err := s.Admit(job.ID)
	if err != nil {
		t.Fatalf("admit returned error (should be a decision): %v", err)
	}
	if res.Admitted {
		t.Fatal("gang needing 8 GPUs should not fit in 7")
	}
	if res.Shortfall.GPUs != 1 {
		t.Errorf("shortfall GPUs = %d, want 1", res.Shortfall.GPUs)
	}
	// Nothing reserved: job stays pending, full capacity available.
	got, _ := s.Get(job.ID)
	if got.State != GangPending {
		t.Fatalf("rejected gang should stay pending, got %q", got.State)
	}
	if avail := s.Available(); avail.GPUs != 7 {
		t.Fatalf("rejected admission must not reserve, avail=%+v", avail)
	}
}

func TestAdmission_MinMembers_PartialGang(t *testing.T) {
	// 8 replicas requested, but min_members=4 → only 4 must co-schedule.
	s, err := NewGangScheduler(ClusterCapacity{GPUs: 8, CPUCores: 1000, MemoryGB: 10000}, testSigner(t))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	spec := GangJobSpec{
		Name:       "elastic",
		Image:      "pytorch:2.3",
		Replicas:   8,
		MinMembers: 4,
		Resources:  ResourceRequest{GPUs: 2, CPUCores: 4, MemoryGB: 16},
	}
	job, _ := s.Submit(spec)
	res, err := s.Admit(job.ID)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !res.Admitted || res.Members != 4 {
		t.Fatalf("min_members gang should admit 4 members, got %+v", res)
	}
	// 4 members * 2 GPU = 8 reserved → 0 available.
	if avail := s.Available(); avail.GPUs != 0 {
		t.Fatalf("expected all 8 GPUs reserved, avail=%+v", avail)
	}
	got, _ := s.Get(job.ID)
	if got.AdmittedMembers != 4 {
		t.Errorf("AdmittedMembers = %d, want 4", got.AdmittedMembers)
	}
}

func TestAdmission_ContentionThenRelease(t *testing.T) {
	// Two gangs of 8 GPU each against a 8-GPU cluster: first fits, second is rejected until first releases.
	s, err := NewGangScheduler(ClusterCapacity{GPUs: 8, CPUCores: 1000, MemoryGB: 10000}, testSigner(t))
	if err != nil {
		t.Fatalf("new scheduler: %v", err)
	}
	a, _ := s.Submit(validSpec("a"))
	b, _ := s.Submit(validSpec("b"))

	if res, _ := s.Admit(a.ID); !res.Admitted {
		t.Fatal("first gang should be admitted")
	}
	if res, _ := s.Admit(b.ID); res.Admitted {
		t.Fatal("second gang should be rejected under contention")
	}
	// Release the first, then the second fits.
	if err := s.Fail(a.ID, "done"); err != nil {
		t.Fatalf("fail a: %v", err)
	}
	if res, _ := s.Admit(b.ID); !res.Admitted {
		t.Fatal("second gang should be admitted after first releases")
	}
}

func TestTryAdmit_IsPure(t *testing.T) {
	s := bigScheduler(t)
	spec := validSpec("probe")
	before := s.Available()
	res := s.TryAdmit(spec)
	if !res.Admitted {
		t.Fatalf("probe should fit: %+v", res)
	}
	after := s.Available()
	if before != after {
		t.Fatalf("TryAdmit mutated capacity: before=%+v after=%+v", before, after)
	}
}

// ----------------------------------------------------------------------------
// Ed25519 receipts (the moat)
// ----------------------------------------------------------------------------

func TestReceipt_VerifyValid(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))
	got, _ := s.Get(job.ID)
	if err := VerifyReceipt(got.Events[0].Receipt); err != nil {
		t.Fatalf("valid receipt should verify: %v", err)
	}
}

func TestReceipt_TamperDetected(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))
	got, _ := s.Get(job.ID)
	r := got.Events[0].Receipt

	// Tamper with each signed field; every mutation must break verification.
	mutations := []func(LifecycleReceipt) LifecycleReceipt{
		func(x LifecycleReceipt) LifecycleReceipt { x.To = GangRunning; return x },
		func(x LifecycleReceipt) LifecycleReceipt { x.Replicas = 999; return x },
		func(x LifecycleReceipt) LifecycleReceipt { x.Seq = 42; return x },
		func(x LifecycleReceipt) LifecycleReceipt { x.JobID = "gang-evil"; return x },
		func(x LifecycleReceipt) LifecycleReceipt { x.Reason = "forged"; return x },
		func(x LifecycleReceipt) LifecycleReceipt { x.IssuedAt = x.IssuedAt.Add(time.Second); return x },
	}
	for i, m := range mutations {
		if err := VerifyReceipt(m(r)); err == nil {
			t.Errorf("mutation %d should fail verification", i)
		}
	}
}

func TestReceipt_WrongKeyRejected(t *testing.T) {
	s := bigScheduler(t)
	job, _ := s.Submit(validSpec("t"))
	got, _ := s.Get(job.ID)
	r := got.Events[0].Receipt
	// Swap in a different signer's public key without re-signing → must fail.
	other := testSignerSeed(t, 99)
	r.PublicKey = other.PublicKeyBase64()
	if err := VerifyReceipt(r); err == nil {
		t.Fatal("receipt with mismatched public key should fail verification")
	}
}

func TestReceipt_MalformedInputs(t *testing.T) {
	if err := VerifyReceipt(LifecycleReceipt{PublicKey: "!!!not-base64", Signature: "AAAA"}); err == nil {
		t.Fatal("bad base64 public key should error")
	}
	if err := VerifyReceipt(LifecycleReceipt{PublicKey: "AAAA", Signature: "AAAA"}); err == nil {
		t.Fatal("wrong-size key should error")
	}
}

func testSignerSeed(t *testing.T, b byte) *ReceiptSigner {
	t.Helper()
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = b
	}
	s, err := NewReceiptSignerFromSeed(seed)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return s
}

// ----------------------------------------------------------------------------
// Construction & validation guards
// ----------------------------------------------------------------------------

func TestNewGangScheduler_RequiresSigner(t *testing.T) {
	if _, err := NewGangScheduler(ClusterCapacity{GPUs: 1}, nil); err == nil {
		t.Fatal("nil signer must be rejected (signed receipts are mandatory)")
	}
}

func TestSubmit_Validation(t *testing.T) {
	s := bigScheduler(t)
	cases := []struct {
		name string
		spec GangJobSpec
		want string
	}{
		{"no name", GangJobSpec{Image: "x", Replicas: 1, Resources: ResourceRequest{GPUs: 1}}, "name is required"},
		{"no image", GangJobSpec{Name: "x", Replicas: 1, Resources: ResourceRequest{GPUs: 1}}, "image is required"},
		{"zero replicas", GangJobSpec{Name: "x", Image: "y", Resources: ResourceRequest{GPUs: 1}}, "replicas must be positive"},
		{"min>replicas", GangJobSpec{Name: "x", Image: "y", Replicas: 2, MinMembers: 5, Resources: ResourceRequest{GPUs: 1}}, "exceeds replicas"},
		{"no resources", GangJobSpec{Name: "x", Image: "y", Replicas: 1}, "at least one resource"},
		{"negative gpu", GangJobSpec{Name: "x", Image: "y", Replicas: 1, Resources: ResourceRequest{GPUs: -1}}, "cannot be negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Submit(tc.spec)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Submit(%s): err=%v, want contains %q", tc.name, err, tc.want)
			}
		})
	}
}

func TestGet_UnknownJob(t *testing.T) {
	s := bigScheduler(t)
	if _, err := s.Get("gang-nope"); err != ErrGangNotFound {
		t.Fatalf("Get unknown = %v, want ErrGangNotFound", err)
	}
	if _, err := s.Admit("gang-nope"); err != ErrGangNotFound {
		t.Fatalf("Admit unknown = %v, want ErrGangNotFound", err)
	}
}

func TestList_PriorityOrder(t *testing.T) {
	s := bigScheduler(t)
	lo := validSpec("low")
	lo.Priority = 1
	lo.Replicas = 1
	lo.Resources = ResourceRequest{GPUs: 1, CPUCores: 1, MemoryGB: 1}
	hi := validSpec("high")
	hi.Priority = 100
	hi.Replicas = 1
	hi.Resources = ResourceRequest{GPUs: 1, CPUCores: 1, MemoryGB: 1}
	if _, err := s.Submit(lo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(hi); err != nil {
		t.Fatal(err)
	}
	list := s.List()
	if len(list) != 2 || list[0].Spec.Name != "high" {
		t.Fatalf("List should order by priority desc, got %+v", list)
	}
}
