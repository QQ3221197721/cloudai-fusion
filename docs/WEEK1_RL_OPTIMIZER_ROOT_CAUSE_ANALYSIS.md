# Week 1 Root Cause Analysis Report

**Module 10 RL Optimizer — Deep Research & Root Cause Analysis**
**项目**: CloudAI Fusion (`d:\IdeaProjects\untitled\cloudai-fusion`)
**范围**: `ai/scheduler/advanced_trainer.py`, `ai/scheduler/distributed_trainer.py`, `ai/scheduler/train.py`, `ai/scheduler/provenance.py`, `pkg/scheduler/deep_rl_optimizer.go`, `pkg/scheduler/rl_optimizer.go`, `pkg/scheduler/scheduler_comparison_bench_test.go`, `ai/tests/test_scheduler.py`, `ai/tests/test_distributed_trainer.py`

---

## Executive Summary

Eric 审计确认的三个 verified defects（状态表示不完整、奖励函数错位、探索不足）**全部在代码中逐行核实成立**，但 Week 1 深读发现了比审计报告**更严重的结构性事实**：

1. **RL 子系统实际上存在四层互不相通的实现**，而非单一"DQN"：Python SB3 PPO/SAC（`advanced_trainer.py`）、Python 手写表格 Q（`train.py`）、Go 手写 DQN（`deep_rl_optimizer.go`）、Go 表格 Q + ONNX 桥（`rl_optimizer.go`）。Eric 审计的两份内部文档对缺陷载体的记载互相矛盾（`EXECUTION_SUMMARY_53MODULES.md` L62 称 DQN 在 `ai/scheduler/advanced_trainer.py`——该文件实为 PPO/SAC；`docs/53-modules-complete-summary.md` L137 称"NO DQN implementation exists"——实际 Go 端 726 行 DQN 存在）。
2. **PPO/SAC 路径在本机从未运行过**：当前 Python 环境未安装 `stable_baselines3`、`gymnasium`、`torch`，且全仓库**没有任何训练产物**（无 `models/` 目录、无 `.onnx` 文件）。`ai/pyproject.toml` 未声明任何运行依赖，CI 中该路径靠 `try/except ImportError` 静默降级（`advanced_trainer.py` L32-56）。
3. **实证复测（本报告新增）**：Go 端公平对比基准显示 DQN 在 Makespan/GPUUtil/Fragmentation 全部 5 项指标垫底（Makespan 2408 min vs 最优 RoundRobin 2204 min，劣化 9.2%）；Python 端 6 项受控实验中 5 项证实了根因假设（1 项被修正为更精确的表述）。
4. **一处测试文档漂移**：`scheduler_comparison_bench_test.go` L779-792 打印的 "ROOT CAUSE"（`Forward()` 忽略权重矩阵、`updateQNetwork()` 无梯度下降）描述的是**已被修复的旧代码**——当前源码已实现真实矩阵乘法（L540-577）与反向传播（L636-721），但学习仍然失败，根因转移到 ε 衰减公式误用、placeholder TD error、全局 min-max 归一化等更深层的缺陷上。

**问题严重程度排序**：Primary blocker 是"奖励信号与环境动力学失真"（Defect #2 深化）+ "观测病态"（Defect #1 深化），二者共同导致任何算法（不只 DQN）都无法在此 MDP 上学到有效策略；探索缺陷（Defect #3）是第三优先级放大器。

---

## 1. Existing Implementation Deep Dive

### 1.0 实现拓扑：四层互不相通的 RL 栈

| 层 | 文件 | 算法 | 观测维度 | 动作空间 | 状态 |
|---|---|---|---|---|---|
| Python-A | `ai/scheduler/advanced_trainer.py` | SB3 PPO+SAC | `num_nodes*6+5 = 65`（L114） | 连续 3 维 Box（L123-127） | 依赖缺失，从未运行 |
| Python-B | `ai/scheduler/train.py` | 手写表格 Q | `num_nodes*4+3 = 35`（L56） | 离散 `num_nodes`（L60） | 可运行，但状态哈希崩溃（§1.4） |
| Python-C | `ai/scheduler/distributed_trainer.py` | 拓扑感知表格 Q + 平均聚合 | GPU id | GPU id | 可运行，但是启发式泄露的 bandit（§1.5） |
| Go-A | `pkg/scheduler/deep_rl_optimizer.go` | 手写 DQN（自称 Patent #24） | 硬编码 50 维（L167） | 离散 8（L168） | 可运行，学习无效（§1.1-1.3） |
| Go-B | `pkg/scheduler/rl_optimizer.go` | 表格 Q + ONNX/远程推理桥 | 5 元组离散字符串键（L44-51） | 4×3×2=24 组合动作（L173-191） | 可运行，ε 耗尽 + 负 Q 钳制 |

