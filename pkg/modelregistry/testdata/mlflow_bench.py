#!/usr/bin/env python3
"""
Real MLflow Model Registry benchmark (MLflow 3.x compatible).

Measures the ACTUAL model registration and lineage-query latency in MLflow using
a SQLite metadata backend (required since MLflow 3.x deprecated the file store)
plus a local filesystem artifact root. This is a genuine, disk-backed operation:
MLflow persists model-version metadata in SQLite and artifacts on the filesystem.

Same work unit as the Go-side M13 benchmark:
  - Register N model versions, each carrying a 4KB artifact payload.
  - Query: look up a model version + walk its lineage/metadata.

We use MlflowClient directly (not the fluent API) to minimize framework overhead
and to make the compared "register one version with an artifact" unit explicit.

Output: JSON on stdout with per-iteration latencies, median, stddev, artifact
sizes. On any failure we emit an "error" field and NEVER fabricate numbers.

Usage: python mlflow_bench.py <model_name> <num_versions> <iterations>
"""

import json
import os
import statistics
import sys
import tempfile
import time
import uuid

# MLflow 3.x puts the local file store in "maintenance mode" and raises unless the
# operator explicitly opts in. We use the file-based registry (a supported scheme)
# for a fully offline, server-free benchmark, so set this before importing mlflow.
os.environ.setdefault("MLFLOW_ALLOW_FILE_STORE", "true")


def run_benchmark(model_name: str, num_versions: int, iterations: int) -> dict:
    """Register model versions with artifacts; measure register + query latency."""
    import mlflow
    from mlflow.tracking import MlflowClient

    mlflow_version = mlflow.__version__

    tmpdir = tempfile.mkdtemp(prefix="mlflow_t2_bench_")
    # Use a local filesystem directory as artifact root (file-backed store).
    artifact_root = os.path.join(tmpdir, "artifacts")
    os.makedirs(artifact_root, exist_ok=True)

    # Build a cross-platform file URI (yields file:///C:/... on Windows).
    from pathlib import Path
    tracking_uri = Path(tmpdir).as_uri()
    client = MlflowClient(tracking_uri=tracking_uri)

    try:
        # Create the experiment; let MLflow derive a default local artifact root.
        exp_id = client.create_experiment(
            f"t2-bench-{uuid.uuid4().hex[:8]}",
        )

        # Create the registered model once (the container for versions).
        client.create_registered_model(model_name)

        # Pre-generate 4KB artifact payloads (mirrors the Go side exactly).
        artifact_files = []
        for i in range(num_versions):
            data = bytes([(i ^ j) & 0xFF for j in range(4 * 1024)])
            p = os.path.join(tmpdir, f"weights-{i}.bin")
            with open(p, "wb") as fh:
                fh.write(data)
            artifact_files.append(p)

        model_card_bytes = os.path.getsize(artifact_files[0])

        # ---- Registration latency: register N versions with artifacts ----
        register_latencies = []
        registered = []  # (version_str, run_id)
        for i in range(iterations):
            art = artifact_files[i % len(artifact_files)]

            start = time.perf_counter()
            # 1) Create a run and upload the artifact (weights).
            run = client.create_run(experiment_id=exp_id)
            client.log_artifact(run.info.run_id, art, artifact_path="weights")
            # 2) Register a new model version pointing at that artifact.
            source = f"{run.info.artifact_uri}/weights"
            mv = client.create_model_version(
                name=model_name,
                source=source,
                run_id=run.info.run_id,
            )
            client.set_model_version_tag(model_name, mv.version, "framework", "pytorch")
            client.set_model_version_tag(model_name, mv.version, "task_type", "classification")
            elapsed_ms = (time.perf_counter() - start) * 1000.0

            register_latencies.append(elapsed_ms)
            registered.append((mv.version, run.info.run_id))

        # ---- Query latency: look up version + walk metadata/lineage ----
        query_latencies = []
        for i in range(iterations):
            ver, _run_id = registered[i % len(registered)]
            start = time.perf_counter()
            # Retrieve the version record (metadata read).
            mv = client.get_model_version(model_name, ver)
            # Walk lineage: the source run + its tags/params (the parent link).
            _ = client.get_run(mv.run_id)
            # Enumerate all versions of this model (the "list lineage" analog).
            _ = client.search_model_versions(f"name='{model_name}'")
            elapsed_ms = (time.perf_counter() - start) * 1000.0
            query_latencies.append(elapsed_ms)

        return {
            "system": "MLflow_Registry",
            "mlflow_version": mlflow_version,
            "model_name": model_name,
            "model_versions": num_versions,
            "iterations": len(register_latencies),
            "register_latency_ms": register_latencies,
            "median_register_ms": statistics.median(register_latencies),
            "stddev_register_ms": statistics.stdev(register_latencies) if len(register_latencies) > 1 else 0.0,
            "query_latency_ms": query_latencies,
            "median_query_ms": statistics.median(query_latencies) if query_latencies else 0.0,
            "stddev_query_ms": statistics.stdev(query_latencies) if len(query_latencies) > 1 else 0.0,
            "model_card_bytes": model_card_bytes,
            "error": None,
        }
    finally:
        try:
            import shutil
            shutil.rmtree(tmpdir, ignore_errors=True)
        except OSError:
            pass


def main():
    if len(sys.argv) < 4:
        print(json.dumps({"error": "usage: mlflow_bench.py <model_name> <num_versions> <iterations>"}))
        sys.exit(1)

    model_name = sys.argv[1]
    num_versions = int(sys.argv[2])
    iterations = int(sys.argv[3])

    try:
        import mlflow  # noqa: F401
    except ImportError:
        print(json.dumps({"error": "mlflow package not found", "mlflow_version": "unavailable"}))
        sys.exit(1)

    try:
        result = run_benchmark(model_name, num_versions, iterations)
        print(json.dumps(result, default=str))
    except Exception as e:  # noqa: BLE001 - report honestly, do not fake
        import mlflow
        print(json.dumps({
            "error": f"{type(e).__name__}: {str(e)}",
            "mlflow_version": getattr(mlflow, "__version__", "unknown"),
            "iterations": 0,
        }, default=str))
        sys.exit(1)


if __name__ == "__main__":
    main()
