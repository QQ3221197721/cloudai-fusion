package evidence

import (
	"context"
	"encoding/json"
	"math/rand"
	"testing"
)

// bumpHex returns a hex string with its first nibble changed (still valid hex,
// guaranteed different), used to corrupt a stored hash/root without breaking JSON.
func bumpHex(s string) string {
	if s == "" {
		return "0"
	}
	first := byte('0')
	if s[0] == '0' {
		first = '1'
	}
	return string(first) + s[1:]
}

// TestChaos_CorruptionNeverFalseAccepts is a fuzz-style chaos test: it takes a
// valid exported bundle and applies a random corruption (flip a payload byte,
// tamper a hash/signature/checkpoint, drop a record, reorder records) hundreds of
// times, asserting the verifier NEVER falsely accepts a corrupted ledger. A single
// false-accept would break the platform's entire "prove it" promise.
func TestChaos_CorruptionNeverFalseAccepts(t *testing.T) {
	l := scaleLedger(t, NewMemoryStore())
	recordN(t, l, 20)
	base, err := l.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	baseBytes, _ := json.Marshal(base)

	rng := rand.New(rand.NewSource(1))
	const iters = 400
	applied := 0
	for iter := 0; iter < iters; iter++ {
		var b ExportBundle
		if err := json.Unmarshal(baseBytes, &b); err != nil {
			t.Fatalf("clone bundle: %v", err)
		}

		switch rng.Intn(6) {
		case 0: // flip a byte in a random record's payload
			idx := rng.Intn(len(b.Records))
			p := append([]byte(nil), b.Records[idx].Payload...)
			if len(p) == 0 {
				continue
			}
			p[rng.Intn(len(p))] ^= 0x01
			b.Records[idx].Payload = p
		case 1: // tamper a stored leaf hash
			idx := rng.Intn(len(b.Records))
			b.Records[idx].Hash = bumpHex(b.Records[idx].Hash)
		case 2: // tamper a signature
			idx := rng.Intn(len(b.Records))
			b.Records[idx].Signature = "AA" + b.Records[idx].Signature
		case 3: // drop a record
			idx := rng.Intn(len(b.Records))
			b.Records = append(b.Records[:idx], b.Records[idx+1:]...)
		case 4: // reorder two adjacent records
			if len(b.Records) < 2 {
				continue
			}
			i := rng.Intn(len(b.Records) - 1)
			b.Records[i], b.Records[i+1] = b.Records[i+1], b.Records[i]
		case 5: // tamper the signed checkpoint root
			if b.Checkpoint == nil {
				continue
			}
			b.Checkpoint.RootHash = bumpHex(b.Checkpoint.RootHash)
		}

		applied++
		rep, err := VerifyBundle(&b)
		if err == nil && rep.Valid {
			t.Fatalf("iter %d: a corrupted bundle was FALSELY ACCEPTED: %+v", iter, rep)
		}
	}
	if applied == 0 {
		t.Fatal("no corruptions were applied")
	}

	// Sanity: the untouched base bundle must still verify (no false-reject).
	var clean ExportBundle
	_ = json.Unmarshal(baseBytes, &clean)
	if rep, _ := VerifyBundle(&clean); !rep.Valid {
		t.Fatalf("the pristine bundle must verify (no false-reject), got %+v", rep)
	}
}