**断层证据**：
- Python-A 的 ONNX 导出（`advanced_trainer.py` L498-538, L668-707）导出 65 维输入、3 维输出的 actor；Go-A 的网络输入 50 维、输出 8 维离散——**观测与动作空间两端完全不兼容**，模型不可能跨端迁移。
- Go-B 的 `NeuralPolicyBridge` 声称支持 ONNX（`rl_optimizer.go` L386-390），但 `modelLoaded` 字段初始化为 `false` 后**没有任何代码路径将其置 true**（L405，全文检索确认）；实际唯一神经路径是 HTTP 远程推理（L459-502），而 Python 侧不存在对应的 `/api/v1/rl/predict` 服务端实现（全仓库检索无果——待 Week 2 确认是否在其他服务中）。
- `NeuralPolicyConfig.ConfidenceThreshold`（L389）从未被读取——死配置。

### 1.1 State Space Analysis（Defect #1：状态表示不完整）

#### 1.1.1 Python-A `GPUSchedulingGymEnv`（advanced_trainer.py L64-308）

**当前特征清单**（docstring L68-81 + `reset()` L147-157 + `_build_obs()` L307-308）：

每节点 6 维 × N=10：
1. GPU utilization ∈ [10,70]（初始均匀分布，L151）
2. GPU memory usage ∈ [10,60]（L152）
3. CPU utilization ∈ [5,50]（L153）
4. Free GPU count ∈ {2..8}（L154）
5. Node hourly cost = `gpu_type_cost × max_gpus`（L155，注意是**整节点**成本，与实际放置的 GPU 数无关）
6. Topology score ∈ [0.3,1.0]（L156，单标量 NVLink 分数）

作业 5 维（`_generate_workload()` L294-305）：
7. GPU count needed ∈ {1,2,4,8}（L298）
8. Priority ∈ [0,100]（L299）
9. Type ∈ {0,1,2}（L300——**docstring L79 声称 one-hot，实现是普通标量**，文档与实现不符）
10. Estimated duration ~ Exp(2.0)（L301）
11. Deadline pressure ∈ [0,1]（L302）

**关键缺口**（对照审计报告与文献）：

| 缺失特征 | 代码证据 | 后果 |
|---|---|---|
| 队列深度（每节点/全局 pending 数） | `_generate_workload()` L294-305 每步生成**单个 iid 作业**，无队列实体 | 环境退化为"一步一作业"的 bandit 式决策；调度最核心的排队动力学（消解拥塞 vs 贪心）不存在。DeepRM（Mao et al., HotNets'16）将 K 个 pending job slot 作为状态的核心成分 |
| 内存碎片化指标 | 仅瞬时 mem usage（L152） | 无法表达"总量够但碎片放不下大作业"的经典失败模式 |
| GPU 间拓扑距离 | 单节点级 topology score 标量（L156） | 作业跨节点/GPU 的 NVLink/PCIe 距离不可见；Go 端 `gpu_topology.go` 已有 P2P 矩阵能力但未接入 RL 状态 |
| 集群级资源压力 | 无任何全局聚合特征 | 策略无法感知"该收敛还是该疏散" |
| 作业紧迫度（priority×等待时间） | deadline pressure 与 priority 相互独立采样 | 无老化机制，高优先级作业不会随等待升级 |
| Spot 中断历史 | 无 | 抢占式实例调度完全缺位 |
| **观测归一化** | observation_space 为粗糙的全局 Box(low=-1, high=200)（L115-120）；训练器未使用 `VecNormalize` | **实验 C 证实**：300 次 reset 统计各维 range，min=0（常量维）、median=7、max=100——存在完全不变的常量特征与百倍尺度差并存。未归一化输入直接喂 [256,128,64] MLP，网络病态初始化 |

