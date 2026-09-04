import re, statistics

baseline = []
async_t = []
merkle = []

files = [('output/m33_isolated/baseline.json', baseline), 
         ('output/m33_isolated/merkle.json', merkle),
         ('output/m33_isolated/pureasync_seq.json', async_t)]

for f, t in files:
    with open(f, 'r', encoding='utf-8-sig') as fh:
        for line in fh:
            if 'ns/op' in line:
                m = re.search(r'(\d+)\s+ns/op', line)
                if m: t.append(int(m.group(1)))

print("="*60)
print("M33 PURE ASYNC BENCHMARK RESULTS")
print("="*60)

mb = statistics.median(baseline) if baseline else None
mm = statistics.median(merkle) if merkle else None
ma = statistics.median(async_t) if async_t else None

print("\n1. Baseline median:", mb if mb else "FAILED")
print("2. Merkle median:", mm if mm else "FAILED")  
print("3. Pure Async median:", ma if ma else "FAILED")

if ma and mb:
    speedup = mb / ma
    print("\nSpeedup vs baseline:", round(speedup, 2), "x")
    verdict = "FLIPPED" if speedup >= 10 else "SIGNIFICANT" if speedup >= 5 else "MODERATE" if speedup >= 2 else "MINIMAL"
    print("Verdict:", verdict)

print("\n4. VerifyChain: BLOCKED (expected - hangs on flush)")

print("\nSUMMARY:")
print("- Baseline (per-record):", format(mb or 'N/A', ',15'))
print("- Merkle Batch:", format(mm or 'N/A', ',15'))
print("- Pure Async:", format(ma or 'N/A', ',15'))

if ma and mb:
    imp = ((mb-ma)/mb)*100
    print("\nImprovement:", round(imp, 1), "% FASTER")
    
    trivy = 2_300_000
    print("\nVs Trivy ({:,} ns/op):".format(trivy))
    if ma < trivy:
        ts = trivy / ma
        print("  BEATS TRIVY by {:.2f}x!".format(ts))
    else:
        print("  Slower by {:.2f}x".format(ma/trivy))
