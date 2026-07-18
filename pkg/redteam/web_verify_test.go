package redteam

import (
	"context"
	"testing"
)

func TestReplayAndVerify_MatchesFaithfulChain(t *testing.T) {
	srv := vulnServer()
	defer srv.Close()
	ctx := context.Background()
	chain := authBypassChain(srv.URL)

	prior, err := RunWebChain(ctx, srv.Client(), chain)
	if err != nil || !prior.Success {
		t.Fatalf("prior run must succeed: err=%v", err)
	}
	// A faithful replay reproduces every request fingerprint.
	ok, err := ReplayAndVerify(ctx, srv.Client(), chain, prior)
	if err != nil || !ok {
		t.Fatalf("faithful chain must replay-verify: ok=%v err=%v", ok, err)
	}
	// Tampering with a recorded request hash must fail verification.
	prior.Steps[0].RequestHash = "deadbeef"
	if ok, _ := ReplayAndVerify(ctx, srv.Client(), chain, prior); ok {
		t.Fatal("a tampered transcript must NOT replay-verify")
	}
}