**对学习的影响**：该观测无法区分"低负载偶发失败"与"饱和期必然失败"，且连续动作（preference）经 `_select_node` 的启发式排名间接映射到节点（见 1.4.3），策略可优化的自由度被启发式预先占据——实验 D 证实 preference∈[0,0.1] 时 100% 落在启发式 rank-0 节点上。

#### 1.1.2 Go-A `DeepRLOptimizer.encodeState`（deep_rl_optimizer.go L277-308）

`State` 结构体（L91-108）声明的字段远比 Python 端丰富（含 RequestQueue、AvgWaitTime、EnergyEfficiency、TimeOfDay 等），但编码实现存在三个致命问题：

1. **变长截断**：特征拼接后 pad 到/截断到硬编码 `inputDim=50`（L300-308）。`RequestQueue` 每项贡献 3 维（L290-292），队列一长，NVLink/聚合/上下文特征全部被截断丢弃——**恰是最重要的全局信息先被扔掉**。
2. **全局 min-max 归一化**（`normalizeFeatures` L456-481）：用**本条样本自身的 min/max** 归一化整条向量。同一物理状态因其他维度的极值不同而得到不同编码——**观测非平稳、不可重复，直接破坏 Markov 性**。正确做法是 per-feature 固定缩放或学习型 running normalization。
3. **对比基准的观测组装**（scheduler_comparison_bench_test.go L333-350 `buildState`）只填了 GPUFeatures/RequestQueue/CurrentLoad——NVLink 特征恒空。DQN 在此观测下 NVLinkSat 却达 56.1%（高于 Random 36.6%），说明信号主要来自 GPUFeatures 的 packing 奖励泄漏（`drlReward` L399-410），非拓扑感知。

#### 1.1.3 Python-B `GPUSchedulingEnv`（train.py L30-148）

每节点 4 维（无成本、无拓扑）+ 作业 3 维（无时长、无 deadline）——是 Python-A 的严格子集，审计缺口同样成立。此外 `done` 恒为 `False`（L94），episode 靠外层固定 100 步截断（L181）。

### 1.2 Reward Function Audit（Defect #2：奖励错位）

#### 1.2.1 Python-A 奖励分解（advanced_trainer.py L163-236, L255-279）

逐步分解（成功放置、无抢占时）：

```
reward = R_util + R_binpack + R_share + R_topo + R_priority + R_cost
```

| 分量 | 位置 | 数值 | 评估 |
|---|---|---|---|
| R_util sweet-spot | L262-267 | new_util∈[65,85]→+6；[50,90]→+3；>95→-2 | **硬边界阶跃奖励**。实验 B2 实测：share_ratio 从 0→1 扫描，奖励序列 +6.57 → +13.01 → +13.60 → +6.53 → +5.04——锯齿状多峰、不单调、不光滑。策略梯度方法在阶跃奖励上收敛极差 |
| R_binpack | L270 | `(new_util-old_util)*0.05` | 方向正确但量级小（≤0.4） |
| R_share | L273-274 | `2.0*(1-share)`，share<1 才给 | **奖励少给 GPU**。作业需求 8 卡、给 2 卡反而加分，且环境对"资源不足导致作业拉长/失败"**无任何惩罚**——SLA 维度缺失 |
| R_topo | L277 | `topo*3.0` | 与动作无关的节点固有属性，等效于让策略挤向高 topo 节点 |
| R_priority | L208-211 | placed 且 priority>80 → +3；失败且 priority>80 → -5 | 方向正确 |
| R_cost | L213-216 | `max(0,(100-node_cost)/100)*2.0` | **实验 B1 证实：200/200 次失败放置全部拿到该奖励**（失败总奖励 -8+cost > -8）。放置失败无成本发生却发成本奖——纯噪声偏置。且 node_cost 是整节点成本（L155），与放置规模无关 |
| R_fail | L191-194 | -8.0 | 失败高优先级净奖励 = -8-5+2×cost_reward ≈ -9.5~-11+1.3；而成功高优先级可达 +6+0.4+2+3+2+3 ≈ +16。对比度尚可，但与 R_share 叠加后出现**退化解**（下述） |
| R_preempt | L185-189 | -3.0，最多释放 2 GPU | 抢占收益封顶（2 卡），几乎总是净亏——策略将学会永不抢占，`preemption_willingness` 动作维度名存实亡 |

**退化解实证（实验 B3）**：固定策略 `share=0`（永远给最少 GPU）1000 步累计 -7422.7，优于 `share=1` 的 -7881.3。**奖励面系统性偏好"饿死作业"**。

