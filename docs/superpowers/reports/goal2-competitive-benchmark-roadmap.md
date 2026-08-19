# 目标二竞品对标系统化路线图 (Goal 2 Competitive Benchmark Roadmap)

> **用途**: 为"逐个攻克 53 功能的目标二（对 2026 竞品的真实绝对优势）"提供执行依据。
> **诚实原则**: 只有具备真实可测代码 + 公开竞品基准的模块才列为"可对标"；mock/dead-code 一律标注为需先补实现。
> **数据来源**: 基于代码静态分析（benchmark 脚本扫描 + dead-code 信号 grep）。
> **生成日期**: 2026-08-17

---

## 一、53 模块真实性判定总览

| 类别 | 数量 | 占比 | 说明 |
|------|------|------|------|
| **Real（可直接对标）** | 28 | 52.8% | 完整实现 + 测试覆盖 + 具备 benchmark 脚本或可测维度 |
| **Needs Real Impl（需先补实现）** | 15 | 28.3% | 含 placeholder / stub / mock / simulated 信号 |
| **No Competitor / No Advantage** | 10 | 18.9% | 标准工具封装或无公开竞品基准 |

**核心结论**: 目前仅约一半模块具备真实竞品对标的前提；约 28% 存在 mock/dead-code 需先补实现；约 19% 本质无可比竞品或无优势潜力。

---

## 二、已有 Benchmark 资源盘点（真实可运行的实测基础）

| 模块 | Benchmark 脚本 | 度量维度 | 竞品参照 |
|------|---------------|----------|----------|
| Module 45 Anomaly Detection | `ai/anomaly/benchmark.py` | F1 / ROC-AUC on synthetic anomalies | sklearn IsolationForest, PyOD, River |
| Module 10 RL Scheduler | `ai/tests/test_competitor_baselines.py` (1542 行) | gini / SLA / throughput / cost | k8s binpack, k8s spread, round_robin, random |
| Module 5 Evidence Ledger (ZKP) | `pkg/evidence/evidence_bench_test.go` (8 benchmarks) | ZKP prove/verify latency, TPS | Sigstore Rekor |
| Module 29 Hunting Engine | `pkg/hunt/evidence_hunt_test.go` | Hunt mine 吞吐 | Splunk UBA, Exabeam |
| Module 46 Metrics/Observability | `pkg/observability/metrics_test.go` (3 benchmarks) | 聚合吞吐 / p95/p99 | Prometheus, Datadog |
| Module 6 Event Fabric | `pkg/eventbus/fabric_bench_test.go` (3 benchmarks) | events/sec | NATS, Kafka |

---

## 三、Top 10 可攻克模块（按"真实优势 × 投入产出比"排序）

| # | 模块 | 优势来源假设 | 竞品 | 度量指标 | 公开基准可得性 | 工作量 |
|---|------|-------------|------|----------|---------------|--------|
| 1 | **Module 45 Anomaly Detection** | 联合异常（打破相关性）捕捉能力优于单变量法 | sklearn IsolationForest / PyOD / River | F1 on joint anomalies, ROC-AUC | 有公开库可复现 | 低 (3 步) |
| 2 | **Module 29 Hunting Engine** | UEBA + IOC 联动 z-score 检测 | Splunk UBA, Exabeam | 检出率 / 误报率 | 需自行复现（商业竞品无公开数字） | 中 |
| 3 | **Module 10 RL Scheduler** | 队列感知 + 公平性多目标 | k8s kube-scheduler | gini_gpu_hours / SLA | 已有 benchmark（中央池自建） | 已进行中 |
| 4 | **Module 5 Evidence Ledger (ZKP)** | ZKP 完整性证明（scope 不可樱桃挑选） | Sigstore Rekor | prove/verify 延迟, 隐私保护能力 | Rekor 开源可复现 | 中 |
| 5 | **Module 28 AISecOps L1** | STIX 2.1 + ClickHouse 情报融合 | CrowdStrike Falcon | 情报摄取吞吐 / 去重率 | 商业竞品无公开数字 | 中 |
| 6 | **Module 32 SOAR Response** | 破坏性动作 human-in-the-loop 审批门 | TheHive / Cortex XSOAR | 响应延迟 / 审批合规率 | 开源可复现 | 中 |
| 7 | **Module 52 Hot-swap Migration** | 零停机 WASM 状态迁移 | Argo Rollouts | 切换停机时间 / 请求丢失率 | 开源可复现 | 中 |
| 8 | **Module 39 GitOps Drift Proof** | 加密证据链漂移检测 | Argo CD | 漂移检出延迟 | 开源可复现 | 中 |
| 9 | **Module 13 Model Registry** | content-addressed + lineage provenance | MLflow / DVC | 血缘追踪完整性 / 回滚正确性 | 开源可复现 | 中 |
| 10 | **Module 50 WASM Executor** | wazero 纯 Go 沙箱开销 | WasmEdge / Firecracker | 实例化延迟 / 调用开销倍数 | 开源可复现 | 中 |

---

## 四、Dead-code / Mock 黑名单（对标前必须先补实现）

| 模块 | 问题类型 | 证据位置 | 修复建议 | 状态 |
|------|---------|----------|----------|------|
| Module 53 GPU WASI | Dead-code (stub) | `pkg/wasm/wasi_gpu.go:310 return nil // stub for now` | 集成 NVIDIA Device Plugin / 真实设备节点 | ✅ 已由 Carter 修复（launchKernelOnDevice 改为真实校验或 ErrNoGPURuntime） |
| Module 2 Multi-cloud | Mock | `pkg/cloud/providers/base_provider.go:195 mockTransport` | 对接真实云 SDK client | 待补 |
| Module 11 GPU Sharing | Partial | `pkg/scheduler/gpu_sharing.go:151 return nil // MIG not supported` | 补齐 MIG 真实分配路径 | 待补 |
| Module 21 Edge Node Manager | Stub/Simulated | REST stub 标记为 simulated | 对接真实边缘节点 API | 待补 |
| Module 22 Offline Decision | Stub | bestResponse 算法未实现 | 实现真实决策逻辑 | 待补 |
| Module 23 Delta Sync | Simulated | "for now return simulated count" | 实现真实向量时钟合并 | 待补 |
| cafctl deploy | Mock | `cmd/cafctl/cmd_deploy.go` mockK8sDeployment | 对接真实 K8s cluster | 待补 |

---

## 五、诚实结论

1. **现在就能做真实竞品对标的模块**: 约 28 个（52.8%）——具备完整实现、测试覆盖与可测维度。
2. **需要先补真实实现的模块**: 约 15 个（28.3%）——含 mock/dead-code/simulated 信号，无法做真实性能对标。其中 Module 53 已由 Carter 修复。
3. **本质上无可比竞品或无优势潜力的模块**: 约 10 个（18.9%）——多为标准工具封装（如配置管理、日志），无公开竞品基准。

**战略建议**: 
- 优先执行 Top 3（Module 45 异常检测、Module 29 狩猎引擎、Module 10 调度器），这三者都有真实可运行代码 + 明确竞品 + 可复现指标。
- 并行推进 Dead-code 黑名单的修复（Module 2/11/21/22/23），为后续对标扫清障碍。
- Module 10 已进入帕累托前沿精细调优阶段，属"方向正确、持续深挖"范畴，不应因单次未达显著而放弃。

---

## 六、执行追踪

所有 benchmark 结果统一落盘至 `tmp/benchmark_summary.json`，每个模块的详细验证文档命名为 `docs/performance-validation-module-<N>.md`。
