# T2 缺陷权威清单 (DEFINITIVE)

> 生成时间: 2026-08-24 | 方法: 逐模块 `go test -bench -json` 实测 + 竞品对比函数识别
> CPU: Intel Core Ultra 9 275HX | OS: Windows | benchtime=10x
> 映射依据: `docs/delivery-evidence/DELIVERY_STATUS_FULL_53_FINAL.md`

---

## T2 缺陷总数 = 43

- 无对比缺陷(含铁定): 37 个模块
- 输了/平: 6 个模块 (M23, M24, M33, M50, M51, M53)
- 合计: **43**

---

## 一、缺陷清单 -- 输了/平 (有对比但未胜出)

| 模块 | 包 | 缺陷类型 | 证据(命令+数字) |
|------|-----|----------|------|
| M23 Delta Sync | deltasync | 输了 | `go test ./pkg/deltasync/ -bench FastCDC|NaiveFixed -json`: FastCDC=862070 ns/op, NaiveFixed=321820 ns/op; 朴素方案快 2.68x |
| M24 Conflict Resolve | deltasync | 输了 | 同 M23 共用包 |
| M33 RedTeam | redteam | 输了 | `go test ./pkg/redteam/ -bench IncrementalChainHash|NaiveRecompute -json`: Incremental=369000, Naive=296680 ns/op; 朴素方案快 1.24x |
| M50 WASM Engine | wasm | 输了 | `go test ./pkg/wasm/ -bench ShardedAllocator_GlobalMutex -json`: Sharded=520, GlobalMutex=230 ns/op; 基线快 2.26x |
| M51 WASM Capability | wasm | 输了 | 同 M50 共用包 |
| M53 GPU WASI | wasm | 输了 | 同 M50 共用包 |

> 注: M33 在 TechniqueIndex vs LinearScan 维度胜出(60 vs 190 ns/op, 3.2x), 但 chain-hash 维度输; 按严格规则有败绩=缺陷。

---

## 二、缺陷清单 -- 无对比 (纯自测, 无竞品/朴素基线头对头)

| # | 模块 | 包 | 缺陷类型 | 说明 |
|---|------|-----|----------|------|
| 1 | M2 Multi-Cloud | cloud | 无对比(STUB) | 代码自承认 STUB BENCHMARKS |
| 2 | M4 Plugin | plugin | 无对比 | DirectCallBaseline 仅测自身开销 |
| 3 | M5 Evidence/ZKP | evidence | 无对比 | 无 Rekor/Sigstore 竞品 bench |
| 4 | M6 Event Fabric | eventbus | 无对比 | NATS 仅降级测试, 未 bench NATS 吞吐 |
| 5 | M7 Consensus | election | 无对比 | 无 etcd/Consul 对比 |
| 6 | M8 Config | config | 无对比 | 注释明确 "不与 Viper 比" |
| 7 | M12 Elastic Pool | elasticpool | 无对比 | 纯自测 |
| 8 | M13 Model Registry | modelregistry | 无对比 | 无 MLflow/W&B |
| 9 | M14 Training Orch | ai/orchestrator | 无对比 | 无 Kubeflow/Ray |
| 10 | M15 Inference Mesh | inference+mesh | 无对比 | 无 Istio/Envoy |
| 11 | M16 Auto-scaling | scaler | 无对比 | 无 KEDA/HPA |
| 12 | M17 Cost/FinOps | billing | 无对比 | Baseline 仅测自身旧路径 |
| 13 | M18 ML Pipeline | pipeline | 无对比 | 无 Argo/Kubeflow |
| 14 | M19 Experiment | experiment | 无对比 | Compare 是自身 A/B 功能 |
| 15 | M20 Model Monitor | modelmonitor | 无对比 | 无 Evidently/WhyLabs |
| 16 | M21 Edge Node | edge | 无对比 | 无 rsync/xdelta3 真实 import |
| 17 | M22 Offline Autonomy | edgeautonomy | **铁定**(0 bench) | 整包 0 benchmark 函数 |
| 18 | M25 Edge Discovery | edge | 无对比 | 同 M21 |
| 19 | M26 Remote Provision | edge | 无对比 | 同 M21 |
| 20 | M29 Hunting | hunt | 无对比 | 无 Splunk/Elastic |
| 21 | M30 Sigma Detect | detect+soc | 无对比 | 无 Sigma/YARA 引擎 |
| 22 | M31 UEBA Anomaly | anomaly | 无对比 | Streaming vs Offline 是自身模式, 且 streaming 准确率更低 |
| 23 | M32 SOAR | soc | 无对比 | 无 Demisto/Phantom |
| 24 | M34 Supply Chain | scanners | 无对比 | 无 Trivy/Grype |
| 25 | M36 Compliance | audit | 无对比 | 无 OPA/Falco |
| 26 | M38 SDK | sdk | 无对比 | 纯自测 |
| 27 | M39 GitOps | gitops | 无对比 | FlatGrouping 是自身朴素模式, 非 ArgoCD |
| 28 | M40 API Client Gen | apiclientgen | 无对比 | 无 openapi-generator |
| 29 | M41 Local Dev Env | devenv | **铁定**(0 bench) | 整包 0 benchmark 函数 |
| 30 | M42 Playground | sandbox | 无对比 | 无 Kata/gVisor |
| 31 | M43 Doc Generator | docgen | 无对比 | 无 Swagger-UI/Redoc |
| 32 | M44 Tutorial | tutorial | 无对比 | 无竞品可比 |
| 33 | M45 AIOps | aiops | 无对比 | 无 Datadog/PagerDuty |
| 34 | M46 Metrics | metrics | 无对比 | 无 Prometheus client-go 头对头 |
| 35 | M48 Alerting | alerting | 无对比 | 提及 Alertmanager 但未 import |
| 36 | M49 Self-Heal | disaster | 无对比 | 无 Chaos Monkey |
| 37 | M52 Hot-swap | hotswap | 无对比 | 自身 fast-path, 无 Wazero/Wasmtime 对比 |

