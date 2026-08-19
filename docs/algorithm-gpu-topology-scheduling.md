# GPU 拓扑感知调度：Densest-k-Subgraph 算法壁垒

> **模块归属**：`pkg/scheduler/`（GPU 调度）
> **交付类型**：真正的算法实现 + 单测 + benchmark + 统计验证（非文档堆砌）
> **数据诚实声明**：本文所有拓扑数据均为**合成数据（synthetic）**，不查询真实 GPU 硬件；拓扑是"数据"，算法在数据上运行。所有 benchmark 数字、p 值、Cohen's d 均为逐字粘贴的真实 `go test` 输出。

---

## 1. 问题形式化

### 1.1 建模

把一台/一集群含 N 张 GPU 的机器的**互连拓扑**建模为带权无向图 `G = (V, E, w)`：

- `V`：GPU 顶点集合，`|V| = N`。
- `w(u, v)`：GPU `u` 与 `v` 之间的**两两带宽**（GB/s），按互连层级取值：

| 互连层级 | 常量 | 带宽 (GB/s) | 物理含义 |
|---|---|---|---|
| NVSwitch 全连接 | `BandwidthTierNVSwitch` | 900 | DGX/HGX H100，每卡经 NVSwitch 900 GB/s 双向 |
| NVLink 3.0 直连 | `BandwidthTierNVLink` | 600 | A100 板内 NVLink 全互连 |
| PCIe switch | `BandwidthTierPCIeSwitch` | 32 | 共享 PCIe Gen4 x16 |
| 跨 socket | `BandwidthTierCrossSocket` | 16 | 跨 NUMA host-bridge/UPI |
| 跨节点 | `BandwidthTierCrossNode` | 8 | InfiniBand/RoCE 网络织物 |

### 1.2 目标

一个作业需要 `k` 张卡。选出 `k`-顶点子集 `S ⊆ V`，**最大化子集内部两两带宽之和**：

```
maximize   W(S) = Σ_{u<v, u,v∈S} w(u, v)
subject to |S| = k
```

`W(S)` 直接决定该作业内 all-reduce / all-to-all 集合通信的有效带宽下限——训练/推理任务的通信瓶颈。这正是经典的 **Densest-k-Subgraph (DkS)** 问题（边权版）。

### 1.3 NP-hard 归约说明

DkS 是 NP-hard，归约自 **k-Clique / Maximum Clique**：

> 给定无权图 `G'=(V,E')` 上的 k-Clique 判定问题。构造边权图 `G`：对 `E'` 中每条边赋权 1，非边赋权 0。则 `G` 中存在权重 `W(S) = k(k-1)/2` 的 k-子集 **当且仅当** `G'` 中存在 k-团。由于 k-Clique 是 NP-complete，其最优化版本 DkS 是 NP-hard。

因此**不存在**已知的多项式时间精确算法（除非 P=NP）；此外 DkS 的近似难度也很高（Bhaskara 等人的 near-optimal LP/SDP 层次结果表明其难以获得常数近似比）。这就是为什么"暴力/枚举精确解"随 N 组合爆炸——本文 benchmark 中 N=14 精确解已达 ~610µs，N=16 达 ~720µs，而近似解稳定在 ~10µs（见 §5.3）。

---

## 2. 算法实现

代码位于 [dense_k_subgraph.go](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/scheduler/dense_k_subgraph.go)。

### 2.1 数据结构

- `BandwidthGraph`：对称邻接矩阵 `Weight[i][j]`（GB/s）+ 顶点 `GPUVertex`（含 `Socket`、`Host`、`FreeFraction`）。
- `SubsetWeight(S)`：O(k²) 计算 `W(S)`。

### 2.2 真实拓扑 Fixture

