package fabric_test

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
)

// seal_guard_test.go proves M0's "no post-seal actions" for the generic wells: a
// fresh seal is intact, and any action recorded AFTER the seal is detected as
// post-seal activity that breaks seal integrity (the seal-then-keep-acting attempt).

func TestFabric_PostSealDetection(t *testing.T) {
	ctx := context.Background()
	l := newLedger(t) // helper from fabric_test.go (same test package)
	f := fabric.New(l)

	well := fabric.Well{
		Name:   "demo",
		Prefix: "demo",
		Actor:  "demo",
		KeyOf: func(e *evidence.Evidence) string {
			if e.Action == "demo.act" {
				return e.Subject
			}
			return ""
		},
	}
	if err := f.Register(well); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := func(subj string) {
		if _, err := l.Record(ctx, evidence.RecordInput{
			Actor: "demo", Action: "demo.act", Subject: subj, Payload: map[string]any{"s": subj},
		}); err != nil {
			t.Fatalf("record %s: %v", subj, err)
		}
	}
	rec("k1")
	rec("k1")

	// Before sealing: not sealed, and an unsealed namespace is not "intact".
	if sealed, _ := f.Sealed(ctx, "demo", "k1"); sealed {
		t.Fatal("namespace must not be sealed yet")
	}
	if intact, _ := f.SealIntact(ctx, "demo", "k1"); intact {
		t.Fatal("an unsealed namespace must not report intact")
	}

	// Seal -> sealed, no post-seal members, intact.
	if _, err := f.Seal(ctx, "demo", "k1"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed, _ := f.Sealed(ctx, "demo", "k1"); !sealed {
		t.Fatal("namespace must be sealed after Seal")
	}
	if post, _ := f.PostSealMembers(ctx, "demo", "k1"); len(post) != 0 {
		t.Fatalf("fresh seal post-seal members = %d, want 0", len(post))
	}
	if intact, _ := f.SealIntact(ctx, "demo", "k1"); !intact {
		t.Fatal("a fresh seal must be intact")
	}

	// Act AFTER the seal -> detected as post-seal activity; integrity broken.
	rec("k1")
	post, err := f.PostSealMembers(ctx, "demo", "k1")
	if err != nil {
		t.Fatalf("post-seal: %v", err)
	}
	if len(post) != 1 {
		t.Fatalf("post-seal members = %d, want 1 (the after-seal action)", len(post))
	}
	if intact, _ := f.SealIntact(ctx, "demo", "k1"); intact {
		t.Fatal("a post-seal action MUST break seal integrity")
	}

	// A different, never-sealed key is unaffected.
	if sealed, _ := f.Sealed(ctx, "demo", "k2"); sealed {
		t.Fatal("k2 was never sealed")
	}
}