---

## 三、达标清单 (有对比且胜出)

| 模块 | 包 | 竞品/基线 | 我方 ns/op | 竞品 ns/op | 胜出幅度 | 命令 |
|------|-----|-----------|-----------|-----------|----------|------|
| M27 RBAC/ABAC | auth | Casbin v2 (真实 import) | 170 | 6,480 | **38.1x** | `go test -tags casbin -bench Casbin_Allow|CompiledRBAC_Allow` |
| M35 Policy | security | Regexp (stdlib 算法对比) | 1,040 | 279,870 | **269x** | `go test -bench AhoCorasick_100|Regexp_100` |
| M47 Tracing | tracing | OTel SDK (真实 import) | 810 | 10,910 | **13.5x** | `go test -bench FastSpanStart|OTelSDKComparison` |
| M10 RL Optimizer | scheduler | HAMi (重写对跑) | DASP 0.96 | HAMi 0.88 | **+9.1%** | 见 benchmark-results/m2_dir1 |
| M11 GPU Share | scheduler | HAMi (重写对跑) | 同上 | 同上 | 同上 | 同上 |
| M28 Intel | intel | NaiveScan O(n) | 470 | 16,350 | **34.8x** | `go test -bench DedupMap_vs_NaiveScan` |

> M1 (runmode) Parallel vs Serial 维度不同(吞吐 vs 延迟), 不计入达标也不计缺陷, 归为存疑。

---

## 四、存疑/排除

| 模块 | 分类 | 原因 |
|------|------|------|
| M1 Runmode | 存疑 | Parallel=5350ns vs Serial=240ns, 维度不同 |
| M3 K8s | 排除 | 用户指定硬件 |
| M9 GPU Sched | 排除 | 用户指定硬件(实际有对比且胜) |
| M37 CLI | 排除 | 非功能包, N/A |

---

## 五、方法论

1. 映射来源: `DELIVERY_STATUS_FULL_53_FINAL.md` 53 行模块-包表
2. 竞品搜索: `Select-String -Pattern "casbin|BobuSumisu|ddsketch|tdigest|prometheus|opentelemetry|zap|sarama|nats"`
3. 基线搜索: `Select-String -Pattern "func Benchmark.*(Baseline|Naive|Compare|_vs_|GlobalMutex|FullDrain)"`
4. 实测: `go test ./pkg/<x>/ -bench=<names> -run=^$ -benchtime=10x -json`
5. 证据: `output/t2_*.json` (18 文件, 总计 ~85KB)

---

## 六、终极汇总

| 类别 | 数量 | 占比(50个) |
|------|------|-----------|
| **T2 缺陷** | **43** | **86%** |
|   其中: 无对比 | 37 | 74% |
|   其中: 输了 | 6 | 12% |
| 达标 | 6 | 12% |
| 存疑 | 1 | 2% |
| 排除 | 3 | — |
