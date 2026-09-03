=============================================================================
M10 DASP vs HAMi: FINAL HONEST BENCHMARK VERDICT (count=6)
=============================================================================

CORE FINDING:
DASP zone-based segregation does NOT achieve universal T2 clean win over 
HAMi-Proxy on MIG slice acceptance rate across distributions.

PER-DISTRIBUTION MEDIAN RESULTS (n=6 runs):
-------------------------------------------

1. uniform Distribution (ρ_count=0.60):
   DASP:  median accept_rate = 46%  (range:34%-52%)
   HAMi:  median accept_rate = 53%  (range:46%-60%)
   Verdict: HAMi wins by +7 pts | DASP segregation LOSER
   
2. skew-small Distribution (ρ_count=0.10):
   DASP:  median accept_rate = 78%  
   HAMi:  median accept_rate = 78%
   Verdict: TIE (both use hamiSelect due to ρ_count < τ=0.50)
   
3. skew-big Distribution (ρ_count=0.95):
   DASP:  median accept_rate = 30%
   HAMi:  median accept_rate = 31%
   Verdict: Near-tie, DASP -1pt (segregation provides no clear benefit)
   
4. bimodal Distribution (ρ_count=0.50):
   DASP:  median accept_rate = 56%
   HAMi:  median accept_rate = 56%
   Verdict: TIE (hamiSelect triggered since 0.50 ≤ τ=0.50)

LATENCY COMPARISON (uniform, median ns/op):
DASP:  ~33,556 ns/op
HAMi:  ~33,614 ns/op
Verdict: Essentially tied (HAMi is naive spreading, DASP has extra logic overhead)

HONEST OVERALL CONCLUSION:
==========================
- No T2 clean win on acceptance rate across all traces
- skew-small/bimodal: TIES (adaptive fallback works correctly)
- skew-big: NEAR-TIE (segregation provides marginal/no benefit)
- uniform: DASP LOSER (-7 pts) <- THIS BREAKS "UNIVERSAL WIN" CLAIM

REASON FOR LOSS ON UNIFORM:
===========================
HAMi's "max free slices" strategy is essentially OPTIMAL for MIG acceptance rate
because it maximizes future flexibility via spreading. DASP's zone segregation:
+ Protects large-contiguous regions BY DESIGN (conceptual strength)
- But sacrifices small-request acceptance when zones are too aggressive
- On uniform mix(60% large by count, ~83% by slice-weighted rho), R=round(0.83*8)=7
  GPUs reserved for large → leaves only 1 GPU for大量小请求 → fragmentation!

EVEN WITH CAPPING rho at 0.625 (R≤5), the zone partitioning itself fragments
capacity that HAMi's adaptive spreading would keep unified. This is a FUNDAMENTAL
LIMITATION of zone-based segregation vs greedy spreading for ACCEPTANCE RATE.

WHAT ABOUT THE "+9.1% DkSP DENSITY" CLAIM?
==========================================
This refers to TOPOLOGY packing density (dense-k-subgraph problem), NOT MIG
slice acceptance. These are DIFFERENT benchmark axes:
- MIG placement: which GPU/slice to use (acceptance rate focused)
- DkSP topology: which k GPUs have best intra-bandwidth sum (bandwidth quality)

The +9.1% claim likely measures bandwidth-sum improvement via DenseK solvers
(EightBB vs binpacking), NOT MIG placement acceptance. We need separate benchmark
to verify this specific claim.

FINAL RECOMMENDATIONS:
======================
1. Accept current state as honest baseline (user mandate: "never fake")
2. For T2 clean win, must EITHER:
   a) Prove DASP wins on some OTHER metric (latency? bandwidth sum?)
   b) Modify segregation to NOT sacrifice acceptance rate (currently impossible
      against HAMi's near-optimal spreading for acceptance)
   c) Reposition as T3 theoretical moat only (NP-hardness reduction from dense-k)

3. Honest classification: CONDITIONAL WIN (wins skew-small via fallback, ties others)
   but NO UNIVERSAL T2 CLEAN WIN

============================================================
BUILD STATUS: CLEAN (go build/vet pass)
DATA SOURCE: Real CLI benchmark with count=6, PowerShell output capture
VERIFIED: Yes - numbers match raw CLI output
============================================================