**缺失目标**：docstring L89 声称 "utilization + cost_efficiency + SLA_compliance + fairness"，但实现中 **fairness 完全不存在**（全代码检索无 gini/JCT/fair-share 逻辑），SLA 仅以 `sla_violations` 计数器存在（L143, L194）且**不进入奖励**、只进 info。能源效率无。审计报告的"misaligned reward"结论成立且有更具体的机理。

#### 1.2.2 Go-A 奖励（deep_rl_optimizer.go）

`DeepRLOptimizer` 自身**不含奖励函数**——奖励由调用方注入（对比测试中 `drlReward` L399-410：可行性 -1/1 + H100 偏好 0.5 + 装填度）。问题在于 **`calculateTDError`（L449-454）是显式 placeholder**：

```go
nextMaxQ := trans.Reward // Simplified for now
tdError := trans.Reward + 0.99*nextMaxQ - 0.0 // Placeholder
```

即 `priority = |2r|+1e-6`（L237）。所谓 "patented prioritized replay" 的优先级**只由奖励本身决定**：高奖励样本永远高优先级，失败样本（负奖励）几乎不被重放——系统性采样偏差，直接违背 prioritized experience replay（Schaul et al., ICLR'16）按 |TD-error| 采样的设计。

#### 1.2.3 Go-B `CalculateReward`（rl_optimizer.go L288-314）

唯一实现了显式多目标加权的奖励：`0.35*util + 0.25*completion + 0.20*cost + 0.15*wait - preempt_penalty`，最后 `*2-1` 压到 [-1,1]。相对最健康，但：无 fairness、无 energy；权重为硬编码无 SLO 推导；且该奖励与 DeepRLOptimizer/Python 端互不引用——**三套奖励函数零复用**。

### 1.3 Exploration Strategy Review（Defect #3：探索不足）

#### 1.3.1 Go-A ε 衰减公式误用（最严重的探索缺陷）

`SelectAction`（L194-217）：

```go
o.currentEpsilon = math.Max(o.epsilonEnd,
    o.epsilonEnd+(o.epsilonStart-o.epsilonEnd)*math.Pow(o.epsilonDecay, float64(o.globalStep)))
```

`epsilonDecay=0.995` 是**按 episode** 设计的衰减率（构造器 L146），却被作用在 `globalStep`（**逐步**计数器）上。数值后果：

| step | 0.995^step | ε |
|---|---|---|
| 100 | 0.606 | 0.61 |
| 500 | 0.082 | 0.09 |
| 919 | 0.0099 | ≈0.02（触底） |

**约 1000 步后探索永久耗尽**——而经验池容量 100k、`minBatchSize=32`，训练才刚开始。更糟的是 `globalStep` 被双重递增（`SelectAction` L198 每次 +1，`updateQNetwork` L632 每批再 +1），实际衰减再快一倍；且它同时被复用为 target 网络更新节拍（L268），语义纠缠。

修正为 `0.995^(globalStep/1000)` 或按 episode 计数衰减是 Week 2 必改项。

#### 1.3.2 Go-B 在线 ε 耗尽（生产路径缺陷）

`UpdateQValue`（rl_optimizer.go L281-284）每次 Q 更新执行 `ε *= 0.999`：从 0.2 出发约 **2350 次调度后 ε 永久钉死在 0.02**，进程生命周期内不重置、不感知分布漂移。生产中长期运行意味着"上线 2350 单后再无在线学习能力"。

#### 1.3.3 Python-A PPO/SAC 的探索配置

- PPO：`ent_coef=0.01` 恒定（L404），无退火 schedule（未用 `schedule_kwargs`）；SB3 PPO 的探索全靠高斯策略噪声 + entropy bonus，对这种**阶跃奖励面**（§1.2.1）的探索效率低。
- SAC：`ent_coef="auto"`（L586）——机制正确（Haarnoja et al. 2018 的自动温度调参）。但 target_entropy 取 SB3 默认 `-dim(A) = -3`。SB3 作者 Raffin 的调参指南指出：对奖励尺度已塑造良好的环境，`-dim(A)` 通常**过度探索**，建议按问题缩放（如 `-0.5×dim(A)`）并观察 entropy 曲线。本环境奖励量级 ±16，-3 的目标熵需要 Week 2 实验标定（待确认项）。
- 无任何 UCB/计数型探索加成（审计建议项，文献支持在表格法中有效、在连续策略中需换形式）。
- **环境随机性未播种**：`reset()`/`_generate_workload()`/`_apply_placement()` 全部使用全局 `np.random`（L151-156, L287-292, L296-305），未使用 `super().reset(seed=seed)` 提供的 `np.random.Generator`。SB3 向量化 seeding 无法控制环境动态 → 评估回调的对比充满噪声，可复现性为零。

