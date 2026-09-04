# Package evidence - Verifiable Control Plane

## Purpose

签名的、哈希链接的、独立可验证的控制平面行动账本。它是平台的"真的吗？"的答案：

> 这个 action 真的在当前运行了吗，还是被降级/模拟了？

成熟平台在结构上是"信任我们"，而这个账本是"证明它"：每个记录的 action 都会产生一个收据，说明哪个真实 vs 模拟的后端执行了它（来自 pkg/capability），哈希其输入和输出，链式链接到前一个收据（防篡改），并使用 Ed25519 签名。任何第三方都可以离线验证链和签名，针对已发布公钥 — 参见 Verifier 和 cmd/cafctl verify。

## Key Files

| File | Lines | Purpose |
|------|-------|---------|
| `ledger.go` | 196 | Append-only chain with Ed25519 signatures |
| `evidence.go` | 178 | Evidence data structure definitions |
| `completeness.go` | 271 | Namespace-parametric completeness proofs (A0) |
| `zk/prover.go` | 220 | Zero-knowledge proof generation (A1) |
| `batch_merkle.go` | 369 | Batch signing with Merkle tree optimization |
| `parallel_verify.go` | 238 | Parallel verification for large chains |

## Dependencies

```
Imports: 
  ↓ github.com/cloudai-fusion/cloudai-fusion/pkg/capability
  ↓ crypto/ed25519
  ↓ github.com/google/go-tdigest/tdigest
  
Imported by:
  → cmd/apiserver/router.go
  → pkg/redteam/manager.go  
  → pkg/finops/pricing.go
  → pkg/scheduler/leader_election.go
```

## Extension Points

✅ **Add new ZK Provers**: Implement `Prover` interface in a new file (no core modification needed)
```go
type MyCustomProver struct{}
func (p MyCustomProver) Prove(...) { /* implement */ }
func (p MyCustomProver) Mode() capability.Mode { return capability.ModeReal }
```

✅ **Plug different Store implementations**: Just implement `Store` interface
```go
type CustomStore struct{}
func (s CustomStore) Append(...) error { /* implement */ }
```

✅ **Replace Anchorer without changing core logic**: Implement `Anchorer` interface
```go
type CustomAnchorer struct{}
func (a CustomAnchorer) Anchor(...) (*TransparencyRef, error) { /* implement */ }
```

## Architecture Overview

```
┌─────────────────────────────────────┐
│ Recorder Interface                   │
│ - Record(ctx, RecordInput)            │
└──────────┬──────────────────────────┘
           │
           ↓
┌─────────────────────────────────────┐
│ Ledger Core                          │
│ - Hash-chained append                │
│ - Ed25519 signing                    │
│ - Capability injection               │
└──┬───────────────────────┬──────────┘
   │                       │
   ↓                       ↓
┌────────────┐      ┌──────────────┐
│ Store Impl │      │ Anchorer     │
│ (SQLite/PG)│      │ (Rekor/Local)│
└────────────┘      └──────────────┘

Optional Extensions:
┌────────────┐
│ Prover Impl│ (ZK proof types)
│(Groth16/etc)│
└────────────┘
```

## Security Model

- **Tamper-evident**: Any modification breaks hash chain
- **Cryptographic signatures**: Ed25519 over each record's hash
- **Independent verifiability**: Offline verification without platform access
- **Capability honesty**: Runtime enforcement of real vs simulated backends

## Usage Example

```go
// Create ledger with in-memory store for testing
signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x42}, 32))
store := evidence.NewMemoryStore()
l, err := evidence.NewLedger(evidence.LedgerConfig{
    Store:  store,
    Signer: signer,
})
if err != nil {
    log.Fatal(err)
}

// Record actions
ctx := context.Background()
rec, err := l.Record(ctx, evidence.RecordInput{
    Actor:   "scheduler",
    Action:  "schedule.bind",
    Subject: "workload-123",
    Payload: map[string]any{"priority": 5},
})
if err != nil {
    log.Fatal(err)
}

// Verify the chain
report := evidence.VerifyChain([]*evidence.Evidence{rec}, signer.PublicKey())
if report.OK() {
    fmt.Println("✓ Chain is valid")
} else {
    fmt.Printf("✗ Chain invalid: %v\n", report.Error)
}
```

## Testing

Run all tests:
```bash
go test -v ./pkg/evidence/...
```

Run with race detector:
```bash
go test -race ./pkg/evidence/...
```

## Specification References

- Moat A, Layer A0 (Completeness): See `docs/verifiable-moat-spec.md §3.1`
- Moat A, Layer A1 (Zero-knowledge): See `docs/verifiable-moat-spec.md §3.2`
- Receipt Structure: See `docs/verifiable-moat-spec.md §2.1`

---

*Package: github.com/cloudai-fusion/cloudai-fusion/pkg/evidence  
Version: v0.1.0  
Last Updated: September 4, 2026*
