package tutorial

// progress_test.go covers the step state machine with tests for gating,
// concurrency safety, and snapshot resume.

import (
	"encoding/json"
	"sync"
	"testing"
)

func buildLinearTutorial() *Tutorial {
	return &Tutorial{
		ID:    "linear",
		Title: "Linear Tutorial",
		Steps: []Step{
			{ID: "s1", Title: "First", Instruction: "Do 1"},
			{ID: "s2", Title: "Second", Instruction: "Do 2", Prerequisites: []string{"s1"}},
			{ID: "s3", Title: "Third", Instruction: "Do 3", Prerequisites: []string{"s2"}},
		},
	}
}

func buildDiamondTutorial() *Tutorial {
	return &Tutorial{
		ID:    "diamond",
		Title: "Diamond",
		Steps: []Step{
			{ID: "root", Instruction: "start"},
			{ID: "left", Instruction: "l", Prerequisites: []string{"root"}},
			{ID: "right", Instruction: "r", Prerequisites: []string{"root"}},
			{ID: "merge", Instruction: "m", Prerequisites: []string{"left", "right"}},
		},
	}
}

func TestProgress_HappyPath(t *testing.T) {
	tut := buildLinearTutorial()
	p, err := NewProgress(tut)
	if err != nil {
		t.Fatal(err)
	}

	// s1 is immediately available
	avail := p.AvailableSteps()
	if len(avail) != 1 || avail[0] != "s1" {
		t.Fatalf("expected [s1], got %v", avail)
	}

	if err := p.Start("s1"); err != nil {
		t.Fatalf("start s1: %v", err)
	}
	if err := p.Complete("s1"); err != nil {
		t.Fatalf("complete s1: %v", err)
	}

	// s2 now available
	avail = p.AvailableSteps()
	if len(avail) != 1 || avail[0] != "s2" {
		t.Fatalf("expected [s2], got %v", avail)
	}

	if err := p.Complete("s2"); err != nil {
		t.Fatalf("complete s2: %v", err)
	}
	if err := p.Complete("s3"); err != nil {
		t.Fatalf("complete s3: %v", err)
	}

	if !p.IsComplete() {
		t.Error("expected tutorial complete")
	}
	done, total := p.CompletedCount()
	if done != 3 || total != 3 {
		t.Errorf("completed %d/%d", done, total)
	}
}

func TestProgress_DependencyGating(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)

	// Cannot start s2 before s1 is done
	err := p.Start("s2")
	if err == nil {
		t.Fatal("should fail to start s2 without s1 completed")
	}

	// Cannot complete s3 before s2
	err = p.Complete("s3")
	if err == nil {
		t.Fatal("should fail to complete s3 without s2 completed")
	}
}

func TestProgress_DiamondGating(t *testing.T) {
	tut := buildDiamondTutorial()
	p, _ := NewProgress(tut)

	_ = p.Complete("root")

	// merge requires both left and right
	err := p.Complete("merge")
	if err == nil {
		t.Fatal("merge should be gated by left+right")
	}

	_ = p.Complete("left")
	// still gated by right
	err = p.Complete("merge")
	if err == nil {
		t.Fatal("merge should still be gated by right")
	}

	_ = p.Complete("right")
	// now merge should succeed
	if err := p.Complete("merge"); err != nil {
		t.Fatalf("merge should succeed: %v", err)
	}
	if !p.IsComplete() {
		t.Error("expected complete")
	}
}

func TestProgress_SnapshotRestore(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	_ = p.Complete("s1")
	_ = p.Start("s2")

	snap, err := p.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	restored, err := RestoreProgress(tut, snap)
	if err != nil {
		t.Fatal(err)
	}

	// Check states match
	st1, _ := restored.State("s1")
	st2, _ := restored.State("s2")
	st3, _ := restored.State("s3")

	if st1 != StateCompleted {
		t.Errorf("restored s1: got %q, want completed", st1)
	}
	if st2 != StateInProgress {
		t.Errorf("restored s2: got %q, want in_progress", st2)
	}
	if st3 != StateNotStarted {
		t.Errorf("restored s3: got %q, want not_started", st3)
	}

	// Can resume from restored state
	if err := restored.Complete("s2"); err != nil {
		t.Fatalf("resume s2: %v", err)
	}
}

func TestProgress_RestoreWrongTutorial(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	snap, _ := p.MarshalSnapshot()

	other := &Tutorial{ID: "other", Title: "O", Steps: []Step{{ID: "x", Instruction: "y"}}}
	_, err := RestoreProgress(other, snap)
	if err == nil {
		t.Fatal("expected error restoring snapshot for wrong tutorial")
	}
}

func TestProgress_ConcurrentSafety(t *testing.T) {
	tut := buildDiamondTutorial()
	p, _ := NewProgress(tut)
	_ = p.Complete("root")

	var wg sync.WaitGroup
	wg.Add(200)
	for i := 0; i < 100; i++ {
		go func() {
			defer wg.Done()
			_ = p.Complete("left")
		}()
		go func() {
			defer wg.Done()
			_ = p.Complete("right")
		}()
	}
	wg.Wait()

	// After both left and right are complete, merge should succeed
	if err := p.Complete("merge"); err != nil {
		t.Fatalf("merge: %v", err)
	}
}

func TestProgress_Idempotent(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)

	if err := p.Complete("s1"); err != nil {
		t.Fatal(err)
	}
	// Complete again — should be idempotent
	if err := p.Complete("s1"); err != nil {
		t.Fatalf("idempotent complete s1: %v", err)
	}
}

func TestProgress_UnknownStep(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)

	_, err := p.State("nope")
	if err == nil {
		t.Error("expected error for unknown step")
	}
	err = p.Start("nope")
	if err == nil {
		t.Error("expected error for unknown step")
	}
}

func TestProgress_SnapshotIsValidJSON(t *testing.T) {
	tut := buildLinearTutorial()
	p, _ := NewProgress(tut)
	_ = p.Complete("s1")

	snap, err := p.MarshalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(snap) {
		t.Error("snapshot is not valid JSON")
	}
}
