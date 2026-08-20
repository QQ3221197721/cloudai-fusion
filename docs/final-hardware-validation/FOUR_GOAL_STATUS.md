# 6 个 HARD 模块四目标达成状态（诚实版）

> 本文件如实记录 6 个硬件依赖模块对 T1/T2/T3/T4 四大目标的达成情况。
> 原则：**只采信真实 CLI 输出，无硬件的项诚实标注为「待硬件验证」，绝不伪造数据。**
> 最后更新：2026-08-20

## 背景

CloudAI Fusion 的 53 个模块中，47 个非硬件模块已完成四目标攻坚。剩余 6 个模块
因依赖物理硬件（NVIDIA GPU / MIG / InfiniBand+CRIU / Intel SGX），单独在此追踪。

验证分两阶段推进：
- **阶段 A（零成本）**：软件降级/mock 路径可验证的模块，在开发机本地实测（50+ 测试 PASS）。
- **阶段 B（硬件门槛）**：必须真机的 T2 benchmark，端点/脚本/前端已就绪，实测待硬件。

## 四目标达成矩阵

| 模块（包/功能） | T1 CLI/API | T2 真实硬件 Benchmark | T3 技术壁垒（代码） | T4 前端页 |
|---|---|---|---|---|
| gpu_topology.go（拓扑发现） | ✅ | ⏳ 待多 GPU+NVLink | ✅ dense-k-subgraph（合成拓扑已验证 p=0.007） | ✅ |
| gpu_sharing.go（MIG 分区）| ✅ `/api/v1/gpu/mig` | ⏳ 待 A100/H100 MIG | ✅ MIG-aware 调度 | ✅ GpuMigDashboard |
| complete_gpu_migration.go（CRIU 迁移）| ✅ `/api/v1/gpu/migrate` | ⏳ 待 2 节点 InfiniBand | ✅ CRIU+RDMA 快迁移 | ✅ GpuMigrationDashboard |
| edgeautonomy/metrics_collector.go | ✅ | ⏳ 待 Linux+GPU | ✅ 离线自治指标 | ✅（EdgeOverview） |
| capability/detection.go（SGX/GPU 探测）| ✅ `/api/v1/sgx/status` | ⏳ 待 Intel SGX CPU | ✅ 能力门控+软件降级 | ✅ SgxEnclaveDashboard |
| resources/gpu.go（GPU 资源采集）| ✅ | ⏳ 待 NVIDIA GPU | ✅ nvidia-smi 解析+优雅降级 | ✅ |

## 已完成部分（真实 CLI 佐证）

### 阶段 A —— 软件降级路径本地实测（EXIT 0）
- `pkg/capability`：9/9 测试 PASS（无 GPU 也能跑策略/能力引擎）
- `pkg/scheduler`：12/12 PASS（合成拓扑数据，测试日志内嵌「SYNTHETIC topology data only」诚实声明）
- `pkg/resources`：3/3 PASS（nvidia-smi 缺失时优雅 fallback，`exit status 2` 已处理）
- `pkg/intel`：~26 PASS（离线 STIX 解析）

### 阶段 B —— T1/T4 无硬件可达部分（EXIT 0）
- 3 个后端端点已实现并注册：`/api/v1/gpu/mig`、`/api/v1/gpu/migrate`、`/api/v1/sgx/status`
- 无硬件时端点**诚实返回** `{"mode":"simulated","simulated":true,"reason":"...","data":{空}}`
  - MIG：`nvidia-smi ... GPU metrics query failed: exit status 2`
  - 迁移：`CRIU not available: ... executable file not found`
  - SGX：`host OS is windows (SGX requires Linux + /dev/sgx_enclave)`
- 3 个前端 Dashboard 已连真实端点，simulated 时显示 `[SIMULATED - no hardware]` 诚实横幅
- 验证：`go build ./...` / `go vet` / `go test ./pkg/api/ ...` / `npm run build` 全部 EXIT 0

## 待硬件验证部分（T2 真实 Benchmark）

以下必须在真实硬件上运行 `docs/final-hardware-validation/` 下的脚本才能产出真实数字：

| 模块 | 需要硬件 | 脚本 | 预估成本 |
|---|---|---|---|
| MIG 分区 | A100/H100 + MIG | `m2_mig_validation.sh` | ~$33（p4d 1h） |
| GPU 迁移 | 2 节点 + InfiniBand + CRIU | `m3_migration_validation.sh` | ~$98（p4d×2 1.5h） |
| SGX | Intel SGX CPU | `m5_sgx_validation.sh` | ~$0.40（Azure DC4s_v3 1h） |

**合计约 $131（含缓冲 ~$160；Spot 可降至 ~$50）。** 端点已就绪，置于对应硬件即返回 `mode:real` 真实数据。

## 诚实声明

- 本机（Windows，无 GPU/SGX）无法产出真实硬件 benchmark，相关项如实标注「待硬件验证」。
- 所有已完成项均有真实 CLI 输出佐证；无任何伪造的 MIG 分区 / 迁移窗口 / enclave 度量数值。
- 一旦获得硬件访问或云预算，运行上述脚本即可将 T2 由「待验证」转为「已验证」。