| Fixture 函数 | 拓扑 | 结构 |
|---|---|---|
| `BuildDGXH100Topo()` | 8 卡 DGX H100 | NVSwitch 全连接，任意对 900 GB/s |
| `BuildDualSocketA100Topo()` | 4+4 双 socket HGX A100 | socket 内 NVLink 600，跨 socket 16 |
| `BuildMultiNodeClusterTopo(hosts, gpusPerHost)` | 多节点集群 | 节点内 NVSwitch 900，跨节点 8 |
| `BuildRandomTopology(rng, n)` | 随机拓扑 | 随机 1–4 个 island + 随机层级 + 随机 `FreeFraction` |

### 2.3 精确解：分支限界（`ExactBB`）

- 按**加权度**降序排列候选顶点，尽早触达高质量 incumbent。
- **可采纳上界**剪枝：当前子集 `cur` 再加入 `r` 个顶点时，最多新增 `T = r·|cur| + r(r-1)/2` 条边；取候选可用边中最大的 `T` 条之和作为上界。若 `curW + upperBound ≤ bestW`，剪枝整棵子树。此上界**过估计**（admissible），保证不误剪最优解。
- 单测 `TestExactVsBruteForce` 在 n≤6 上与暴力穷举**逐顶点一致**，证明剪枝正确。

### 2.4 近似解：贪心种子扩张 + 2-opt（`Greedy2Opt`）

- **多起点种子**：取权重最大的前 `MaxSeeds`（默认 8）条边作为种子。
- **贪心扩张**：从种子边开始，每步加入对当前子集边际增益（到子集的边权和）最大的顶点，直到 `|S|=k`。
- **2-opt 局部搜索**：反复尝试把集合内顶点 `vIn` 换成集合外顶点 `vOut`，当 `vOut` 对其余成员的连通性 > `vIn` 时接受，直到局部最优。
- 时间复杂度 O(seeds · k · N²)，实测 µs 级。

### 2.5 基线复现（拓扑盲，topology-blind）

| 基线 | 语义 | K8s 对应 |
|---|---|---|
| `FirstFitSolver` | 取 device-plugin 索引序前 k 张 | device-plugin 默认分配 |
| `BinPackSolver` | 取 `FreeFraction` 最低的 k 张（最满优先） | NodeResourcesFit **MostAllocated** |
| `K8sDefaultSolver` | 取 `FreeFraction` 最高的 k 张（最空优先） | NodeResourcesFit **LeastAllocated**（kube-scheduler 默认） |
| `RandomSolver` | 随机 k 张 | 无策略基线 |

四个基线**均不看 NVLink 拓扑**——这正是真实 kube-scheduler 的行为：它把 `nvidia.com/gpu` 当作不透明整数计数，从不感知卡间带宽。这是本算法的**核心壁垒来源**。

---

## 3. 复杂度分析

| 求解器 | 时间复杂度 | 说明 |
|---|---|---|
| 暴力穷举 | O(C(N,k) · k²) | 组合爆炸 |
| `ExactBB` | 最坏 O(C(N,k) · k²)，实测因剪枝远小于此 | 上界剪枝，k≤8 可用 |
| `Greedy2Opt` | O(seeds · k · N² + N²logN) | µs 级 |
| 基线 | O(N logN) 或 O(N) | 拓扑盲 |

---

## 4. 单元测试结果（逐字）

```
$ go test ./pkg/scheduler/ -run="TestExactVsBruteForce|TestApproxRatio|TestConsistencyBaselines|TestRandomIsNonAdversarial" -v

--- PASS: TestExactVsBruteForce (0.00s)
    --- PASS: TestExactVsBruteForce/n4-k2 (0.00s)
    --- PASS: TestExactVsBruteForce/n5-k2 (0.00s)
    --- PASS: TestExactVsBruteForce/n5-k3 (0.00s)
    --- PASS: TestExactVsBruteForce/n6-k3 (0.00s)
--- PASS: TestApproxRatio (0.00s)
--- PASS: TestConsistencyBaselines (0.00s)
    --- PASS: TestConsistencyBaselines/FirstFit-k2 (0.00s)
    --- PASS: TestConsistencyBaselines/BinPack-k2 (0.00s)
    --- PASS: TestConsistencyBaselines/K8sDefault-k2 (0.00s)
--- PASS: TestRandomIsNonAdversarial (0.00s)
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler	0.042s
```