#### 1.3.4 Python-B/C

- Python-B：ε 从 1.0 按 episode 衰减 0.995、下限 0.01（train.py L173-175）——形式正确，但被状态哈希崩溃（§1.4.2）抵消；且用全局 `np.random`（L185）不可复现。
- Python-C：`QLearnerConfig.epsilon=0.1` **固定无衰减字段**（distributed_trainer.py L91-97）；`select_action`（L121-125）的 ε-greedy 用注入 rng（好），但 greedy 分支的 `score_actions().argmax()` 含 +0.5×topology bonus（L113-119）——**实验 A 证实：Q 表全零时 greedy 动作已落在 preferred domain**，`test_learner_prefers_domain_after_training`（test_distributed_trainer.py L60-71）不需要任何 `update()` 即可通过。该测试测的是启发式而非学习。

### 1.4 深层环境缺陷（审计未覆盖、Week 1 新发现）

#### 1.4.1 环境动力学：非 Markov、无队列、退化为 bandit

`step()` 每步生成全新 iid 作业（L222→L294-305），上一作业是否放好对下一作业的唯一影响是节点状态的缓慢随机衰减（L287-292）。**没有队列、没有作业到达过程、没有作业完成事件对齐**——这是"上下文赌博机 + 慢漂移背景"，不是调度 MDP。Go 端对比基准（测试文件 L444-535）反而实现了真正的离散事件仿真（到达、完成、释放资源），**讽刺的是 Python 训练环境比 Go 测试仿真器更简化**。

#### 1.4.2 Python-B 状态哈希崩溃

`state_idx = int(np.sum(obs[:4]) % 100)`（train.py L182）：只取**第一个节点**的 4 维特征求和取模。8 节点 × 连续值 × 35 维真实状态被压进 100 个桶，且丢掉作业特征——状态别名使 Q 更新互相踩踏。`test_train_learning`（test_scheduler.py L177-184）只断言 `np.isfinite`，**从未要求学习改进**——测试绿灯与学习失败并存的结构性原因。

#### 1.4.3 动作间接映射：启发式吃掉策略空间

`_select_node`（advanced_trainer.py L238-253）用固定权重 `0.3*headroom + 0.3*free_ratio + 0.2*cost_eff + 0.2*topo` 给节点打分排序，`node_preference ∈ [0,1]` 只是在**排序结果里选分位**。策略无法表达"我就是要选贵但空闲的节点 7"这类反启发式偏好（除非恰好改变分位）。**RL 的可学习增量被压缩为分位微调**。正确设计要么直接离散选节点（mask 不可行动作），要么预测每节点评分残差。

#### 1.4.4 Go-A DQN 实现细节缺陷清单

- target 网络初始化违背 DQN 惯例：`Copy()` 先于 `InitializeWeights()`（L182-186），复制的是空权重后各自随机初始化 → 初始 online≠target，早期 target 信号是纯噪声；
- `softCopyTargetNetwork`（L724-726）名为 soft 实为**硬拷贝**（Polyak τ=1）；
- 无 Double DQN（L606-613 用 target 网络 max，vanilla DQN 过估计偏差）；
- 损失为 MSE + clip(delta,±1)（L674-679），非 Huber——粗糙但可用；
- `rand.Float64()` 全局源无种子控制（L205, L408）——不可复现实验；
- `ExperiencePool.prioritySum` 只增不减（L346）——死代码（无读者）；
- 逐样本 SGD 而非批平均（L591-631）——可接受但低效。

#### 1.4.5 测试与文档漂移

