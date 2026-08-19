package evidence

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// batch_append.go pipelines high-throughput ingestion of the append-only chain.
//
// An Ed25519-signed hash chain has an irreducibly SEQUENTIAL critical section:
// each receipt's Hash commits to the previous receipt's Hash (PrevHash), and the
// signature is over that Hash — so signing and chaining cannot be reordered or
// parallelized without breaking tamper-evidence. What CAN run ahead of the
// critical section is everything that depends only on the caller's input:
// canonicalizing the payload and computing the SHA-256 InputHash/OutputHash.
//
// BatchRecord therefore precomputes those content hashes for the whole batch in
// PARALLEL, then walks the batch through the same sequential sign+append path
// Record uses. This overlaps the batch's hashing/marshaling with the signing
// critical path — an honest, correctness-preserving speedup. It deliberately
// does NOT parallelize signing (that would corrupt the chain).

// preparedInput is the position-independent part of a receipt: everything that
// can be computed from RecordInput alone, before the chain head is known. It is
// produced by prepareRecordInput (a pure function, safe to run concurrently) and
// consumed by appendPrepared inside the sequential critical section.
type preparedInput struct {
	actor      string
	action     string
	subject    string
	inputHash  string
	outputHash string
	payload    json.RawMessage
	backends   []BackendFact // explicit backends (nil => snapshot components)
	components []string
}

// prepareRecordInput computes the content hashes and canonical payload for one
// input. It is pure (no ledger/chain state, no I/O) and therefore safe to invoke
// from many goroutines at once — the basis for BatchRecord's parallel precompute.
func prepareRecordInput(in RecordInput) (preparedInput, error) {
	inputHash, err := HashAny(in.Input)
	if err != nil {
		return preparedInput{}, err
	}
	outputHash, err := HashAny(in.Output)
	if err != nil {
		return preparedInput{}, err
	}
	var payload json.RawMessage
	if in.Payload != nil {
		b, mErr := marshalCanonical(in.Payload)
		if mErr != nil {
			return preparedInput{}, mErr
		}
		payload = b
	}
	return preparedInput{
		actor:      in.Actor,
		action:     in.Action,
		subject:    in.Subject,
		inputHash:  inputHash,
		outputHash: outputHash,
		payload:    payload,
		backends:   in.Backends,
		components: in.Components,
	}, nil
}

// appendPrepared runs the sequential critical section: it resolves backends,
// assembles the record against the current chain head, computes its leaf hash,
// signs it, anchors it, and durably appends it. It mirrors the pre-refactor
// Record body exactly, so Record and BatchRecord produce byte-identical chains.
func (l *Ledger) appendPrepared(ctx context.Context, p preparedInput) (*Evidence, error) {
	backends := p.backends
	if backends == nil {
		backends = l.snapshotBackends(p.components)
	}

	// build assembles, hashes, signs, and anchors a record given the current
	// chain head. It runs either under the ledger mutex or inside the store's
	// atomic append, so Seq/PrevHash assignment is race-free.
	sgn := l.currentSigner()
	build := func(last *Evidence) (*Evidence, error) {
		prev := GenesisPrevHash
		var seq uint64 = 1
		if last != nil {
			prev = last.Hash
			seq = last.Seq + 1
		}
		e := &Evidence{
			ID:         common.NewUUID(),
			Seq:        seq,
			PrevHash:   prev,
			Timestamp:  time.Now().UTC(),
			Actor:      p.actor,
			Action:     p.action,
			Subject:    p.subject,
			RunMode:    l.cap.RunMode(),
			Backends:   backends,
			InputHash:  p.inputHash,
			OutputHash: p.outputHash,
			Payload:    p.payload,
		}
		hash, herr := e.ComputeHash()
		if herr != nil {
			return nil, herr
		}
		e.Hash = hash
		// Sign over the hex leaf hash; the Verifier signs/verifies the same bytes.
		sig, serr := sgn.Sign([]byte(hash))
		if serr != nil {
			return nil, serr
		}
		e.Signature = sig
		e.KeyID = sgn.KeyID()
		// Anchor is best-effort but always truthful. NOTE: the simulated anchor is
		// a pure struct assignment (safe inside a DB tx); Phase 5's real Rekor
		// client must anchor post-commit and update LogEntry, which is excluded
		// from the signed content by design.
		if ref, aerr := l.anchorer.Anchor(ctx, AnchorRequest{LeafHex: hash, SignatureB64: sig, PublicKey: sgn.PublicKey()}); aerr == nil {
			e.LogEntry = ref
		} else {
			e.LogEntry = &TransparencyRef{Backend: "simulated", Detail: aerr.Error(), IntegratedAt: time.Now().UTC()}
		}
		return e, nil
	}

	// Prefer the store's atomic append (race-free across concurrent writers, e.g.
	// multiple processes sharing a DB); otherwise serialize within this process.
	if as, ok := l.store.(AtomicStore); ok {
		e, aerr := as.AppendChained(ctx, build)
		if aerr != nil {
			return nil, aerr
		}
		observeRecord(e)
		return e, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	last, err := l.store.Last(ctx)
	if err != nil {
		return nil, err
	}
	e, err := build(last)
	if err != nil {
		return nil, err
	}
	if err := l.store.Append(ctx, e); err != nil {
		return nil, err
	}
	observeRecord(e)
	return e, nil
}

// BatchRecord appends a batch of receipts, precomputing every record's content
// hashes (InputHash/OutputHash) and canonical payload in PARALLEL, then signing
// and appending them SEQUENTIALLY to preserve chain integrity.
//
// Correctness contract:
//   - The precompute phase is pure (prepareRecordInput) and touches no shared
//     ledger state, so it is race-free.
//   - Signing/chaining stays strictly sequential and in input order, so the
//     resulting sub-chain is identical to calling Record for each input in turn.
//
// The batch is NOT guaranteed to be contiguous in the global chain: if other
// goroutines call Record concurrently, their receipts may interleave. Each
// individual append is still atomic and the chain stays valid; "batch" here
// means "these inputs were ingested together", not "these Seqs are adjacent".
// On the first append error, BatchRecord returns the receipts appended so far
// (which are already durable) along with the error.
func (l *Ledger) BatchRecord(ctx context.Context, inputs []RecordInput) ([]*Evidence, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	// Phase 1: parallel, position-independent precompute (SHA-256 + JSON).
	prepared := make([]preparedInput, len(inputs))
	errs := make([]error, len(inputs))

	workers := runtime.NumCPU()
	if workers > len(inputs) {
		workers = len(inputs)
	}
	var wg sync.WaitGroup
	batch := (len(inputs) + workers - 1) / workers
	for w := 0; w < workers; w++ {
		start := w * batch
		if start >= len(inputs) {
			break
		}
		end := start + batch
		if end > len(inputs) {
			end = len(inputs)
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				prepared[i], errs[i] = prepareRecordInput(inputs[i])
			}
		}(start, end)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	// Phase 2: sequential sign + chain + append (the irreducible critical path).
	results := make([]*Evidence, 0, len(inputs))
	for i := range prepared {
		e, err := l.appendPrepared(ctx, prepared[i])
		if err != nil {
			return results, err
		}
		results = append(results, e)
	}
	return results, nil
}