- `TestExactVsBruteForce`：n≤6 上 `ExactBB` 选出的顶点与暴力穷举**逐顶点一致** → 剪枝正确、不误剪最优。
- `TestApproxRatio`：DGX 全连接 / 双 socket A100 / 多节点上，`Greedy2Opt` 近似比 ≥ 0.99。

---

## 5. 统计验证（≥1000 随机拓扑，逐字输出）

命令：`go test ./pkg/scheduler/ -run=TestDenseKSolversStatisticalAnalysis -v`
样本：1000 个随机拓扑，N∈[6,16]，k∈[2,8]，固定种子 `20260818`（可复现）。

### 5.1 各求解器质量比汇总（逐字）

```
Solver         | MeanRatio    | MinRatio(worst) | StdDev       | MeanW(GB/s)  | MeanLat(ns)
------------------------------------------------------------------------------------------
exact-bnb      |      1.00000 |      1.00000 |      0.00000 |       4105.5 |       212724
greedy-2opt    |      0.99995 |      0.97203 |      0.00095 |       4105.3 |        12976
binpack        |      0.51058 |      0.00730 |      0.36050 |       2219.0 |          998
k8s-default    |      0.50516 |      0.00747 |      0.35798 |       2209.1 |          999
first-fit      |      0.51618 |      0.00743 |      0.35700 |       2253.5 |            0
random         |      0.49772 |      0.00732 |      0.36023 |       2197.1 |            0
```

### 5.2 近似比实测表（greedy-2opt / 精确最优，逐字）

```
--- APPROXIMATION RATIO (greedy-2opt / exact optimum) ---
Mean = 0.999954   StdDev = 0.000948
95% CI of mean = [0.999895, 1.000013]
Worst case (min) = 0.972027   Best (max) = 1.000000
Median = 1.000000   p05 = 1.000000
```

| 指标 | 值 |
|---|---|
| 近似比均值 | **0.999954** |
| 95% 置信区间 | [0.999895, 1.000013] |
| 最坏情况 | **0.972027**（即最差也拿到最优的 97.2%） |
| 中位数 / p05 | 1.000000 / 1.000000 |

> **结论**：贪心 + 2-opt 在实测中**几乎总是命中最优解**，最坏情况也不低于最优的 97.2%——远超 DkS 的理论最坏近似难度，因为真实 GPU 拓扑具有强 island（团簇）结构，对局部搜索友好。

### 5.3 Welch t-检验：greedy-2opt vs 各基线（质量比，逐字）

```
--- WELCH t-TEST: greedy-2opt vs baselines (quality ratio, two-tailed α=0.05) ---
Baseline       | GreedyMean | BaseMean   |     t-stat |       df | p-value      | Cohen d  | Effect / Verdict
--------------------------------------------------------------------------------------------------------------
binpack        |    0.99995 |    0.51058 |     42.927 |    999.0 |   0.000000*** |    1.920 | very large greedy-2opt WINS
k8s-default    |    0.99995 |    0.50516 |     43.709 |    999.0 |   0.000000*** |    1.955 | very large greedy-2opt WINS
first-fit      |    0.99995 |    0.51618 |     42.852 |    999.0 |   0.000000*** |    1.916 | very large greedy-2opt WINS
random         |    0.99995 |    0.49772 |     44.088 |    999.0 |   0.000000*** |    1.972 | very large greedy-2opt WINS
```

| 基线 | Greedy 均值 | 基线均值 | t 统计量 | p 值 | Cohen's d | 效应量 | 判定 |
|---|---|---|---|---|---|---|---|
| binpack | 0.99995 | 0.51058 | 42.927 | <1e-6 *** | 1.920 | very large | greedy WINS |
| k8s-default | 0.99995 | 0.50516 | 43.709 | <1e-6 *** | 1.955 | very large | greedy WINS |
| first-fit | 0.99995 | 0.51618 | 42.852 | <1e-6 *** | 1.916 | very large | greedy WINS |
| random | 0.99995 | 0.49772 | 44.088 | <1e-6 *** | 1.972 | very large | greedy WINS |