`scheduler_comparison_bench_test.go` L779-792 的 HONEST REPORTING 打印的根因（"Forward() ignores weight matrices"、"NO gradient descent"）**与当前源码不符**——这两处已实现真实矩阵乘法（L540-577）与反向传播（L636-721，且有修复注释 L328-331/L378-391 表明 SumTree 越界与死循环也被修过）。结论"does NOT beat the topology heuristic"仍然成立（本次复测数据见 §2.3），但**归因已过时**——若以注释为准做修复计划会修错地方。这是"慢验证"原则的直接胜利：必须以当前代码为准。

#### 1.4.6 Provenance 未接线

`provenance.py` 本身实现正确（流式 SHA-256 L39-45、canonical JSON L48-54、字段与 Go 签名器对齐 L58-66），但**全仓库无任何调用点**将其与 `advanced_trainer.py`/`train.py` 的训练输出绑定——RL 训练零溯源。审计任务里"检查记录机制是否正确"的答案：机制正确，集成缺失。

---

## 2. Literature Comparison

### 2.1 PPO（Schulman et al., 2017）与 SB3 最佳实践

- 论文建议 entropy bonus 辅助探索、advantage 归一化；SB3 默认 `normalize_advantage=True`（当前代码受益）。当前 `ent_coef=0.01` 恒定无退火——社区实践（含 Raffin 的 RL tips）普遍对 ent_coef / learning rate 使用线性退火，尤其在奖励面含阶跃时。
- PPO 对**未归一化观测**敏感：本环境 0~100 与 0~1 特征混布（实验 C），标准做法是 `VecNormalize`（obs 归一化必开，reward 归一化视情况）——当前训练器完全未用。

### 2.2 SAC（Haarnoja et al., 2018）

- 自动熵温度 α 是 SAC 的核心优势，当前 `ent_coef="auto"` 正确。
- target_entropy 默认 `-dim(A)`；SB3 维护者 Raffin 的实践文章（"Getting SAC to Work on a Massive Parallel Simulator"及 2025-26 高性能变体文献）指出该默认对"奖励已良好塑造"的任务过度探索，常需缩放至 `-0.5×dim(A)` 量级并配合观察实际 policy entropy 曲线调整。当前 3 维动作、奖励 ±16 的环境需实验标定（Week 4 验证项）。
- SAC 依赖**奖励平滑性**（重放 + Q 回归）：当前阶跃型 sweet-spot 奖励（±6 跳变）对 Q 函数拟合是最坏情况之一。

### 2.3 生产调度系统模式

- **DeepRM**（Mao et al., HotNets'16，RL 调度基线）：image-like 状态（资源 × 时间槽占用剖面 + K 个 pending 作业槽）、奖励 = 平均 slow-down（显式 JCT 对齐业务目标）。本实现"瞬时利用率快照 + 单作业"与之相差一个时代。
- **Quasar/Tiresias/Gandiva**（NSDI/SOSP 生产线）：优先级老化（等待越久权重越高）、风险感知分配、拓扑感知 gang 调度。当前代码的 priority 静态、无老化。
- **Kubernetes 默认调度器**：bin-packing（MostAllocated）与 spread 两种极性 + 打分插件化。当前 Go 端 TopologyAware 启发式（61.0% NVLinkSat）实际就是这类打分器——**基准数据显示启发式已接近该仿真上界，RL 必须先修复 MDP 才可能超越**。
- 本次 Go 端复测（`go test -run TestSchedulerComparison`，2026-08-16）：

| Scheduler | Makespan(min) | GPUUtil% | Wait(min) | NVLinkSat% | Frag% |
|---|---|---|---|---|---|
| Random | 2276.0 | 96.6 | 749.4 | 36.6 | 48.0 |
| RoundRobin | **2204.0** | **98.1** | **661.0** | 36.6 | **47.1** |
| BinPack | 2288.0 | 96.6 | 721.9 | 39.0 | 48.3 |
| TopologyAware | 2291.0 | 96.2 | 661.3 | **61.0** | 48.2 |
| **DRL(DQN)** | **2408.0（最差）** | 94.7（最低） | 680.8 | 56.1 | 49.9（最差） |

### 2.4 与当前实现的关键差异总结

1. 生产/文献系统的状态显式编码**排队与时间剖面**；当前实现无队列实体。
2. 生产系统奖励对齐**端到端 SLO（JCT/slow-down/fairness）**；当前奖励是 7 个启发式分量的阶跃叠加且含退化解向量。
3. 生产系统探索按**环境阶段自适应**（如 ε 按进度、或 SAC α 自动）；当前 Go 端 1000 步耗尽、Python 端无退火、且环境本身不可播种。

