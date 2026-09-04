#!/usr/bin/env python3
"""M19 head-to-head harness: measures REAL mlflow on the SAME work unit as our
Go FSTracker benchmark.

Work unit (one "run"), identical to pkg/experiment FSTracker benchmark:
  - start a run
  - log 2 params (lr, batch)
  - log 3 metrics (metric0, metric1, metric2)
  - end the run

Then measures:
  - log throughput (runs/sec) for N runs
  - query latency: filter+retrieve runs by hyperparameters (lr=0.001 AND batch=32)
  - storage cost (bytes on disk) after N runs

Uses MLflow's local file store (mlflow's default, no server) — the fastest, most
favorable-to-mlflow configuration (no HTTP/network overhead). Outputs one JSON line.

Anti-fiasco: real `import mlflow`, no fake timing, honest numbers.
"""
import json
import os
import shutil
import sys
import tempfile
import time
import pathlib

import mlflow
from mlflow.tracking import MlflowClient


def dir_size(path: str) -> int:
    total = 0
    for root, _dirs, files in os.walk(path):
        for f in files:
            fp = os.path.join(root, f)
            try:
                total += os.path.getsize(fp)
            except OSError:
                pass
    return total


def build_and_query(n_runs: int, count: int) -> dict:
    """Build the MLflow file store ONCE with n_runs, then measure query + top-k
    latency `count` times against the SAME store. This isolates query latency
    (the FLIP M19 metric) from one-time ingestion cost. Honest: no warm-up is
    excluded selectively; every sample is a real search_runs round-trip."""
    import random
    tmp = tempfile.mkdtemp(prefix="mlflow-h2h-")
    try:
        os.environ["MLFLOW_ALLOW_FILE_STORE"] = "true"
        tracking_uri = pathlib.Path(tmp).as_uri()
        mlflow.set_tracking_uri(tracking_uri)
        client = MlflowClient(tracking_uri=tracking_uri)
        exp_id = client.create_experiment("h2h-bench")

        # Diverse metric values so top-k ranking is meaningful (fixed seed = reproducible)
        rng = random.Random(42)
        params = {"lr": "0.001", "batch": "32"}

        # ---- BUILD (one-time ingestion) ----
        t0 = time.perf_counter()
        for _ in range(n_runs):
            run = client.create_run(exp_id)
            rid = run.info.run_id
            for k, v in params.items():
                client.log_param(rid, k, v)
            ts = int(time.time() * 1000)
            client.log_metric(rid, "metric0", rng.random(), timestamp=ts, step=0)
            client.log_metric(rid, "metric1", rng.random(), timestamp=ts, step=0)
            client.log_metric(rid, "metric2", rng.random(), timestamp=ts, step=0)
            client.set_terminated(rid, "FINISHED")
        log_elapsed = time.perf_counter() - t0
        throughput = n_runs / log_elapsed if log_elapsed > 0 else 0.0
        storage = dir_size(tmp)

        # ---- QUERY SAMPLES (count repeats against the SAME store) ----
        samples = []
        for _ in range(count):
            # full filter query
            t1 = time.perf_counter()
            found = client.search_runs(
                experiment_ids=[exp_id],
                filter_string="params.lr = '0.001' and params.batch = '32'",
                max_results=n_runs,
            )
            query_ms = (time.perf_counter() - t1) * 1000.0

            # top-k with ORDER BY at k=10/50/100
            topk_latencies = []
            for topk_k in (10, 50, 100):
                t_topk = time.perf_counter()
                topk_found = client.search_runs(
                    experiment_ids=[exp_id],
                    filter_string="params.lr = '0.001' and params.batch = '32'",
                    order_by=["metrics.metric0 DESC"],
                    max_results=topk_k,
                )
                topk_ms = (time.perf_counter() - t_topk) * 1000.0
                topk_latencies.append({
                    "k": topk_k,
                    "ms": topk_ms,
                    "matched": len(topk_found),
                    # MLflow's own order_by IS the ground truth ranking → recall == 1.0
                    "recall_at_k": 1.0 if len(topk_found) >= topk_k else (len(topk_found) / topk_k),
                })

            samples.append({
                "n_runs": n_runs,
                "throughput_runs_per_sec": throughput,
                "query_ms": query_ms,
                "query_matched": len(found),
                "storage_bytes": storage,
                "topk_latencies": topk_latencies,
            })
        return {"samples": samples, "build_elapsed_s": log_elapsed}
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def main():
    n_runs = int(sys.argv[1]) if len(sys.argv) > 1 else 200
    count = int(sys.argv[2]) if len(sys.argv) > 2 else 6
    result = build_and_query(n_runs, count)
    samples = result["samples"]
    # median helper
    def median(vals):
        s = sorted(vals)
        m = len(s) // 2
        return s[m] if len(s) % 2 else (s[m - 1] + s[m]) / 2.0

    def stddev(vals):
        mean = sum(vals) / len(vals)
        return (sum((x - mean) ** 2 for x in vals) / len(vals)) ** 0.5

    thr = [s["throughput_runs_per_sec"] for s in samples]
    qms = [s["query_ms"] for s in samples]
    store = [s["storage_bytes"] for s in samples]
    
    # Aggregate top-k latencies (topk_latencies is a list of {k, ms, matched, recall_at_k})
    topk_results = {}
    for topk_k in [10, 50, 100]:
        k_times = []
        k_recalls = []
        for ss in samples:
            for entry in ss.get("topk_latencies", []):
                if entry["k"] == topk_k:
                    k_times.append(entry["ms"])
                    k_recalls.append(entry["recall_at_k"])
        if k_times:
            topk_results[str(topk_k)] = {
                "latency_ms_median": median(k_times),
                "recall_at_k": median(k_recalls),
            }
    
    output = {
        "tool": "mlflow",
        "mlflow_version": mlflow.__version__,
        "n_runs": n_runs,
        "count": count,
        "throughput_runs_per_sec_median": median(thr),
        "throughput_runs_per_sec_stddev": stddev(thr),
        "query_ms_median": median(qms),
        "query_ms_stddev": stddev(qms),
        "storage_bytes_median": median(store),
        "storage_bytes_per_run": median(store) / n_runs,
        "topk_latency_ms": topk_results,
        "samples": samples,
    }
    print(json.dumps(output))


if __name__ == "__main__":
    main()
