# Sherman-Morrison 增量更新的 O(d²) 复杂度证明

## 1. 背景：为何需要增量更新

在流式马氏距离检测中，每来一个新点 $x_k$，协方差矩阵会发生 rank-1 更新：

$$
C_k = C_{k-1} + w w^T, \quad w = \sqrt{\frac{k-1}{k}}(x_k - \bar{x}_{k-1})
$$

马氏距离评分需要 $C_k^{-1}$（或其 Cholesky 因子）。**朴素方案**：每步重新求逆/分解，成本 $O(d^3)$。**增量方案**：利用 Sherman-Morrison 公式或 Cholesky rank-1 update，成本降至 $O(d^2)$。

> **实现说明（诚实标注）**：本项目 [linalg.go](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/anomaly/linalg.go) 的 `CholeskyRank1Update` 采用 **Cholesky 秩-1 更新（Gill-Golub-Murray-Saunders 算法）** 而非直接应用 Sherman-Morrison 逆更新。二者在数学上等价（都实现 $O(d^2)$ 增量），但 Cholesky 更新数值稳定性更优（保持正定性、避免逆矩阵的病态放大）。本文先给出 Sherman-Morrison 的经典证明作为复杂度下界论证，再说明 Cholesky 更新的等价性与数值优势。

---

## 2. Sherman-Morrison 公式：证明

### 2.1 定理陈述

**定理 2.1 (Sherman-Morrison, 1950)**  
设 $A \in \mathbb{R}^{d \times d}$ 可逆，$u, v \in \mathbb{R}^d$ 为列向量。若 $1 + v^T A^{-1} u \neq 0$，则 $A + uv^T$ 可逆，且：

$$
\boxed{(A + uv^T)^{-1} = A^{-1} - \frac{A^{-1} u v^T A^{-1}}{1 + v^T A^{-1} u}}
$$

### 2.2 证明（直接验证）

**思路**: 记 $B = A^{-1} - \dfrac{A^{-1} u v^T A^{-1}}{1 + v^T A^{-1} u}$，验证 $(A + uv^T)B = I$。

记标量 $\beta = 1 + v^T A^{-1} u$（假设 $\beta \neq 0$）。展开乘积：

$$
(A + uv^T)B = (A + uv^T)\left(A^{-1} - \frac{A^{-1}uv^T A^{-1}}{\beta}\right)
$$

分为四项：

$$
= \underbrace{A A^{-1}}_{\text{(I)}} - \underbrace{\frac{A A^{-1} u v^T A^{-1}}{\beta}}_{\text{(II)}} + \underbrace{u v^T A^{-1}}_{\text{(III)}} - \underbrace{\frac{u v^T A^{-1} u v^T A^{-1}}{\beta}}_{\text{(IV)}}
$$

逐项化简：

- **(I)** $= I$
- **(II)** $= \dfrac{u v^T A^{-1}}{\beta}$ （因 $AA^{-1}=I$）
- **(III)** $= u v^T A^{-1}$
- **(IV)** $= \dfrac{u (v^T A^{-1} u) v^T A^{-1}}{\beta}$。注意 $v^T A^{-1} u$ 是**标量**，记作 $\gamma$，故 (IV) $= \dfrac{\gamma \cdot u v^T A^{-1}}{\beta}$

合并 (II)(III)(IV)：

$$
(A+uv^T)B = I - \frac{u v^T A^{-1}}{\beta} + u v^T A^{-1} - \frac{\gamma \cdot u v^T A^{-1}}{\beta}
$$

提取公因子 $u v^T A^{-1}$：

$$
= I + u v^T A^{-1}\left(-\frac{1}{\beta} + 1 - \frac{\gamma}{\beta}\right) = I + u v^T A^{-1} \cdot \frac{-1 + \beta - \gamma}{\beta}
$$

代入 $\beta = 1 + \gamma$（因 $\gamma = v^T A^{-1} u$）：

$$
-1 + \beta - \gamma = -1 + (1+\gamma) - \gamma = 0
$$

因此 $(A+uv^T)B = I$，故 $B = (A+uv^T)^{-1}$。证毕。∎

### 2.3 对称秩-1 情形（协方差更新）

在我方场景中 $u = v = w$（对称秩-1 更新 $C + ww^T$），公式退化为：

$$
(C + ww^T)^{-1} = C^{-1} - \frac{C^{-1} w w^T C^{-1}}{1 + w^T C^{-1} w}
$$

