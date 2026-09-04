#!/usr/bin/env python3
"""
Real Argo Workflows DAG compilation benchmark via Hera SDK.

Measures the ACTUAL local compile latency of Argo Workflows via Hera v7.x's 
compiler for an N-component linear ML pipeline. This compiles a Python DSL
pipeline into an Argo Workflow CRD YAML (submittable to `argo submit`).
No Kubernetes cluster required - this is purely local compilation.

We build the pipeline dynamically with N components so the DAG structure
mirrors what the Go-side M18 benchmark constructs (linear chain of N tasks).

Output: JSON on stdout with per-iteration compile latency (ms), median, stddev,
and the compiled YAML size (bytes) as a proxy for artifact complexity.

Usage: python argo_compile_bench.py <task_count> <iterations>
"""
import json
import os
import statistics
import sys
import tempfile
import time


def build_and_compile(task_count: int, out_path: str) -> float:
    """Build an N-component Argo workflow and compile it. Returns compile ms.

    Uses Hera v7.x's DAG DSL to build a linear chain with explicit `depends:` edges,
    then serializes to Argo Workflow CRD YAML. The output file is written to disk
    so `compiled_yaml_bytes` reflects a REAL artifact (like M18's Create()+Publish()).
    """
    from hera.workflows import DAG, Workflow, Script

    # Build a linear-chain DAG with N-1 `depends:` edges — exactly equivalent to KFP's
    # linear pipeline via data-flow wiring and to M18's stage list with parent refs.
    start_time = time.perf_counter()

    tmpl = Script(
        name="step",
        image="python:3.11-slim",
        command=["python"],
        source="print(1)",
    )
    with Workflow(name=f"m18-vs-argo-bench-{task_count}") as wf:
        with DAG(name="main"):
            prev = None
            for i in range(task_count):
                t = tmpl(name=f"task-{i}", arguments={})
                if prev is not None:
                    prev >> t  # This creates a depends edge (the DAG operation)
                prev = t

        yaml_str = wf.to_yaml()

    end_time = time.perf_counter()
    compile_ms = (end_time - start_time) * 1000.0

    # Write the artifact to disk (matches M18's persist-to-disk semantics)
    with open(out_path, "w", encoding="utf-8") as f:
        f.write(yaml_str)

    return compile_ms


def main():
    task_count = int(sys.argv[1]) if len(sys.argv) > 1 else 10
    iterations = int(sys.argv[2]) if len(sys.argv) > 2 else 6

    latencies = []
    yaml_size = 0
    tmpdir = tempfile.mkdtemp(prefix="argo_bench_")

    try:
        for i in range(iterations):
            out_path = os.path.join(tmpdir, f"argo_{task_count}_{i}.yaml")
            try:
                ms = build_and_compile(task_count, out_path)
                latencies.append(ms)
                
                # Write the actual file to measure size
                if os.path.exists(out_path):
                    yaml_size = os.path.getsize(out_path)
            except Exception as e:  # noqa: BLE001 - report, do not fake
                print(json.dumps({"error": f"{type(e).__name__}: {e}"}), file=sys.stderr)
                # Record a failed iteration honestly as None -> skip
                continue
    finally:
        # best-effort cleanup
        try:
            for f in os.listdir(tmpdir):
                os.remove(os.path.join(tmpdir, f))
            os.rmdir(tmpdir)
        except OSError:
            pass

    if not latencies:
        print(json.dumps({"error": "no successful Argo compilations"}))
        sys.exit(1)

    result = {
        "system": "Argo_Workflows_Hera",
        "hera_version": __import__("hera").__version__,
        "task_count": task_count,
        "iterations": len(latencies),
        "compile_latency_ms": latencies,
        "median_ms": statistics.median(latencies),
        "stddev_ms": statistics.stdev(latencies) if len(latencies) > 1 else 0.0,
        "compiled_yaml_bytes": yaml_size,
    }
    print(json.dumps(result))


if __name__ == "__main__":
    main()