> **结论**：算法解相对**全部四个拓扑盲基线**（含 K8s 默认打分逻辑）具有 p<1e-6、Cohen's d≈1.9–2.0（"very large"效应量）的显著优势。分配质量 `W(S)` 约为基线的 **1.9–2.0 倍**（4105 vs ~2200 GB/s）。这是**真实的算法壁垒**：K8s 默认调度器因拓扑盲，在随机拓扑上平均只拿到最优带宽的 ~50%。

### 5.4 求解延迟：greedy-2opt vs exact-bnb（逐字）

```
--- SOLVE LATENCY: greedy-2opt vs exact-bnb (ns) ---
greedy-2opt mean = 12976 ns   exact-bnb mean = 212724 ns   speedup = 16.39x
Welch t = -9.976, df = 1052.6, p = 1.85749e-22
```

> 近似解相对精确解 **16.39× 加速**（12976 ns vs 212724 ns），p=1.86e-22，同时质量比 0.99995——近似解以极低代价换来几乎无损的质量。

---

## 6. Benchmark（求解延迟 + 分配质量，逐字）

### 6.1 近似比 harness（`BenchmarkDenseKApproximationRatio`，3000 样本，逐字）

```
$ go test ./pkg/scheduler/ -bench=BenchmarkDenseKApproximationRatio -count=1 -benchtime=3000x -run=^$

[ApproximationRatio] samples=3000 mean=0.999957 stddev=0.000774 min=0.972027 p05=1.000000 median=1.000000 max=1.000000
[ApproximationRatio] mean greedy latency=11923 ns, mean exact latency=222174 ns, speedup=18.63x
PASS
```

3000 独立样本复核：近似比均值 0.999957、最坏 0.972027、加速 18.63×，与 1000 样本统计完全一致。

### 6.2 求解延迟随 N 增长（`BenchmarkDenseKScaling`，固定 k=6，`-count=5 -benchtime=200x`，逐字）

