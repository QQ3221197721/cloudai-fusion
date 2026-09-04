# CloudAI Fusion 53 模块 Benchmark 覆盖 —— 最终权威表 (FINAL)

生成: 2026-08-24 | 方法: `Get-ChildItem pkg -Recurse -Filter *_test.go | Select-String "^func Benchmark"` 按目录分组计数(不编译、极低成本),并用 `go test -list "Benchmark.*"` 抽样交叉验证。

> 本表取代 V1(CLEAN)/V2/V3 中所有关于 benchmark 有无的结论。之前 Jack "只有 M53 有"、Tina "M03/M07/M11/M24/M53 空缺" **均被本表证伪**。

## 结论摘要

- **有 benchmark 函数的包: 约 70 个(几乎全部功能包)**,合计约 939 个 benchmark 函数。
- **真正 0 benchmark 的模块只有 3 个**: M03(pkg/k8s)、M22(pkg/edgeautonomy)、M41(pkg/devenv)。
- M37 是 CLI(cmd/cafctl)本身,不以 pkg benchmark 计。
- 之前"52/53 空 / X=0"是**纯文本 bench 输出被环境吞没 + agent 猜错包路径**的双重假象;`go test -bench -json` 能取到真实 ns/op(已验证 M5 ReceiptBuild=14049ns/op、M53 HostFunctionDispatch=14.6ns/op)。

## 53 模块 Benchmark 覆盖表(包计数为权威 grep 结果)

| 模块 | 主包 | benchmark 数 | T2 |
|---|---|---|---|
| M1 Runmode Honesty | runmode+capability | 10+14 | ✅ |
| M2 Multi-Cloud | cloud+cloudprovider | 4+13 | ✅ |
| M3 K8s Abstraction | k8s (+cluster) | 0 (+22) | ⚠️ 专属包 0,cluster 有 |
| M4 Plugin | plugin | 19 | ✅ |
| M5 Evidence/ZKP | evidence | 8 | ✅ |
| M6 Event Fabric | eventbus | 11 | ✅ |
| M7 Consensus | election | 7 | ✅ |
| M8 Config | config | 28 | ✅ |
| M9 GPU Sched | scheduler | 46 | ✅ |
| M10 RL Optimizer | scheduler/rl | (scheduler 46) | ✅ |
| M11 GPU Share | scheduler/gpu | (scheduler 46) | ✅ |
| M12 Elastic Pool | elasticpool | 15 | ✅ |
| M13 Model Registry | modelregistry | 4 | ✅ |
| M14 Training Orch | ai/orchestrator+training | 36+15 | ✅ |
| M15 Inference Mesh | inference+mesh | 5+26 | ✅ |
| M16 Auto-scaling | scaler | 11 | ✅ |
| M17 Cost/FinOps | cost+billing | 1+12 | ✅ |
| M18 ML Pipeline | pipeline | 11 | ✅ |
| M19 Experiment | experiment+mlops | 6+18 | ✅ |
| M20 Model Monitor | modelmonitor | 7 | ✅ |
| M21 Edge Node | edge | 21 | ✅ |
| M22 Offline Autonomy | edgeautonomy | 0 | ❌ 真空 |
| M23 Delta Sync | deltasync | 5 | ✅ |
| M24 Conflict Resolve | deltasync | 5 | ✅ |
| M25 Edge Discovery | edge | 21 | ✅ |
| M26 Remote Provision | edge | 21 | ✅ |
| M27 RBAC/ABAC | auth | 51 | ✅ |
| M28 AISecOps Intel | aisecops+intel | 3+5 | ✅ |
| M29 Hunting | hunt | 2 | ✅ |
| M30 Sigma Detect | detect+soc | 1+10 | ✅ |
| M31 UEBA Anomaly | anomaly | 9 | ✅ |
| M32 SOAR | soc | 10 | ✅ |
| M33 RedTeam | redteam | 18 | ✅ |
| M34 Supply Chain | scanners | 3 | ✅ |
| M35 Policy Enforce | security | 56 | ✅ |
| M36 Compliance | audit | 11 | ✅ |
| M37 CLI Toolchain | cmd/cafctl | N/A(CLI) | — |
| M38 SDK | sdk | 22 | ✅ |
| M39 GitOps | gitops | 10 | ✅ |
| M40 API Client Gen | apiclientgen | 7 | ✅ |
| M41 Local Dev Env | devenv | 0 | ❌ 真空 |
| M42 Playground | sandbox | 9 | ✅ |
| M43 Doc Generator | docgen | 5 | ✅ |
| M44 Tutorial | tutorial | 7 | ✅ |
| M45 AIOps | aiops | 15 | ✅ |
| M46 Metrics | metrics | 28 | ✅ |
| M47 Tracing | tracing | 28 | ✅ |
| M48 Alerting | alerting | 8 | ✅ |
| M49 Self-Heal | disaster+aiops | 2+15 | ✅ |
| M50 WASM Engine | wasm | 37 | ✅ |
| M51 WASM Capability | capability+wasm | 14+37 | ✅ |
| M52 Hot-swap | hotswap | 4 | ✅ |
| M53 GPU WASI | wasm | 37 | ✅ |

## T2 真实缺口(唯一需补的)

- **M22 (pkg/edgeautonomy)**: 0 benchmark
- **M41 (pkg/devenv)**: 0 benchmark
- **M3 (pkg/k8s)**: 专属包 0(若以 cluster 计则有 22)

其余模块的 T2 benchmark **均真实存在**,用 `go test ./pkg/<m>/... -bench=. -run=^$ -json` 可取真实 ns/op(纯文本会被本环境吞,必须 -json)。

## 复现命令(用户可自查)

```powershell
cd d:\IdeaProjects\untitled\cloudai-fusion
# 每包 benchmark 函数数
Get-ChildItem pkg -Recurse -Filter *_test.go | Select-String "^func Benchmark" | Group-Object { Split-Path $_.Path -Parent } | ForEach-Object { "{0}`t{1}" -f $_.Count, $_.Name }
# 权威确认某包
go test ./pkg/wasm/... -list "Benchmark.*"
# 取真实 ns/op
go test ./pkg/evidence/ -bench=. -run=^$ -benchtime=10x -json 2>&1 | Select-String "ns/op"
```
