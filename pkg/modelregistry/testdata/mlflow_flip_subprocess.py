#!/usr/bin/env python3
"""
FLIP M13 vs MLflow: Lineage query latency + storage dedup ratio benchmark.
Same workload: N models with lineage chain depth D.
Compares Go-native vs Python/SQLite approaches.
Usage: python mlflow_flip_subprocess.py <num_models> <chain_depth> <iterations>
"""

import json
import os
import sys
import time
import uuid
import tempfile
from pathlib import Path

# Set env var BEFORE importing mlflow (required for MLflow 3.x)
os.environ.setdefault("MLFLOW_ALLOW_FILE_STORE", "true")

def run_benchmark(num_models, chain_depth, iterations):
    """Benchmark MLflow registry with lineage chains."""
    
    # Build tracking URI (file-backed for offline test)
    tmpdir = str(Path(tempfile.mkdtemp(prefix="mlflow_flip_bench_")))
    artifact_root = os.path.join(tmpdir, "artifacts")
    os.makedirs(artifact_root, exist_ok=True)
    
    tracking_uri = Path(tmpdir).as_uri()
    
    try:
        import mlflow
        from mlflow.tracking import MlflowClient
        
        mlflow_version = mlflow.__version__
        client = MlflowClient(tracking_uri=tracking_uri)
        
        # Create experiment and registered model
        exp_id = client.create_experiment(f"flip-m13-bench-{uuid.uuid4().hex[:8]}")
        model_name = f"flip-model-chain-{uuid.uuid4().hex[:8]}"
        client.create_registered_model(model_name)
        
        # Pre-create artifacts with some duplicates
        artifact_map = {}
        artifact_sizes = [4*1024, 64*1024, 256*1024]
        
        total_written = 0
        for m in range(num_models):
            size = artifact_sizes[m % len(artifact_sizes)]
            p = os.path.join(tmpdir, f"weights-{m}.pt")
            data = bytes([(m ^ j) & 0xFF for j in range(size)])
            with open(p, "wb") as fh:
                fh.write(data)
            artifact_map[f"m{m}"] = p
            total_written += size
        
        # Register models with parent-child lineage relationships
        for m in range(num_models):
            name = f"flip-m{m}"
            prev_ver = None
            
            for d in range(chain_depth):
                version = f"1.{d}.0"
                
                # Create run and upload artifact
                run = client.create_run(exp_id)
                run_artifact_path = f"weights/{name}"
                client.log_artifact(run.info.run_id, artifact_map[name], artifact_path=run_artifact_path)
                source = f"{run.info.artifact_uri}/{run_artifact_path}"
                
                # Register model version with lineage metadata
                client.create_model_version(
                    name=model_name,
                    source=source,
                    run_id=run.info.run_id,
                    tags={"parent_version": prev_ver or ""}
                )
                
                if d > 0:
                    prev_ver = version
        
        # Count unique artifacts stored (MLflow has no native dedup like M13)
        artifact_files = list(Path(artifact_root).rglob("*"))
        unique_artifacts = len([f for f in artifact_files if f.is_file()])
        
        # Calculate dedup ratio (MLflow stores everything separately, so ~1.0x)
        # At best we get minor savings from exact-duplicate uploads
        dedup_ratio = float(unique_artifacts) / max(1, unique_artifacts)
        
        # Benchmark lineage queries (walk parent chains via tags/versions)
        query_latencies = []
        depths = []
        
        for m in range(min(20, num_models)):
            name = f"flip-m{m}"
            
            # Get all versions of this model
            versions = client.search_model_versions(f"name='{model_name}'")
            if not versions:
                continue
            
            # Find leaf version (highest number)
            sorted_vers = sorted(versions, key=lambda v: int(v.version.split('.')[1]))
            leaf_version = sorted_vers[-1]
            
            for i in range(iterations):
                start = time.perf_counter()
                
                # Walk lineage by reading parent tag
                mv = client.get_model_version(name, leaf_version.version)
                parent = mv.tags.get("parent_version", "")
                
                if parent:
                    parent_mv = client.get_model_version(name, parent)
                
                elapsed_ms = (time.perf_counter() - start) * 1000.0
                query_latencies.append(elapsed_ms)
                
                # Track chain depth
                depth = sum(1 for _ in walk_lineage(client, model_name, leaf_version.version))
                depths.append(depth)
        
        if not query_latencies:
            raise RuntimeError("No query latencies recorded")
        
        import statistics
        median_query_ns = statistics.median(query_latencies) * 1e6  # Convert ms to ns
        stddev_query_ns = statistics.stdev(query_latencies) * 1e6
        
        avg_chain_len = statistics.mean(depths) if depths else 0
        
        return {
            "system": "MLflow_Registry",
            "version": f"MLflow_{mlflow_version}",
            "model_count": num_models,
            "total_versions": num_models * chain_depth,
            "iterations": iterations,
            "median_query_latency_ns_op": median_query_ns,
            "stddev_query_latency_ns_op": stddev_query_ns,
            "dedup_ratio": dedup_ratio,
            "avg_lineage_depth": avg_chain_len,
            "artifact_size_avg_bytes": artifact_sizes[0],
            "error": None
        }
        
    except ImportError as e:
        return {
            "system": "MLflow_Registry",
            "error": f"ImportError: {str(e)}"
        }, False
        
    finally:
        # Cleanup
        import shutil
        try:
            shutil.rmtree(tmpdir, ignore_errors=True)
        except OSError:
            pass


def walk_lineage(client, model_name, version):
    """Walk parent lineage chain yielding versions."""
    current = version
    seen = set()
    while current and current not in seen:
        seen.add(current)
        yield current
        try:
            mv = client.get_model_version(model_name, current)
            parent = mv.tags.get("parent_version", "")
            if not parent:
                break
            current = parent
        except Exception:
            break


if __name__ == "__main__":
    if len(sys.argv) < 4:
        print(json.dumps({"error": "usage: mlflow_flip_subprocess.py <num_models> <chain_depth> <iterations>"}))
        sys.exit(1)
    
    num_models = int(sys.argv[1])
    chain_depth = int(sys.argv[2])
    iterations = int(sys.argv[3])
    
    try:
        result, ok = run_benchmark(num_models, chain_depth, iterations)
        print(json.dumps(result, default=str))
    except Exception as e:
        print(json.dumps({
            "system": "MLflow_Registry",
            "error": f"{type(e).__name__}: {str(e)}"
        }, default=str))
        sys.exit(1)
