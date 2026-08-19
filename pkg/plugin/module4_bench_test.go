package plugin

// ============================================================================
// Module 4 — Plugin Ecosystem performance-wall benchmarks
//
// These benchmarks measure the five dimensions used to argue an in-process
// performance wall against out-of-process plugin runtimes (HashiCorp
// go-plugin, Docker plugins, Envoy WASM filters):
//
//  1. Hot add / hot remove latency        (Registry.Add / Registry.Remove)
//  2. Capability authorization latency     (SecurityManager.Allow)
//  3. Panic-isolation recovery overhead    (SafeCall normal vs panic path)
//  4. Marketplace submission verification   (GPG verify + Poseidon + semver,
//                                            each isolated + the full gateway)
//  5. Concurrent hot-load throughput        (10 plugins Add+Remove in parallel)
//
// Honesty note: every number here is an *in-process* Go call. The comparison
// documented in docs/performance-validation-module-4.md contrasts this against
// runtimes that pay process-boundary / IPC costs — a DIFFERENT isolation
// level, not a like-for-like feature race. See that doc for the caveats.
//
// Run:
//   go test ./pkg/plugin/... -bench=. -benchmem -run '^$'
// ============================================================================

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

// ----------------------------------------------------------------------------
// 1. Hot add / hot remove latency
// ----------------------------------------------------------------------------

// BenchmarkHotAdd isolates the cost of hot-registering an already-constructed
// plugin (the simple Add path: no Init/Start, no resource controller). The
// Remove that frees the name is excluded from the timer.
func BenchmarkHotAdd(b *testing.B) {
	r := NewRegistry()
	p := newProbe("bench-add", ExtSchedulerScore)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := r.Add("bench-add", p); err != nil {
			b.Fatalf("add: %v", err)
		}
		b.StopTimer()
		if err := r.Remove("bench-add"); err != nil {
			b.Fatalf("remove: %v", err)
		}
		b.StartTimer()
	}
}

// BenchmarkHotRemove isolates the cost of hot-unregistering a plugin (drives
// Stop under recover, releases index). The Add that re-creates it is excluded.
func BenchmarkHotRemove(b *testing.B) {
	r := NewRegistry()
	p := newProbe("bench-remove", ExtSchedulerScore)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := r.Add("bench-remove", p); err != nil {
			b.Fatalf("add: %v", err)
		}
		b.StartTimer()
		if err := r.Remove("bench-remove"); err != nil {
			b.Fatalf("remove: %v", err)
		}
	}
}

// BenchmarkHotAddRemoveCycleWithLifecycle measures a full swap-in/swap-out
// cycle that runs the plugin's Init, Start and Stop under panic recovery — the
// realistic cost of live-reloading a plugin without restarting the host.
func BenchmarkHotAddRemoveCycleWithLifecycle(b *testing.B) {
	r := NewRegistry()
	ctx := context.Background()
	cfg := map[string]interface{}{"k": "v"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := newProbe("bench-cycle", ExtSchedulerScore)
		if err := r.AddWithOptions(ctx, "bench-cycle", p, AddOptions{Config: cfg}); err != nil {
			b.Fatalf("add: %v", err)
		}
		if err := r.Remove("bench-cycle"); err != nil {
			b.Fatalf("remove: %v", err)
		}
	}
}

// ----------------------------------------------------------------------------
// 2. Capability authorization latency
// ----------------------------------------------------------------------------

func benchSecurityManager(b *testing.B) *SecurityManager {
	b.Helper()
	// File audit disabled: measure the authorization decision + the in-memory
	// audit ring that is always maintained. The optional file sink adds one
	// buffered write per call and is intentionally excluded.
	sm, err := NewSecurityManager(SecurityConfig{DisableFileAudit: true})
	if err != nil {
		b.Fatalf("new security manager: %v", err)
	}
	if err := sm.Grant(CapabilityPolicy{
		PluginName:  "bench-plugin",
		Permissions: []string{"read:cluster", "read:pods", "write:metrics"},
		DenyList:    []string{"write:secrets"},
	}); err != nil {
		b.Fatalf("grant: %v", err)
	}
	return sm
}

// BenchmarkAllowGranted measures the allowed path: a granted capability matches.
func BenchmarkAllowGranted(b *testing.B) {
	sm := benchSecurityManager(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !sm.Allow("bench-plugin", "read:pods") {
			b.Fatal("expected allow")
		}
	}
}

// BenchmarkAllowDeniedExplicit measures the DenyList path (explicit refusal).
func BenchmarkAllowDeniedExplicit(b *testing.B) {
	sm := benchSecurityManager(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if sm.Allow("bench-plugin", "write:secrets") {
			b.Fatal("expected deny")
		}
	}
}