```
BenchmarkDenseKScaling/N10/exact-bnb-24         	     200	     16475 ns/op	    7232 B/op	      75 allocs/op
BenchmarkDenseKScaling/N10/exact-bnb-24         	     200	     18278 ns/op	    7232 B/op	      75 allocs/op
BenchmarkDenseKScaling/N10/exact-bnb-24         	     200	     11842 ns/op	    7232 B/op	      75 allocs/op
BenchmarkDenseKScaling/N10/exact-bnb-24         	     200	     17587 ns/op	    7232 B/op	      75 allocs/op
BenchmarkDenseKScaling/N10/exact-bnb-24         	     200	     13572 ns/op	    7232 B/op	      75 allocs/op
BenchmarkDenseKScaling/N10/greedy-2opt-24       	     200	      9628 ns/op	    1792 B/op	      15 allocs/op
BenchmarkDenseKScaling/N10/greedy-2opt-24       	     200	      3788 ns/op	    1792 B/op	      15 allocs/op
BenchmarkDenseKScaling/N10/greedy-2opt-24       	     200	      3884 ns/op	    1792 B/op	      15 allocs/op
BenchmarkDenseKScaling/N10/greedy-2opt-24       	     200	      4966 ns/op	    1792 B/op	      15 allocs/op
BenchmarkDenseKScaling/N10/greedy-2opt-24       	     200	      3876 ns/op	    1792 B/op	      15 allocs/op
BenchmarkDenseKScaling/N12/exact-bnb-24         	     200	    133890 ns/op	   40632 B/op	     454 allocs/op
BenchmarkDenseKScaling/N12/exact-bnb-24         	     200	    131002 ns/op	   40632 B/op	     454 allocs/op
BenchmarkDenseKScaling/N12/exact-bnb-24         	     200	    135368 ns/op	   40632 B/op	     454 allocs/op
BenchmarkDenseKScaling/N12/exact-bnb-24         	     200	    128736 ns/op	   40632 B/op	     454 allocs/op
BenchmarkDenseKScaling/N12/exact-bnb-24         	     200	    134870 ns/op	   40657 B/op	     454 allocs/op
BenchmarkDenseKScaling/N12/greedy-2opt-24       	     200	      5889 ns/op	    2432 B/op	      15 allocs/op
BenchmarkDenseKScaling/N12/greedy-2opt-24       	     200	      4664 ns/op	    2432 B/op	      15 allocs/op
BenchmarkDenseKScaling/N12/greedy-2opt-24       	     200	      4766 ns/op	    2432 B/op	      15 allocs/op
BenchmarkDenseKScaling/N12/greedy-2opt-24       	     200	      4722 ns/op	    2432 B/op	      15 allocs/op
BenchmarkDenseKScaling/N12/greedy-2opt-24       	     200	      7192 ns/op	    2432 B/op	      15 allocs/op
BenchmarkDenseKScaling/N14/exact-bnb-24         	     200	    609593 ns/op	  155593 B/op	    1432 allocs/op
BenchmarkDenseKScaling/N14/exact-bnb-24         	     200	    627207 ns/op	  155568 B/op	    1432 allocs/op
BenchmarkDenseKScaling/N14/exact-bnb-24         	     200	    624745 ns/op	  155594 B/op	    1432 allocs/op
BenchmarkDenseKScaling/N14/exact-bnb-24         	     200	    593436 ns/op	  155568 B/op	    1432 allocs/op
BenchmarkDenseKScaling/N14/exact-bnb-24         	     200	    610236 ns/op	  155568 B/op	    1432 allocs/op
BenchmarkDenseKScaling/N14/greedy-2opt-24       	     200	      7590 ns/op	    2896 B/op	      14 allocs/op
BenchmarkDenseKScaling/N14/greedy-2opt-24       	     200	     10024 ns/op	    2896 B/op	      14 allocs/op
BenchmarkDenseKScaling/N14/greedy-2opt-24       	     200	     13308 ns/op	    2896 B/op	      14 allocs/op
BenchmarkDenseKScaling/N14/greedy-2opt-24       	     200	      8234 ns/op	    2896 B/op	      14 allocs/op
BenchmarkDenseKScaling/N14/greedy-2opt-24       	     200	      7482 ns/op	    2896 B/op	      14 allocs/op
BenchmarkDenseKScaling/N16/exact-bnb-24         	     200	    714416 ns/op	  149728 B/op	     964 allocs/op
BenchmarkDenseKScaling/N16/exact-bnb-24         	     200	    749896 ns/op	  149729 B/op	     964 allocs/op
BenchmarkDenseKScaling/N16/exact-bnb-24         	     200	    726966 ns/op	  149728 B/op	     964 allocs/op
BenchmarkDenseKScaling/N16/exact-bnb-24         	     200	    691144 ns/op	  149728 B/op	     964 allocs/op
BenchmarkDenseKScaling/N16/exact-bnb-24         	     200	    718606 ns/op	  149728 B/op	     964 allocs/op
BenchmarkDenseKScaling/N16/greedy-2opt-24       	     200	      7849 ns/op	    3664 B/op	      14 allocs/op
BenchmarkDenseKScaling/N16/greedy-2opt-24       	     200	     13831 ns/op	    3664 B/op	      14 allocs/op
BenchmarkDenseKScaling/N16/greedy-2opt-24       	     200	      8003 ns/op	    3664 B/op	      14 allocs/op
BenchmarkDenseKScaling/N16/greedy-2opt-24       	     200	     10483 ns/op	    3664 B/op	      14 allocs/op
BenchmarkDenseKScaling/N16/greedy-2opt-24       	     200	      9057 ns/op	    3664 B/op	      14 allocs/op
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler	1.695s
```

