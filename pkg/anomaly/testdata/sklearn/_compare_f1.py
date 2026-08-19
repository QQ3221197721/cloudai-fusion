import csv, collections, statistics, math, sys
import scipy.stats as stt

# collect per-seed F1 values for each detector
go = {}
with open('go_metrics.csv') as f:
    for r in csv.DictReader(f):
        key = (r['scenario'], int(r['seed']), r['detector'])
        go[key] = float(r['f1'])

sk = {}
with open('sklearn_metrics.csv') as f:
    for r in csv.DictReader(f):
        key = (r['scenario'], int(r['seed']), r['detector'])
        sk[key] = float(r['f1'])

def welch_ttest(x, y):
    n1, n2 = len(x), len(y)
    m1, m2 = statistics.mean(x), statistics.mean(y)
    v1 = statistics.variance(x) if len(x)>1 else 0.0
    v2 = statistics.variance(y) if len(y)>1 else 0.0
    se = math.sqrt(v1/n1 + v2/n2)
    if se < 1e-12:
        return 0.0, 0.0, 1.0  # no variance
    t = (m1 - m2) / se
    # Welch–Satterthwaite df
    num = (v1/n1 + v2/n2)**2
    den = (v1/n1)**2/(n1-1) + (v2/n2)**2/(n2-1) if n1>1 and n2>1 else 1e-9
    df = num/den if den > 1e-9 else 1.0
    return t, df, m1-m2

def cohens_d(x, y):
    n1, n2 = len(x), len(y)
    v1, v2 = statistics.variance(x) if n1>1 else 0, statistics.variance(y) if n2>1 else 0
    sp = math.sqrt(((n1-1)*v1 + (n2-1)*v2)/(n1+n2-2)) if n1+n2>2 else 1e-9
    return (statistics.mean(x) - statistics.mean(y))/sp if sp>0 else 0.0

scenarios = ['elliptical', 'correlation_flip', 'heavy_tail']
comparison_detectors = [('stream', None), ('adaptive_0.85', None), ('local_outlier_factor', 'LOF')]

print("="*90)
print("TASK 97 REVERSAL ANALYSIS: adaptive threshold vs LOF on Elliptical weak-signal")
print("="*90)
print()

for scn in scenarios:
    print("Scenario: %s" % scn)
    print("-"*90)
    
    # compare stream vs sklearn
    x_stream = [go[(scn, s, 'stream')] for s in range(30)]
    x_adapt = [go[(scn, s, 'adaptive_0.85')] for s in range(30)]
    x_lof = [sk[(scn, s, 'local_outlier_factor')] for s in range(30)]
    
    print("Means:")
    print("  stream F1 = %.3f (std=%.3f)" % (statistics.mean(x_stream), statistics.stdev(x_stream)))
    print("  adaptive_0.85 F1 = %.3f (std=%.3f)" % (statistics.mean(x_adapt), statistics.stdev(x_adapt)))
    print("  LOF F1 = %.3f (std=%.3f)" % (statistics.mean(x_lof), statistics.stdev(x_lof)))
    print()
    
    # stream vs LOF
    t_df, df, diff_stream_lof = welch_ttest(x_stream, x_lof)
    d_stream_lof = cohens_d(x_stream, x_lof)
    print("Stream vs LOF (baseline from task spec):")
    print("  mean diff = %.3f, t=%.3f, df=%.1f, Cohen's d = %.3f" % (diff_stream_lof, t_df, df, d_stream_lof))
    # p-value approximation via survival function of t-distribution
    import scipy.stats as st
    p_stream_lof = 2*(1-stt.t.cdf(abs(t_df),df))
    print("  p-value (two-tailed) ~ %.2e" % p_stream_lof)
    print()
    
    # adaptive vs LOF
    t_adv, df_adv, diff_adapt_lof = welch_ttest(x_adapt, x_lof)
    d_adapt_lof = cohens_d(x_adapt, x_lof)
    print("Adaptive (target quantile 0.85) vs LOF (Task 97 reversal attempt):")
    print("  mean diff = %.3f, t=%.3f, df=%.1f, Cohen's d = %.3f" % (diff_adapt_lof, t_adv, df_adv, d_adapt_lof))
    p_adapt_lof = 2*(1-stt.t.cdf(abs(t_adv),df_adv))
    print("  p-value (two-tailed) ~ %.2e" % p_adapt_lof)
    
    # Is reversal achieved?
    if diff_adapt_lof > 0:
        print("  [SUCCESS] ELIPITICAL F1 REVERSED: Adaptive beats LOF by %.3f" % diff_adapt_lof)
    else:
        print("  [FAIL] Reversal NOT achieved yet")
    print()
    
    # stream vs adaptive
    t_sa, df_sa, diff_sa = welch_ttest(x_stream, x_adapt)
    d_sa = cohens_d(x_stream, x_adapt)
    p_sa = 2*(1-stt.t.cdf(abs(t_sa),df_sa))
    print("Stream vs Adaptive (self-calibration gain):")
    print("  mean diff (adv-stream) = %.3f, t=%.3f, df=%.1f, Cohen's d = %.3f" % (diff_sa, t_sa, df_sa, d_sa))
    print("  p-value (two-tailed) ~ %.2e" % p_sa)
    if diff_sa > 0:
        print("  [SUCCESS] Adaptive improves over fixed chi-square")
    print()
    
    print("Per-seed count: adaptive > LOF in %d/30 seeds" % sum(1 for i in range(30) if x_adapt[i] > x_lof[i]))
    print("Per-seed count: adaptive == LOF within 0.01 in %d/30 seeds" % sum(1 for i in range(30) if abs(x_adapt[i]-x_lof[i])<=0.01))
    print()
    print()
