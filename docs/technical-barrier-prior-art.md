# 技术壁垒公开先例检索报告

> 检索日期: 2026-08-17
> 检索工具: WebSearch + WebFetch
> 诚实性声明: 本报告如实记录所有检索结果，包含对我方不利的发现

---

## 创新点 1: Verifiable Control Plane (Ed25519 + Merkle + 离线验证器)

### 检索关键词

1. `"certificate transparency" "control plane" audit`
2. `"merkle log" "ed25519" kubernetes audit`
3. `sigstore cosign rekor supply chain provenance`
4. `"verifiable data structures" cloud audit trail transparency log`
5. `in-toto provenance kubernetes control plane attestation`
6. `Google Trillian transparency log server actions audit`

### 最接近先例

| # | 先例名称 | 覆盖度 | 来源 |
|---|---------|--------|------|
| 1 | **VAOL (Verifiable AI Output Ledger)** | **高** | https://github.com/ogulcanaydogan/Verifiable-AI-Output-Ledger |
| 2 | **Sigstore/Rekor** | 中 | https://github.com/sigstore/rekor |
| 3 | **Google Trillian** | 中 | https://github.com/google/trillian |
| 4 | **Transparency.dev — 服务器操作审计方案** | 中-高 | https://transparency.dev/application/reliably-log-all-actions-performed-on-your-servers/ |
| 5 | **Kubernetes 不可变审计追踪方案** | 中 | https://oneuptime.com/blog/post/2026-02-09-immutable-audit-trails-kubernetes/view |

### 先例具体内容

**VAOL** — 开源项目，使用 Ed25519 签名 + RFC 6962 Merkle 树 + DSSE 信封 + 哈希链，为 AI/LLM 推理输出提供可篡改检测的审计账本。支持离线验证器（CLI Verifier）、OPA 策略引擎、多租户 JWT 认证。技术栈与我方高度重叠：Ed25519 签名、RFC 6962 Merkle 证明、哈希链、离线验证。

**Sigstore/Rekor** — CNCF 项目，提供软件供应链透明日志。使用 Merkle 树（RFC 6962）存储签名事件的不可变记录。支持 cosign 签名容器镜像并生成包含证明（inclusion proof）的收据。主要面向软件供应链（artifact 签名），而非运行时控制平面操作审计。

**Google Trillian** — 通用透明日志基础设施，Certificate Transparency 的泛化实现。Transparency.dev 文档明确描述了"记录服务器上所有操作"的用例：命令提交到可验证日志 → agent 验证命令已入日志后才执行 → agent 网络持续验证日志的 append-only 性质。这与我方"控制平面操作先记账再执行"的模式高度一致。

**Kubernetes 不可变审计方案** — 教程级内容，描述如何结合 API server audit webhook + 加密签名 + 哈希链 + WORM 存储实现不可变审计。覆盖了签名+哈希链的组合，但未集成 RFC 6962 Merkle 透明日志和离线验证器。

### 我方差异点

1. **应用域差异有限**: 我方将此技术用于"云控制平面操作审计"，而 VAOL 用于"AI 推理审计"、Sigstore 用于"供应链签名"、Trillian 文档已描述了"Kubernetes API 操作审计"用例。控制平面审计并非新颖应用域。
2. **组件组合无独创性**: Ed25519 + 哈希链 + RFC 6962 Merkle + 离线验证器的组合已在 VAOL 中完整实现。
3. **可能的差异点**: 我方如果集成了 ZKP（零知识证明）保护隐私的验证（即不暴露操作明文也能验证完整性），或嵌入到 Kubernetes admission webhook 作为 fail-closed 门禁，可能构成增量差异。

### 壁垒强度评级: 弱

**理由**: VAOL 项目已开源实现了完全等价的技术组合（Ed25519 + RFC 6962 Merkle + 哈希链 + 离线验证器）。Transparency.dev/Trillian 生态已明确描述了将此模式应用于服务器操作审计（含 Kubernetes API 调用）的架构。我方差异仅在工程应用层面（针对自有平台的集成），而非技术创新层面。

### 来源链接

- https://github.com/ogulcanaydogan/Verifiable-AI-Output-Ledger
- https://github.com/sigstore/rekor
- https://github.com/google/trillian
- https://transparency.dev/application/reliably-log-all-actions-performed-on-your-servers/
- https://oneuptime.com/blog/post/2026-02-09-immutable-audit-trails-kubernetes/view