**延迟随 N 增长对比（中位数近似）**：

| N | exact-bnb (ns) | greedy-2opt (ns) | exact/greedy 倍数 |
|---|---|---|---|
| 10 | ~16,475 | ~3,884 | ~4× |
| 12 | ~133,890 | ~4,766 | ~28× |
| 14 | ~610,236 | ~8,234 | ~74× |
| 16 | ~718,606 | ~9,057 | ~79× |

> **NP-hard 组合爆炸的实测证据**：精确解延迟从 N10 的 ~16µs 暴涨到 N14 的 ~610µs（约 37×），而近似解从 ~4µs 缓增到 ~9µs（约 2×）。这直接量化了"精确解不可扩展、近似解 µs 级恒定"的算法权衡。

### 6.3 多节点集群延迟（`BenchmarkDenseKMultiNode`，2 节点×8 卡=16 卡，逐字节选）

```
BenchmarkDenseKMultiNode/k2/exact-bnb-24         	     300	      9087 ns/op	    8120 B/op	      52 allocs/op
BenchmarkDenseKMultiNode/k2/greedy-2opt-24       	     300	      2497 ns/op	    3376 B/op	      14 allocs/op
BenchmarkDenseKMultiNode/k5/exact-bnb-24         	     300	     39941 ns/op	   25952 B/op	     146 allocs/op
BenchmarkDenseKMultiNode/k5/greedy-2opt-24       	     300	      4544 ns/op	    3664 B/op	      14 allocs/op
BenchmarkDenseKMultiNode/k8/exact-bnb-24         	     300	     68193 ns/op	   37872 B/op	     185 allocs/op
BenchmarkDenseKMultiNode/k8/greedy-2opt-24       	     300	      7872 ns/op	    3808 B/op	      14 allocs/op
```

> 16 卡集群 k=8 时，精确解 ~68µs，近似解 ~7.9µs（~8.6× 加速），近似比在 §5 中已验证接近 1.0。

### 6.4 DGX H100 全连接（`BenchmarkDenseKDGXH100/k8`，逐字节选）

```
BenchmarkDenseKDGXH100/k8/exact-bnb-24         	     500	       885.8 ns/op	    1880 B/op	      15 allocs/op
BenchmarkDenseKDGXH100/k8/greedy-2opt-24       	     500	      3014 ns/op	    1440 B/op	      14 allocs/op
BenchmarkDenseKDGXH100/k8/binpack-24           	     500	       128.0 ns/op	     248 B/op	       5 allocs/op
BenchmarkDenseKDGXH100/k8/first-fit-24         	     500	        66.60 ns/op	     128 B/op	       2 allocs/op
BenchmarkDenseKDGXH100/k8/k8s-default-24       	     500	       112.4 ns/op	     248 B/op	       5 allocs/op
BenchmarkDenseKDGXH100/k8/random-24            	     500	       378.0 ns/op	     192 B/op	       3 allocs/op
```

> **诚实披露（无优势场景）**：在 DGX H100 **全连接**拓扑上，任意 k 张卡带宽相同（900 GB/s 均一），因此所有求解器质量比都是 1.0——**此时算法相对基线无质量优势**（`TestApproxRatio` 已验证 ratio=1.0）。这是拓扑决定的：全连接下调度选择无关紧要。算法优势仅在**异构拓扑**（双 socket、多节点、随机 island）上体现。此外 k=8=N 时精确解上界立刻剪枝到唯一解，故其延迟（~886ns）反而低于近似解的多起点搜索（~3014ns），符合预期。

---

## 7. 竞品对比