---

## 3. 复杂度分析：O(d²) vs O(d³)

### 3.1 增量更新的 O(d²) 论证

**引理 3.1**  
给定 $A^{-1}$（已知，$d\times d$），计算 $(A+uv^T)^{-1}$ 的成本为 $O(d^2)$。

**证明（逐步计数浮点运算）**:

| 步骤 | 运算 | 复杂度 |
|------|------|--------|
| 1. 计算 $p = A^{-1} u$ | 矩阵-向量乘 | $O(d^2)$ |
| 2. 计算 $q^T = v^T A^{-1}$ | 向量-矩阵乘 | $O(d^2)$ |
| 3. 计算标量 $\beta = 1 + v^T p$ | 内积 | $O(d)$ |
| 4. 计算外积 $p q^T$ | rank-1 外积 | $O(d^2)$ |
| 5. 更新 $A^{-1} - (pq^T)/\beta$ | 矩阵减法 | $O(d^2)$ |

**总计**: $O(d^2)$。证毕。∎

### 3.2 朴素重算的 O(d³) 下界

**引理 3.2**  
直接对 $A + uv^T$ 求逆（无历史信息），成本为 $O(d^3)$。

**证明**: 通用矩阵求逆（LU 分解 / Gaussian 消元）需 $\frac{2}{3}d^3 + O(d^2)$ 次浮点乘加。Cholesky 分解需 $\frac{1}{3}d^3$。二者都是 $\Theta(d^3)$。∎

### 3.3 加速比

$$
\text{Speedup} = \frac{O(d^3)}{O(d^2)} = \Theta(d)
$$

**推论 3.3**: 维度越高，增量更新优势越显著。$d=100$ 时理论加速 **100×**；$d=50$ 时 **50×**。

---

## 4. Cholesky 秩-1 更新的等价性与优势

### 4.1 为何实现选择 Cholesky 而非 Sherman-Morrison

Sherman-Morrison 直接维护 $C^{-1}$，但存在两个问题：

1. **数值不稳定**：$C$ 病态时 $C^{-1}$ 的误差被放大，尤其小样本高维情形
2. **正定性丢失**：浮点误差累积可能使 $C^{-1}$ 失去对称正定性

**Cholesky 秩-1 更新**（维护 $L$ 使 $LL^T = C$）克服这两点：

- 保持下三角结构 → 正定性天然维持
- Givens 旋转式更新数值稳定（backward stable）
- 马氏距离通过一次前向替换 $z = L^{-1}v$ 得到，同样 $O(d^2)$

### 4.2 Cholesky rank-1 update 的 O(d²) 证明

