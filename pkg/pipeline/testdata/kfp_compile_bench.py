#!/usr/bin/env python3
"""
Real Kubeflow Pipelines (KFP) DAG compilation benchmark.

Measures the ACTUAL local compile latency of KFP v2's Compiler for an
N-component linear ML pipeline. This is a genuine, cluster-free operation:
KFP compiles a Python DSL pipeline into an IR YAML (PipelineSpec protobuf
serialized to YAML). No Kubernetes, no Docker, no network required.

We build the pipeline dynamically with N components so the DAG structure
mirrors what the Go-side M18 benchmark constructs (linear chain of N tasks).

Output: JSON on stdout with per-iteration compile latency (ms), median, stddev,
and the compiled YAML size (bytes) as a proxy for artifact complexity.

Usage: python kfp_compile_bench.py <task_count> <iterations>
"""
import json
import os
import statistics
import sys
import tempfile
import time


def build_and_compile(task_count: int, out_path: str) -> float:
    """Build an N-component KFP pipeline and compile it. Returns compile ms."""
    from kfp import dsl
    from kfp.compiler import Compiler

    # Define a reusable lightweight component (no container run; compile-only).
    @dsl.component(base_image="python:3.11-slim")
    def step(name: str, value: int = 1) -> int:
        return value + 1

    # Build a linear-chain pipeline of task_count steps. Each step depends on
    # the previous one via data flow (output -> input), which is exactly how
    # KFP expresses a DAG edge. This forces real topological wiring in the IR.
    def pipeline_fn():
        prev = None
        for i in range(task_count):
            if prev is None:
                t = step(name=f"task-{i}", value=1)
            else:
                t = step(name=f"task-{i}", value=prev.output)
            prev = t

    pipe = dsl.pipeline(name="m18-vs-kfp-bench")(pipeline_fn)

    start = time.perf_counter()
    Compiler().compile(pipeline_func=pipe, package_path=out_path)
    return (time.perf_counter() - start) * 1000.0


def main():
    task_count = int(sys.argv[1]) if len(sys.argv) > 1 else 10
    iterations = int(sys.argv[2]) if len(sys.argv) > 2 else 6

    latencies = []
    yaml_size = 0
    tmpdir = tempfile.mkdtemp(prefix="kfp_bench_")

    try:
        for i in range(iterations):
            out_path = os.path.join(tmpdir, f"pipe_{task_count}_{i}.yaml")
            try:
                ms = build_and_compile(task_count, out_path)
                latencies.append(ms)
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
        print(json.dumps({"error": "no successful KFP compilations"}))
        sys.exit(1)

    result = {
        "system": "Kubeflow_Pipelines",
        "kfp_version": __import__("kfp").__version__,
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