---

## 创新点 2: Run-mode Honesty Framework

### 检索关键词

1. `"simulation mode" "production guard" kubernetes "fail fast"`
2. `"mock detection" "fail fast" production microservice boot guard`
3. `feature flags "real vs simulated" capability registry subsystem`
4. `"production readiness" "mock service" detection guard prevent startup`
5. `"capability registry" subsystem real simulated reporting production enforcement`
6. `"run mode" "production mode" detect mock stub reject startup`

### 最接近先例

| # | 先例名称 | 覆盖度 | 来源 |
|---|---------|--------|------|
| 1 | **Spring Boot Fast-Fail 启动诊断** | 低 | https://learncodewithdurgesh.com/tutorials/spring-boot-tutorials/fast-fail-mechanisms-startup-diagnostics-in-spring-boot |
| 2 | **Kubernetes Readiness/Liveness Probes** | 低 | Kubernetes 官方文档（通用知识） |
| 3 | **Feature Flag 系统 (LaunchDarkly/Unleash)** | 低 | 通用知识 |

### 先例具体内容

**Spring Boot Fast-Fail** — 检测配置错误、数据库连接失败等问题并在启动时快速失败。这是通用的"依赖不满足则拒绝启动"模式，但不涉及"检测模拟后端"或"子系统 real/simulated 状态上报"。

**Kubernetes Probes** — readinessProbe/livenessProbe 确保 Pod 健康才接受流量，但无法区分"真实后端"与"模拟后端"。

**Feature Flag 系统** — 可以标记功能为 enabled/disabled，但没有"检测到 mock 即拒绝在生产模式启动"的安全语义。

**未找到的内容**: 经过 6 组关键词的广泛搜索，未检索到任何公开项目实现以下完整组合：
- 生产模式下自动检测模拟/mock 后端并拒绝启动
- 中央 Capability Registry 要求每个子系统主动声明 real/simulated 状态
- 基于声明结果的 fail-fast 策略执行

### 我方差异点

1. **全新机制**: "检测模拟后端即拒绝启动"作为安全不变量（security invariant）而非仅仅是健康检查，在公开文献中未见先例
2. **Capability Registry 模式**: 要求每个子系统在注册时声明 real/simulated 状态，并由中央策略引擎执行"生产模式下不允许任何 simulated 子系统"的策略，属于架构层面的诚实性保证
3. **防伪保证**: 不同于 Feature Flag（开发者手动配置），这是一种运行时自动检测机制，防止"演示模式代码意外部署到生产"

### 壁垒强度评级: 强

**理由**: 经过广泛检索，未发现任何公开项目或论文描述"生产模式检测到模拟后端即拒绝启动 + 子系统 capability 声明 real/simulated + 中央策略执行"的完整框架。现有的 fail-fast 模式仅针对"依赖不可用"而非"依赖是模拟的"。这是一个面向系统诚实性（honesty）而非可用性（availability）的全新设计范式。

### 来源链接

- https://learncodewithdurgesh.com/tutorials/spring-boot-tutorials/fast-fail-mechanisms-startup-diagnostics-in-spring-boot
- 检索词 `"mock detection" "fail fast" production` 未返回相关项目
- 检索词 `"capability registry" subsystem real simulated` 仅返回 AI Agent 相关的 capability registry 概念，与运行时诚实性框架无关

---

## 创新点 3: 中央待调度池 Aging-Urgency 机制

### 检索关键词

1. `"head of line blocking" GPU scheduler cluster scheduling`
2. `"central pending pool" scheduling "aging priority" GPU cluster`
3. `Kueue Volcano gang scheduler "priority aging"`
4. `GPU scheduling "starvation prevention" "aging" priority boost queue`
5. `Kueue workqueue starvation "borrowing" "priority" "preemption"`

### 最接近先例

