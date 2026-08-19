import csv, collections, statistics, sys

agg = collections.defaultdict(lambda: collections.defaultdict(list))
with open('go_metrics.csv') as f:
    for r in csv.DictReader(f):
        k = (r['scenario'], r['detector'])
        for m in ('precision', 'recall', 'f1', 'auc'):
            agg[k][m].append(float(r[m]))

scns = ['elliptical', 'correlation_flip', 'heavy_tail']
# discover detectors present
dets = sorted({d for (_, d) in agg.keys()})
print('detectors present:', dets)
print()
for scn in scns:
    for det in dets:
        k = (scn, det)
        if k in agg:
            p = statistics.mean(agg[k]['precision'])
            rc = statistics.mean(agg[k]['recall'])
            f1 = statistics.mean(agg[k]['f1'])
            au = statistics.mean(agg[k]['auc'])
            n = len(agg[k]['f1'])
            print('%-18s %-16s P=%.3f R=%.3f F1=%.3f AUC=%.3f (n=%d)' % (scn, det, p, rc, f1, au, n))
    print()