---

## 3. Root Cause Summary

**Primary blocker（单点最重要）**：**MDP 建模本身失真**——无队列的 bandit 化环境（§1.4.1）+ 锯齿状/含退化解的奖励面（§1.2.1）+ 病态观测（§1.1.1 归一化缺失、Go 端全局 min-max 破坏平稳性）。在此 MDP 上，任何算法（DQN/PPO/SAC）都不可能学到超越内置启发式的策略；Eric 三缺陷是这一顶层失真的三个切面。

**Secondary blockers（按优先级）**：
1. Go-A ε 衰减公式误用（episode 率作用于 step 计数器）+ globalStep 双重递增 → ~1000 步探索耗尽（§1.3.1）；
2. Go-A `calculateTDError` placeholder → prioritized replay 退化为 reward-biased 采样（§1.2.2）；
3. Go-B 在线 ε 2350 次更新后永久钉死 0.02（§1.3.2）；
4. Python 环境不可播种（全局 `np.random`）→ 实验不可复现、评估噪声（§1.3.3）；
5. Python-B 状态哈希崩溃（§1.4.2）与 Python-C 启发式泄露（§1.3.3 实验A）；
6. 测试只验证"管道通"，不验证"学习发生"（§1.4.2/§1.3.3）；
7. 四层实现观测/动作空间不兼容、provenance 未接线、测试注释漂移（§1.0/§1.4.5/§1.4.6）。

**Recommended fix sequence**（严格顺序，先地基后算法）：
1. 重建环境（队列 + 到达过程 + 离散事件动力学 + 可播种 RNG + per-feature 归一化）；
2. 重写奖励（连续化 sweet-spot、移除失败放置的 cost_reward、加入 JCT/slow-down 与 fairness、消除 share 退化解）；
3. 修 Go 探索（ε 公式、计数器解耦、Double DQN、真 TD-error 优先级）；
4. 统一观测/动作契约（Python 训练 ⇄ Go 推理同一份特征 schema）；
5. 学习性门禁测试（Q 值收敛、策略改进断言、对照基线回归门禁）；
6. 最后才是超参/熵标定与基准冲刺。

---

## 4. Proposed Fix Plan (High-level)

### Week 2: 环境与奖励重建（Python-A + Go 契约）

- **新 `SchedulingEnvV2`**（替换 `GPUSchedulingGymEnv`）：
  - 状态：每节点 [util, mem, mem_frag, free_gpus, cost_per_gpu, topo_affinity] + 全局 [queue_depth, cluster_pressure(t-1 压力差分), 抢占配额余量] + 队列前 K=8 作业 [gpu_need, priority, waited/deadline, duration, type_onehot(3)]；全部 per-feature 固定缩放到 [0,1]，写入共享 schema 文档供 Go 端复用；
  - 动作：改为离散节点选择 + MultiBinary 可行性 action mask（或保留连续但直接输出 per-node score residual，废弃分位映射）；
  - 动力学：泊松到达 + 作业时长 + 完成释放（对齐 Go 测试仿真器 L444-535 的离散事件模型），`np.random.Generator` 全程注入。
- **奖励 V2**：`r = w1*(-Δavg_slowdown) + w2*(-Δgini) + w3*(-cost_rate) + w4*(-energy_est) + w_fail*(-violatioin)`；sweet-spot 改为二次型惩罚 `-(util-0.75)²`；失败不给 cost_reward；share 惩罚与资源不足挂钩（需求 k 卡给 m<k 卡按 (1-m/k) 罚）。权重的初始值由 Go-B `CalculateReward` 的 0.35/0.25/0.2/0.15 比例校准，Week 4 做敏感性扫描。
- **修 Go-A**：ε 公式改 `decay^(step/1000)`；`globalStep` 拆分为 `envSteps`/`gradSteps`；Double DQN action selection；`calculateTDError` 接入真 Q 值（存储 transition 时的 Q 与 nextQ）；SumTree 采样加 IS 权重（或降级 uniform + 周期优先级重算，诚实标注）。

### Week 3: 探索策略与训练管线

