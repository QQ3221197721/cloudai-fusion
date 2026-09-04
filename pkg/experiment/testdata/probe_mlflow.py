#!/usr/bin/env python3
import time, tempfile, shutil, os, pathlib, random
import mlflow
from mlflow.tracking import MlflowClient
os.environ['MLFLOW_ALLOW_FILE_STORE'] = 'true'
tmp = tempfile.mkdtemp(prefix="mlflow-probe1k-")
uri = pathlib.Path(tmp).as_uri()
mlflow.set_tracking_uri(uri)
c = MlflowClient(tracking_uri=uri)
eid = c.create_experiment("probe1k")
N=1000
rng = random.Random(42)
opts = ["adam","sgd","rmsprop"]
t0=time.perf_counter()
for i in range(N):
    r=c.create_run(eid); rid=r.info.run_id
    c.log_param(rid,"lr", f"0.{i%10}"); c.log_param(rid,"opt", opts[i%3])
    ts=int(time.time()*1000)
    c.log_metric(rid,"acc", rng.random(), timestamp=ts, step=0)
    c.set_terminated(rid,"FINISHED")
build=time.perf_counter()-t0
print(f"build {N} runs = {build:.2f}s ({N/build:.1f} runs/sec)")
for trial in range(4):
    t1=time.perf_counter()
    found=c.search_runs([eid], filter_string="params.opt = 'adam'", order_by=["metrics.acc DESC"], max_results=10)
    print(f"  search#{trial} top-10 = {(time.perf_counter()-t1)*1000:.1f}ms matched={len(found)}")
shutil.rmtree(tmp, ignore_errors=True)