参考 [linalg.go 第105-118行](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/anomaly/linalg.go#L105-L118) 的实现：

```go
func CholeskyRank1Update(L [][]float64, w []float64) {
    d := len(w)
    for k := 0; k < d; k++ {          // 外层 d 次
        lkk := L[k][k]
        r := math.Hypot(lkk, w[k])
        c := r / lkk
        s := w[k] / lkk
        L[k][k] = r
        for i := k + 1; i < d; i++ {  // 内层 d-k 次
            L[i][k] = (L[i][k] + s*w[i]) / c
            w[i] = c*w[i] - s*L[i][k]
        }
    }
}
```

**复杂度**: 外层循环 $d$ 次，内层循环 $\sum_{k=0}^{d-1}(d-k) = \frac{d(d+1)}{2} = O(d^2)$。证毕。∎

### 4.3 马氏距离评分的 O(d²)

参考 [linalg.go 第150-153行](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/anomaly/linalg.go#L150-L153):

$$
D^2 = v^T (LL^T)^{-1} v = \|L^{-1}v\|^2
$$

通过前向替换求 $z = L^{-1}v$（$O(d^2)$），再取 $\|z\|^2$（$O(d)$）。总计 $O(d^2)$。

---

## 5. 摊还复杂度分析

### 5.1 定理：摊还每点 O(d²)

**定理 5.1 (Amortized Per-Point Cost)**  
设 refactorization window 为 $W \geq d$，则流式检测器的**摊还每点成本**为 $O(d^2)$。

**证明**:

- 每 $W$ 步执行一次完整 Cholesky refactorization：$O(d^3)$
- 其余 $W-1$ 步执行 rank-1 update：各 $O(d^2)$
- $W$ 步总成本：$O(d^3) + (W-1) \cdot O(d^2) = O(d^3 + Wd^2)$
- 摊还每点：$\dfrac{O(d^3 + Wd^2)}{W} = O\left(\dfrac{d^3}{W} + d^2\right)$

当 $W \geq d$ 时，$\dfrac{d^3}{W} \leq d^2$，故摊还成本 $= O(d^2)$。证毕。∎

> 代码中默认 `window = 200`（[detector.go 第78行](file:///d:/IdeaProjects/untitled/cloudai-fusion/pkg/anomaly/detector.go#L78)），远大于典型 $d \in [10, 100]$，确保摊还 $O(d^2)$。

### 5.2 实证验证（TestPerPointComplexityScaling）

实测（本环境运行结果）:

```
per-point ns: d=25 -> 1959.3, d=50 -> 5978.0, d=100 -> 17192.2
ratio d50/d25 = 3.05 (O(d²) predicts ~4), d100/d50 = 2.88
```

**解读**: 维度翻倍时耗时约增 3×（O(d²) 理论预测 4×，实测因常数项与缓存效应略低）。**关键结论：远低于 O(d³) 预测的 8×**，实证复杂度介于 O(d²) 与 O(d^1.6) 之间，排除了 cubic scaling。

---

## 6. 相对 sklearn 的复杂度壁垒

### 6.1 sklearn 无增量协方差更新

sklearn 的 `EmpiricalCovariance` / `LedoitWolf` 是 **batch estimator**，无 `partial_fit` 接口。流式场景必须：

1. **重新拟合**：每来一批新数据，`fit(X_all)` → $O(Nd^2)$ 协方差 + $O(d^3)$ 求逆
2. **滑动窗口**：维护窗口内所有点，每步重算 → $O(W d^2 + d^3)$ 每点

**对比**:

| 方案 | 每点成本 | 内存 |
|------|---------|------|
| **我方 Streaming Cholesky** | **O(d²)** | O(d²) |
| sklearn refit-per-point | O(Nd² + d³) | O(Nd) |
| sklearn sliding window | O(Wd² + d³) | O(Wd) |

### 6.2 IsolationForest 的流式困境

`IsolationForest.fit` 构建 $t$ 棵隔离树，成本 $O(t \psi \log \psi)$（$\psi$ 为子采样大小）。**无 online 更新**：新数据要么被 stale 模型评分（精度衰减），要么全量重训（$O(N)$ 级别成本）。

**结论**: sklearn 生态在流式增量场景下**结构性缺失**，我方 $O(d^2)$ 增量更新是**不可替代**的架构优势。

---

## 7. 总结定理

**定理 7.1 (Sherman-Morrison / Cholesky Streaming Moat)**  
流式马氏距离检测器通过 Cholesky 秩-1 更新（数学等价于 Sherman-Morrison 逆更新），实现：

1. **每点摊还成本 $O(d^2)$**（定理 5.1）
2. **相对朴素重算加速比 $\Theta(d)$**（推论 3.3）
3. **相对 sklearn batch 方案的结构性优势**：sklearn 无增量协方差/隔离树更新（§6）
4. **数值稳定性**：Cholesky 更新 backward stable，优于直接维护逆矩阵（§4.1）

证毕。∎

---

## 8. 参考文献

1. Sherman, J., & Morrison, W.J. (1950). "Adjustment of an Inverse Matrix Corresponding to a Change in One Element of a Given Matrix." *Annals of Mathematical Statistics*, 21(1), 124-127.

2. Golub, G.H., & Van Loan, C.F. (2013). *Matrix Computations* (4th ed.). Johns Hopkins University Press. [Cholesky rank-1 update, §6.5.4]

3. Gill, P.E., Golub, G.H., Murray, W., & Saunders, M.A. (1974). "Methods for Modifying Matrix Factorizations." *Mathematics of Computation*, 28(126), 505-535.

4. Higham, N.J. (2002). *Accuracy and Stability of Numerical Algorithms* (2nd ed.). SIAM. [Backward stability of Cholesky update]

5. Liu, F.T., Ting, K.M., & Zhou, Z.H. (2008). "Isolation Forest." *ICDM 2008*. [无 online 更新的结构限制]

---

**文件元信息**  
- 版本：v1.0  
- 创建时间：2026-08-24  
- 对应实验：Task #261, T3 M31 UEBA 统计壁垒攻坚  
- 实现对照：pkg/anomaly/linalg.go (CholeskyRank1Update, mahalanobisSqFromChol)