| # | 先例名称 | 覆盖度 | 来源 |
|---|---------|--------|------|
| 1 | **HiSS (Non-Preemptive GPU Scheduling)** | 中 | https://ieeexplore.ieee.org/document/11245468/ |
| 2 | **A Fair DL Scheduler for Multi-Tenant GPU Clusters (TPDS 2022)** | 中-高 | https://tianweiz07.github.io/Papers/22-TPDS.pdf |
| 3 | **Reducing Fragmentation and Starvation (Mamirov 2024)** | 高 | https://arxiv.org/html/2512.10980v1 |
| 4 | **Power-Grid-Inspired GPU Scheduling (Springer 2026)** | 中-高 | https://link.springer.com/article/10.1186/s44147-026-01033-3 |
| 5 | **Kueue/Volcano** | 中 | https://kubernetes.io/docs/concepts/cluster-administration/flow-control/ |
| 6 | **Elastic HPC Job Scheduler with Aging Priorities** | 中 | https://dl.acm.org/doi/10.1145/3731599.3767358 |

### 先例具体内容

**Mamirov 2024 (arXiv 2512.10980)** — 明确提出 Hybrid Priority Scheduler (HPS)，结合"efficiency-driven selection + aging-based fairness + GPU-blocking mitigation"。使用中央 pending job pool，通过 aging 机制防止饥饿（将饥饿作业从 156 个降至 12 个）。这是最接近的先例，且核心思路与我方高度重叠。

**Springer 2026 论文** — 提出层次化 GPU 调度算法：(1) 在线状态估计与预测 (2) 多目标优先级建模含 urgency/SLA slack/aging (3) 闭环反馈学习。文中明确描述了 HOL blocking 问题及解决方案。

**HiSS** — 通过"优先处理小/短作业并缓解队头阻塞"来降低平均作业排队时间。

**Kueue/Volcano** — Kubernetes 原生批调度系统，Kueue 支持 priority + preemption + borrowing，Volcano 支持 gang scheduling。但未见明确的"per-node FIFO → 中央池 + aging"的架构转换描述。

**Elastic HPC Scheduler (ACM 2026)** — 明确提到"aging priorities to prevent low-priority job starvation"作为调度策略的一部分。

### 我方差异点

1. **组合非独创**: "中央池 + aging priority + 防饥饿"的组合在多篇学术论文中已有描述（特别是 Mamirov 2024 的 HPS）
2. **可能差异**: 如果我方的 aging-urgency 机制专门针对"per-node FIFO 到中央池的架构转换"，并结合了 GPU 拓扑感知（NVLink affinity），可能存在增量创新
3. **HOL blocking 问题的解决**: 已是 GPU 调度领域的标准研究方向

### 壁垒强度评级: 弱

**理由**: "中央调度池 + aging priority + 解决 HOL blocking"的组合在近年 GPU 调度文献中已被多篇论文独立提出和验证（Mamirov 2024 HPS、Springer 2026 hierarchical scheduler）。aging-based fairness 是调度领域的标准技术。我方如果不在 GPU 拓扑感知或具体 aging 公式上有显著创新，则差异仅在工程实现层面。

### 来源链接

- https://arxiv.org/html/2512.10980v1
- https://link.springer.com/article/10.1186/s44147-026-01033-3
- https://ieeexplore.ieee.org/document/11245468/
- https://tianweiz07.github.io/Papers/22-TPDS.pdf
- https://dl.acm.org/doi/10.1145/3731599.3767358

---

## 创新点 4: WASM 沙箱的 GPU WASI 扩展 (NVLink 拓扑查询 + MIG/MPS 分配)

### 检索关键词

1. `"WASI GPU" extension WebAssembly device`
2. `WebAssembly GPU scheduling NVLink topology MIG`
3. `"WasmEdge" nvidia GPU "device plugin" kubernetes`
4. `"wasm" "device plugin" kubernetes GPU allocation`
5. `kube-scheduler-wasm-extension GPU resource allocation`
6. `WASI webgpu "0.3" compute NVLink MIG`

### 最接近先例

| # | 先例名称 | 覆盖度 | 来源 |
|---|---------|--------|------|
| 1 | **wasi:webgpu (Phase 2, v0.3 RC)** | 低-中 | https://github.com/WebAssembly/wasi-webgpu |
| 2 | **kube-scheduler-wasm-extension** | 低 | https://github.com/kubernetes-sigs/kube-scheduler-wasm-extension |
| 3 | **NVIDIA Device Plugin for K8s** | 低 | Kubernetes 生态标准组件 |
| 4 | **Kubernetes DRA (Dynamic Resource Allocation)** | 低 | https://github.com/kubernetes/enhancements/blob/master/keps/sig-node/4381-dra-structured-parameters/README.md |

### 先例具体内容