- PPO：`ent_coef` 线性退火 0.02→0.001（`schedule_kwargs`）；`VecNormalize(obs)` 必开；
- SAC：target_entropy 扫描 {-3, -1.5, -0.75}，按 policy entropy 与 eval 曲线选择；
- Go-B：ε 衰减改为按"自上次策略改进以来的样本数"重置的窗口衰减，感知漂移；
- ONNX 契约统一：单一 65→3 模型 + Go 端确定性推理路径（消除 50 维/8 动作的自制网络或明确其只作离线对照）；
- provenance 接线：训练完成即生成 weights_hash/train_config_hash 并写 JSON。

### Week 4: Validation Phase

- **训练协议**：固定 10 组环境种子 × 3 个算法种子；PPO 500k / SAC 300k steps；每 25k 评估 50 episodes（确定性）；记录 reward/Q-mean/policy-entropy/eval 曲线。
- **基准**：复用 Go 端 8 GPU/100 作业离散事件仿真（同一 workload seed=42），指标 = Makespan / GPUUtil / Wait / NVLinkSat / Frag + 新增 AvgSlowdown 与 Gini。
- **验收标准**（对齐 EXECUTION_SUMMARY 验收红线，全部满足才可通过）：
  1. 学习门禁：eval reward 单调改善且后 1/3 训练期方差 < 10%；Q-mean 收敛曲线平台化；
  2. 性能门禁：vs RoundRobin（makespan 最优基线）改善 > 10%，vs TopologyAware 的 NVLinkSat 差距 < 2pt 或反超；
  3. 消融门禁：无-公平性项与有-公平性项的 Gini 差异显著（验证多目标真实生效）；
  4. 诚实性门禁：所有指标来自真实仿真事件；任何 FAIL 原样上报，禁止调参至过拟合单一 seed。

---

## 5. Risks & Assumptions

**Technical risks**:
1. 奖励连续化（二次型替代阶跃）可能让早期训练信号变弱（risk: 平坦奖励面）——缓解：保留小幅度阶跃残差 + potential-based shaping 做差分；
2. 队列化环境使 episode 变长、样本成本升高（risk: 500k steps 预算不够）——缓解：优先 SAC（样本效率）+ Go 仿真器做高速评估器；
3. Go/Python 双端特征 schema 演进中再次漂移（risk: 修复后复现旧问题）——缓解：单一 JSON schema + 双端契约测试；
4. Double DQN + IS 权重引入新实现 bug 面（risk: 学习更差且难归因）——缓解：每改动独立 A/B 消融。

**Assumptions**:
1. 假定 `/api/v1/rl/predict` 无 Python 服务端实现（全仓库检索未见；如存在于别处需修正 §1.0 结论）——**待确认**；
2. 假定 SB3/torch/gymnasium 可以安装且版本 ≥ SB3 2.x（ONNX wrapper 的 `mlp_extractor`/`actor.mu` API 假定 2.x 结构）——Week 2 安装时验证；
3. 假定 Go 对比仿真器的 8 GPU/100 作业负载足以代表目标场景（100 作业、单 wave 到达）——Week 4 增加多波次/异构到达率负载。

**Unknown unknowns（需 Week 2 探索）**:
- 生产流量中 `rl_optimizer.go` 的 `RLOptimizer` 被哪些调用方实际触发（`engine.go` 接线方式未在本次范围内逐行核实——**待确认**）；
- `gpu_topology.go` 的 P2P 矩阵与 RL 状态融合的工程成本；
- Python 端若在 ai-engine 容器内运行，SB3+torch 的镜像体积/启动时间影响。

---

## Appendix A: Week 1 实验记录（可复现）

- 脚本：`d:\IdeaProjects\untitled\tmp\week1_rl_diag.py`（numpy-only，gymnasium/structlog 以 stub 注入，零环境侵入）
- 结果：A=泄露确认（Q=0 时 greedy 落 preferred domain）；B1=200/200 失败放置获 cost_reward；B2=奖励沿 share 呈 +6.57/+13.01/+13.60/+6.53/+5.04 锯齿（修正原"单调偏好少给卡"假设为"多峰不光滑"）；B3=退化解策略优于慷慨策略（-7422.7 vs -7881.3）；C=观测尺度差 ≥100x 且存在常量维；D=preference∈[0,0.1] 100% 选启发式 rank-0。
- Go 基准：`go test ./pkg/scheduler/ -run TestSchedulerComparison -v`（输出见 §2.3 表格）。

---

*Report generated by Qoder RL Engineer Sam*
*Date: August 16, 2026*