// BenchmarkAllowDeniedNoPolicy measures deny-by-default for an unknown plugin
// (no policy registered at all) — the cheapest refusal.
func BenchmarkAllowDeniedNoPolicy(b *testing.B) {
	sm := benchSecurityManager(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if sm.Allow("ghost-plugin", "read:pods") {
			b.Fatal("expected deny")
		}
	}
}

// BenchmarkCheckNoAudit measures the pre-flight Check path, which evaluates the
// policy without writing an audit record — the pure decision cost.
func BenchmarkCheckNoAudit(b *testing.B) {
	sm := benchSecurityManager(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !sm.Check("bench-plugin", "read:pods").Allowed() {
			b.Fatal("expected allow")
		}
	}
}

// ----------------------------------------------------------------------------
// 3. Panic-isolation recovery overhead
// ----------------------------------------------------------------------------

// BenchmarkSafeCallNormal measures the deferred-recover overhead on the normal
// (no-panic) path — the tax every plugin call pays for isolation.
func BenchmarkSafeCallNormal(b *testing.B) {
	fn := func() error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := SafeCall("bench", "op", fn); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// BenchmarkSafeCallPanic measures the panic path: recover + build an
// *ErrPluginPanic including a captured stack trace. This is the cost of
// quarantining a misbehaving plugin, paid only when one actually panics.
func BenchmarkSafeCallPanic(b *testing.B) {
	fn := func() error { panic("bench panic") }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := SafeCall("bench", "op", fn); err == nil {
			b.Fatal("expected panic to be recovered as error")
		}
	}
}

// BenchmarkDirectCallBaseline is the reference: calling the same closure with
// no SafeCall wrapper, so the SafeCall overhead can be read as the delta.
func BenchmarkDirectCallBaseline(b *testing.B) {
	fn := func() error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := fn(); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}

// ----------------------------------------------------------------------------
// 4. Marketplace submission verification
// ----------------------------------------------------------------------------

// benchArtifactSize is the representative plugin-artifact size the marketplace
// benchmarks sign and commit over. GPG verification and the Poseidon
// commitment both hash the whole artifact with SHA-256, so their cost scales
// with this size; it is stated explicitly so numbers can be extrapolated.
const benchArtifactSize = 64 * 1024 // 64 KiB

func benchArtifact() []byte {
	a := make([]byte, benchArtifactSize)
	for i := range a {
		a[i] = byte(i * 31)
	}
	return a
}

// benchGenKeyAndSign generates a fresh OpenPGP entity, returns its armored
// public key, and produces an armored detached signature over the artifact.
// Key generation is done once, before the timer starts.
func benchGenKeyAndSign(b *testing.B, artifact []byte) (armoredPub, armoredSig string) {
	b.Helper()
	entity, err := openpgp.NewEntity("Bench Signer", "module-4-benchmark", "bench@cloudai-fusion.io", nil)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}
	var pubBuf bytes.Buffer
	w, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		b.Fatalf("armor encode: %v", err)
	}
	if err := entity.Serialize(w); err != nil {
		b.Fatalf("serialize public key: %v", err)
	}
	_ = w.Close()

	var sigBuf bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&sigBuf, entity, bytes.NewReader(artifact), nil); err != nil {
		b.Fatalf("detached sign: %v", err)
	}
	return pubBuf.String(), sigBuf.String()
}

// BenchmarkGPGVerify isolates the detached-signature verification cost over a
// benchArtifactSize artifact (dominated by SHA-256 of the artifact + one
// public-key operation).
func BenchmarkGPGVerify(b *testing.B) {
	artifact := benchArtifact()
	pub, sig := benchGenKeyAndSign(b, artifact)
	v, err := NewOpenPGPVerifier(pub)
	if err != nil {
		b.Fatalf("new verifier: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := v.Verify(artifact, sig); err != nil {
			b.Fatalf("verify: %v", err)
		}
	}
}

// BenchmarkPoseidonCommitment isolates the supply-chain commitment cost: two
// SHA-256 hashes (namespace + payload) folded into a BN254 Poseidon2
// Merkle–Damgard commitment via pkg/evidence/zk.
func BenchmarkPoseidonCommitment(b *testing.B) {
	artifact := benchArtifact()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = PoseidonCommitment("bench-plugin", "1.2.3", artifact)
	}
}

// BenchmarkSemverCheck isolates the version-compatibility gate (parse both
// versions + precedence comparison).
func BenchmarkSemverCheck(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CheckVersionCompatibility("1.2.3", "1.3.0"); err != nil {
			b.Fatalf("semver: %v", err)
		}
	}
}