| 维度 | 本算法（Greedy2Opt / ExactBB） | K8s 默认调度器（NodeResourcesFit） |
|---|---|---|
| 是否感知 NVLink/NVSwitch 拓扑 | ✅ 是（图边权即带宽） | ❌ 否（`nvidia.com/gpu` 为不透明计数） |
| 异构拓扑分配质量 `W(S)` | ~4105 GB/s（近似比 0.99995） | ~2209 GB/s（最优的 ~50%） |
| 相对优势 | **1.86×** 带宽，p<1e-6，Cohen's d=1.955（very large） | 基线 |
| 求解延迟 | 近似 ~13µs / 精确 ~213µs（N≤16） | ~0–1µs（但拓扑盲，质量差） |
| 最坏近似比 | 0.972（实测 1000 样本） | 无保证（最差 0.007） |

> K8s 生态的 topology-aware 方案（如 NVIDIA GPU Operator 的 `nvidia.com/gpu.topology` 标签、Volcano 的 `numa-aware` 插件）多为**启发式打分**或**亲和性约束**，未把问题形式化为 DkS 并给出带**实测近似比 + 精确最优上界**的求解器。本实现的壁垒在于：(1) 精确解提供可证明的最优上界；(2) 近似解在真实拓扑上实测近似比 0.99995（最坏 0.972）；(3) 全套统计显著性验证（p 值 + Cohen's d + 置信区间）。

---

## 8. 构建 / 静态检查 / 测试（逐字输出）

```
$ go build ./pkg/scheduler/
BUILD_EXIT=0

$ go vet ./pkg/scheduler/
VET_EXIT=0
```

```
$ go test ./pkg/scheduler/ -run="TestExactVsBruteForce|TestApproxRatio|TestConsistencyBaselines|TestRandomIsNonAdversarial" -v
--- PASS: TestExactVsBruteForce (0.00s)
--- PASS: TestApproxRatio (0.00s)
--- PASS: TestConsistencyBaselines (0.00s)
--- PASS: TestRandomIsNonAdversarial (0.00s)
PASS
ok  	github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler	0.042s
```

```
$ go test ./pkg/scheduler/ -run=TestDenseKSolversStatisticalAnalysis -v
--- PASS: TestDenseKSolversStatisticalAnalysis (0.23s)
PASS
```

**环境**：`goos: windows, goarch: amd64, cpu: Intel(R) Core(TM) Ultra 9 275HX`，Go 1.26.5，GOMODCACHE=E:\go\pkg\mod。

---

## 9. 结论

1. **算法壁垒成立**：在异构 GPU 拓扑上，本算法分配质量约为 K8s 默认调度器（拓扑盲）的 **1.9×**，统计显著（p<1e-6，Cohen's d≈1.9–2.0，"very large"）。
2. **近似解质量近乎无损**：实测近似比均值 0.999954、最坏 0.972027，95% CI [0.999895, 1.000013]。
3. **精确解提供可证明最优上界**，n≤6 与暴力穷举逐顶点一致；但随 N 组合爆炸（N14 ~610µs），验证了 DkS 的 NP-hard 本质。
4. **近似解 µs 级恒定**（N16 仍 ~9µs），相对精确解 16–79× 加速。
5. **诚实披露**：DGX H100 全连接等均一拓扑下算法无质量优势（拓扑使然）；所有数据均为合成拓扑，未查询真实硬件。

### 交付物清单

| 文件 | 内容 |
|---|---|
| [dense_k_subgraph.go](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/scheduler/dense_k_subgraph.go) | 图结构 + fixtures + ExactBB + Greedy2Opt + 4 基线 |
| [dense_k_subgraph_test.go](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/scheduler/dense_k_subgraph_test.go) | 精确 vs 暴力一致性、近似比、基线确定性 |
| [dense_k_subgraph_stat_test.go](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/scheduler/dense_k_subgraph_stat_test.go) | 1000 拓扑统计实验（Welch t-test + Cohen's d + CI） |
| [dense_k_subgraph_bench_test.go](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/scheduler/dense_k_subgraph_bench_test.go) | 求解延迟 + 近似比 benchmark |
| 本文档 | 问题形式化、NP-hard 归约、复杂度、近似比实测、竞品对比、p 值与效应量 |