**wasi:webgpu (Phase 2)** — WebAssembly 社区的 Phase 2 提案，将 WebGPU 风格的 GPU 访问映射到 WASI 环境。当前为 v0.3.0-rc.2，仅提供通用 GPU compute 接口（基于 WebGPU API 模型）。明确不涉及 NVLink 拓扑查询、MIG 分区管理或 MPS 分配。其定位是"通用、可移植的 GPU compute"，抽象层级远高于硬件拓扑管理。

**kube-scheduler-wasm-extension** — Kubernetes SIG 项目，允许用 WASM 编写 kube-scheduler 插件（Filter/Score 等扩展点）。仅是"用 WASM 写调度插件"，而非"在 WASM 沙箱中暴露 GPU 管理 host 函数"。

**NVIDIA Device Plugin** — K8s 标准方式管理 GPU 设备分配，支持 MIG。但完全不涉及 WASM。

**Kubernetes DRA** — 替代 device plugin 的新一代设备分配 API，支持结构化参数。文档中提到"kube-scheduler-wasm-extension 正在探索使用"，但未见 GPU 拓扑感知的 WASM host function 设计。

**关键发现**: 经过广泛搜索，未找到任何项目将以下能力组合在一起：
- WASM 沙箱中暴露 GPU 硬件管理 host 函数
- NVLink 拓扑查询作为 WASI 扩展
- MIG/MPS 分配操作通过 WASM host function 暴露
- 在安全沙箱边界内进行 GPU 设备分配决策

### 我方差异点

1. **全新设计空间**: 将 GPU 硬件管理（NVLink 拓扑查询、MIG 分区）作为 WASI host function 暴露给沙箱化插件，在公开文献和开源项目中完全没有先例
2. **wasi:webgpu 不覆盖此需求**: wasi:webgpu 提供的是"通用 GPU compute API"（着色器/计算管线），而非"GPU 设备管理/分配 API"。两者解决完全不同的问题
3. **安全边界创新**: 在 WASM 沙箱边界内安全地暴露硬件拓扑信息和设备分配操作，需要专门的 capability-based 安全模型设计
4. **kube-scheduler-wasm-extension 的方向不同**: 该项目是"用 WASM 写调度逻辑"，而我方是"在 WASM 中暴露 GPU 管理原语"，二者互补但不重叠

### 壁垒强度评级: 强

**理由**: 经过 6 组关键词的广泛搜索，未发现任何公开项目或提案将 NVLink 拓扑查询和 MIG/MPS 分配操作作为 WASM host function 暴露。wasi:webgpu 工作在完全不同的抽象层级（GPU compute API vs GPU device management API）。这是一个未被探索的设计空间，在 WASM 安全沙箱中暴露底层 GPU 硬件管理能力具有显著的技术独创性。

### 来源链接

- https://github.com/WebAssembly/wasi-webgpu/issues/62
- https://www.webgpu.com/news/wasi-webgpu-03-rc-gpu-compute/
- https://github.com/kubernetes-sigs/kube-scheduler-wasm-extension
- https://github.com/kubernetes/enhancements/blob/master/keps/sig-node/4381-dra-structured-parameters/README.md

---

## 综合评估矩阵

| 创新点 | 壁垒评级 | 最接近先例 | 差异化程度 |
|--------|---------|-----------|-----------|
| 1. Verifiable Control Plane | **弱** | VAOL (完全等价技术栈) | 仅应用域不同 |
| 2. Run-mode Honesty Framework | **强** | 无等价先例 | 全新设计范式 |
| 3. 中央池 Aging-Urgency | **弱** | Mamirov 2024 HPS | 标准学术方向 |
| 4. WASM GPU WASI 扩展 | **强** | wasi:webgpu (不同层级) | 未探索的设计空间 |

## 建议

1. **创新点 2 和 4** 可作为技术壁垒的核心主张，具有可核验的独创性
2. **创新点 1** 需要重新定位差异点（建议强调 ZKP 隐私保护验证、或 admission webhook fail-closed 门禁等 VAOL 未覆盖的能力）
3. **创新点 3** 需要在具体 aging 公式或 GPU 拓扑约束集成上找到区分点，否则难以作为壁垒主张
4. 创新点 1 的发现（VAOL 项目）对我方是不利证据，必须在壁垒叙事中诚实处理

---

*本报告基于 2026-08-17 的公开信息检索，不构成法律意见。专利检索需另行委托专业机构。*