// benchExternalManifest builds a valid manifest for the external gateway path.
func benchExternalManifest() PluginManifest {
	return PluginManifest{
		APIVersion: "v1",
		Kind:       "CloudAIPlugin",
		Metadata: Metadata{
			Name:            "bench-plugin",
			Version:         "1.2.3",
			Author:          "bench",
			License:         "Apache-2.0",
			Description:     "benchmark manifest",
			ExtensionPoints: []ExtensionPoint{ExtSchedulerScore},
		},
		Spec: PluginSpec{
			GoModule:    "example.com/bench",
			EntryPoint:  "New",
			Permissions: []string{"read:cluster"},
		},
	}
}

// BenchmarkSubmissionGatewayExternal measures the full external-channel gateway:
// manifest validation + artifact digest + semver + GPG verify + Poseidon
// commitment + permission review, i.e. every automated gate a community
// submission passes through.
func BenchmarkSubmissionGatewayExternal(b *testing.B) {
	artifact := benchArtifact()
	pub, sig := benchGenKeyAndSign(b, artifact)
	v, err := NewOpenPGPVerifier(pub)
	if err != nil {
		b.Fatalf("new verifier: %v", err)
	}
	gw := NewSubmissionGateway(GatewayConfig{
		Verifier:           v,
		AllowedPermissions: []string{"read:cluster", "read:pods"},
	})
	commitment := PoseidonCommitment("bench-plugin", "1.2.3", artifact)
	sub := Submission{
		Channel:          ChannelExternal,
		Manifest:         benchExternalManifest(),
		Artifact:         artifact,
		Submitter:        "bench",
		ArmoredSignature: sig,
		Commitment:       commitment,
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		review, err := gw.Submit(ctx, sub)
		if err != nil {
			b.Fatalf("submit: %v", err)
		}
		if review.Status != SubmissionApproved {
			b.Fatalf("status = %s, failed = %v", review.Status, review.FailedChecks())
		}
	}
}

// BenchmarkSubmissionGatewayInternal measures the first-party channel: the same
// gates minus GPG/Poseidon, gated instead on a CI attestation digest match.
func BenchmarkSubmissionGatewayInternal(b *testing.B) {
	artifact := benchArtifact()
	gw := NewSubmissionGateway(GatewayConfig{
		AllowedPermissions: []string{"read:cluster", "read:pods"},
	})
	sub := Submission{
		Channel:   ChannelInternal,
		Manifest:  benchExternalManifest(),
		Artifact:  artifact,
		Submitter: "platform-team",
		CI: &CIAttestation{
			Pipeline:       "plugin-ci",
			RunID:          "12345",
			Commit:         "deadbeef",
			ArtifactSHA256: ArtifactDigest(artifact),
			TestsPassed:    true,
			TestCount:      112,
			Coverage:       0.87,
		},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		review, err := gw.Submit(ctx, sub)
		if err != nil {
			b.Fatalf("submit: %v", err)
		}
		if review.Status != SubmissionApproved {
			b.Fatalf("status = %s, failed = %v", review.Status, review.FailedChecks())
		}
	}
}

// ----------------------------------------------------------------------------
// 5. Concurrent hot-load throughput
// ----------------------------------------------------------------------------

const benchConcurrentPlugins = 10

// BenchmarkConcurrentHotAddRemove measures throughput of benchConcurrentPlugins
// distinct plugins being hot-added and hot-removed in parallel per iteration.
// It reports a plugins/sec metric so the number is legible next to the
// per-process spawn cost of an out-of-process runtime. Names are unique per
// worker and each is removed before the batch completes, so iterations do not
// collide.
func BenchmarkConcurrentHotAddRemove(b *testing.B) {
	r := NewRegistry()
	names := make([]string, benchConcurrentPlugins)
	for i := range names {
		names[i] = fmt.Sprintf("cc-%02d", i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		errs := make(chan error, benchConcurrentPlugins)
		for w := 0; w < benchConcurrentPlugins; w++ {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				p := newProbe(name, ExtSchedulerScore)
				if err := r.Add(name, p); err != nil {
					errs <- err
					return
				}
				if err := r.Remove(name); err != nil {
					errs <- err
				}
			}(names[w])
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			b.Fatalf("concurrent add/remove: %v", err)
		}
	}
	// Each iteration performs benchConcurrentPlugins add+remove pairs.
	total := float64(b.N) * float64(benchConcurrentPlugins)
	b.ReportMetric(total/b.Elapsed().Seconds(), "addremove/s")
}
