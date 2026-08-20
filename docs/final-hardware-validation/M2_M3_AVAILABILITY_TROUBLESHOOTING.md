# M2/M3 (A100 实例) 阿里云"找不到"排查与解决方案

> 文档创建日期：2026-08-20
> 适用场景：用户已开通 M5 SGX 实例（深圳，公网 IP 39.108.104.207），但在购买页找不到 M2（gn7e 单 A100 80G）和 M3（ebmgn7ex 裸金属 8xA100+eRDMA）

---

## 1. 各地域 gn7e / ebmgn7ex 可购情况表

> **重要说明**：阿里云 GPU 实例的精确可购地域/可用区信息是**动态变化的**，取决于实时库存。下表基于阿里云官方文档和公开信息整理，实际下单时以 ECS 控制台「实例可购买地域」工具为准。

| 地域 | gn7e (单/多 A100) | ebmgn7ex (裸金属 8xA100 eRDMA) | 备注 |
|------|-------------------|-------------------------------|------|
| **华北2（北京）** cn-beijing | **大概率可购** | **大概率可购** | A100 库存最稳定的地域之一，12 个可用区，GPU 集群部署密集 |
| **华北6（乌兰察布）** cn-wulanchabu | **大概率可购** | **大概率可购** | 阿里云智算中心重点地域，A100/eRDMA 训练集群集中部署 |
| **华东1（杭州）** cn-hangzhou | 可能可购 | 可能可购 | 阿里云总部所在地，GPU 资源较充足 |
| **华东2（上海）** cn-shanghai | 可能可购 | 可能可购 | 大型数据中心，12 个可用区 |
| **华南1（深圳）** cn-shenzhen | **可能不售/无库存** | **很可能不售** | 深圳偏重通用计算和网络业务，A100 高端 GPU 实例库存极少或不在此地域售卖 |
| 华南2（河源） cn-heyuan | 不确定 | 不确定 | 较新地域，可能有 GPU 资源 |
| 华北3（张家口） cn-zhangjiakou | 不确定 | 不确定 | 大数据定位，A100 未确认 |
| 西南1（成都） cn-chengdu | 大概率无 | 大概率无 | 非 GPU 重点地域 |

**数据来源**：
- 阿里云 EGS 地域和可用区文档：https://help.aliyun.com/zh/egs/regions-and-zones-1 （2026-08-20 访问）
- 阿里云 GPU 实例规格文档：https://help.aliyun.com/zh/egs/gpu-accelerated-compute-optimized-instance-families （2026-08-20 访问）
- ECS 实例可购买地域查询工具：https://ecs-buy.aliyun.com/instanceTypes （控制台实时查询）
- InfoQ 文章确认 ebmgn7ex "可部署于阿里云所有可用区"（设计目标）：https://xie.infoq.cn/article/a4b39328bb41cb323703eee91

**关键发现**：
- gn8is（L20 GPU，非 H100）和 gn8v/gn8v-tee（H100 96GB）文档明确标注"**仅支持海外等部分地域，如有需求，请联系阿里云销售人员**"，说明这些型号在国内公开购买受限
- gn7e 和 ebmgn7ex **没有**类似的地域限制标注，理论上在国内主要地域可购，但受配额和库存约束
- gn7s（A30，支持 MIG）标注"如需使用，请提交工单申请"

---

## 2. "找不到"三类原因判定流程

### 逐步排查流程图

```
购买页选择 GPU 实例 → 找不到 gn7e / ebmgn7ex
         │
         ├─ 检查 ①：是否选对了地域？
         │   └─ 深圳(cn-shenzhen) 可能不售 A100 实例
         │       → 切换到 北京/乌兰察布/上海/杭州 试试
         │
         ├─ 检查 ②：配额是否为 0？
         │   └─ 即使地域有售，新账号 GPU 配额默认为 0
         │       → 配额为 0 时，购买页不显示该规格
         │       → 需先申请配额，审批通过后才能在购买页看到并下单
         │
         └─ 检查 ③：该可用区是否上线此规格？
             └─ 同地域不同可用区的售卖规格不同
                 → 切换可用区再试
```

### 原因 (a)：配额为 0 — 最可能的原因

**判定方法**：
1. 登录阿里云控制台 → 搜索"配额中心" → 进入
2. 左侧导航栏 → 产品列表 → 通用配额
3. 选择产品：「云服务器 ECS 规格配额」
4. 选择目标地域（如 华北2 北京）
5. 搜索配额 ID：`q_ecs_gn7e_prepay_g`（包年包月）或 `q_ecs_gn7e_postpay_g`（按量付费）
6. 查看「配额」列的值
   - 如果配额 = 0 → **这就是找不到的原因**
   - 如果配额 > 0 但购买页仍无 → 排查原因 (b) 或 (c)

**机制说明**：阿里云 GPU 实例（尤其是 A100 级别）的配额是**按地域、按付费方式、按规格族**分别管理的。新账号或从未购买过 GPU 的账号，gn7e 系列的 GPU 卡数配额默认为 0。配额为 0 意味着该规格**不会出现在购买页的选项中**。

### 原因 (b)：所选地域不售该规格

**判定方法**：
1. 访问 ECS 实例可购买地域工具：控制台 → ECS → 概览页 → 右上角「实例可购买地域」
2. 或直接访问：https://ecs-buy.aliyun.com/instanceTypes
3. 搜索 `gn7e` 或 `ebmgn7ex`
4. 查看各地域的可购买状态
   - 如果目标地域显示"不可购买" → 该地域确实不售
   - 需要换地域

**深圳地域说明**：华南1（深圳）主要服务消费互联网和通用计算场景，A100 级别的高端 GPU 训练实例（gn7e、ebmgn7ex）很可能未在深圳部署或仅有极少库存。这是 M2/M3 在深圳找不到的最可能硬件原因。

### 原因 (c)：可用区级别不可用

**判定方法**：
1. 确认地域已切换到支持 gn7e 的地域（如北京）
2. 在购买页的「可用区」下拉框中逐个切换
3. 每切换一个可用区，观察实例规格列表是否出现 gn7e
4. ebmgn7ex 裸金属实例可能仅在特定可用区（通常是有 eRDMA 交换机的机房）可购

---

## 3. A100 配额申请分步指引

### 前置条件
- 阿里云实名认证账号
- 建议账号有一定消费记录（有利于配额审批）
- 确认目标地域（推荐：华北2 北京 或 华北6 乌兰察布）

### 操作步骤

#### 方式一：通过配额中心申请（推荐）

1. **登录阿里云控制台**
   - 访问 https://home.console.aliyun.com/

2. **进入配额中心**
   - 顶部搜索栏输入"配额中心" → 点击进入
   - 或直接访问：https://quotas.console.aliyun.com/

3. **定位到 ECS 规格配额**
   - 左侧导航栏 → 「产品列表」 → 「通用配额」
   - 在产品下拉框中选择「云服务器 ECS 规格配额」

4. **选择目标地域**
   - 页面顶部切换地域为「华北2（北京）」或「华北6（乌兰察布）」

5. **搜索 gn7e 配额**
   - 在搜索框中输入 `gn7e`
   - 找到以下配额项（根据付费方式选择）：
     - `q_ecs_gn7e_prepay_g`：包年包月的(ebm)gn7e/gn7ex系列GPU实例卡数上限
     - `q_ecs_gn7e_postpay_g`：按量付费的(ebm)gn7e/gn7ex系列GPU实例卡数上限
   - **注意**：gn7e 和 ebmgn7ex 共用同一个配额项！

6. **点击「申请」**
   - 在目标配额行的「操作」列，点击「申请」
   - 如果显示"不可调整"，需要改用工单方式（见方式二）

7. **填写申请信息**
   - 申请配额：
     - M2 验证（单 A100）：填 1（1 张 GPU 卡）
     - M3 验证（8xA100 裸金属 x2 台）：填 16（8 卡 x 2 台 = 16 张）
   - 申请理由（示例）：
     > "用于 AI 训练平台验证，需要 A100 实例进行 MIG 分片和 eRDMA 多机通信测试。已有同账号下深圳地域 SGX 实例在运行，本次新增 GPU 训练节点。"

8. **提交并等待**
   - 点击「确认调整」
   - 通常 1-3 个工作日内审批
   - 结果通过短信和邮箱通知
   - 可在「申请历史」中查看审批进度

#### 方式二：通过工单申请（配额中心无法操作时）

1. 登录控制台 → 右上角「工单」 → 「提交工单」
2. 选择产品：云服务器 ECS
3. 问题类型：配额提升
4. 描述中写明：
   - 目标地域
   - 目标规格（ecs.gn7e-c16g1.4xlarge / ecs.ebmgn7ex.32xlarge）
   - 需要的 GPU 卡数
   - 付费方式（按量/包年包月）
   - 使用用途

### 预计时间线
| 步骤 | 预计耗时 |
|------|---------|
| 配额申请提交 | 5 分钟 |
| 配额审批 | 1-3 个工作日（A100 级别可能需 2-5 天） |
| 审批通过后下单 | 即时（如有库存） |
| 实例交付（gn7e） | 分钟级 |
| 实例交付（ebmgn7ex 裸金属） | 可能需要 10-30 分钟 |

---

## 4. 备选方案对比表

### MIG 支持声明（诚实红线）

> **严正声明**：NVIDIA Multi-Instance GPU (MIG) 技术**仅支持以下 GPU**：
> - NVIDIA A100（40GB / 80GB）
> - NVIDIA A30（24GB）
> - NVIDIA H100（80GB / 96GB）
> - NVIDIA H200
> - NVIDIA B100/B200 系列
>
> **A10 不支持 MIG。** 阿里云 gn7i 实例（A10 GPU）**无法**用于验证 M2 的 MIG 分片功能。
> 
> 来源：NVIDIA 官方文档 https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/platform-support.html
> 以及第三方确认 https://www.spheron.network/blog/fractional-gpu-inference-vgpu-mps-right-sizing/

### 对比表

| 方案 | 实例规格 | GPU | MIG 支持 | eRDMA | 可购性 | 能验证什么 | 不能验证什么 |
|------|---------|-----|---------|-------|--------|-----------|------------|
| **首选：换地域购 gn7e** | ecs.gn7e-c16g1.4xlarge | A100 80G x1 | **支持** | 不支持 | 北京/乌兰察布（需配额） | M2 全部功能（MIG 7 切片） | — |
| **首选：换地域购 ebmgn7ex** | ecs.ebmgn7ex.32xlarge | A100 80G x8 | **支持** | **160Gbps** | 北京/乌兰察布（需配额） | M3 全部功能（MIG + eRDMA 跨机） | — |
| 备选A：gn7i (A10) | ecs.gn7i-c16g1.4xlarge | A10 24G x1 | **不支持** | 不支持 | 多地域易购 | GPU 基础推理、CUDA 环境验证 | **无法验证 MIG**、无法验证 eRDMA |
| 备选B：gn7s (A30) | ecs.gn7s-c16g1.4xlarge | A30 24G x1 | **支持** | 不支持 | 需提交工单申请 | MIG 功能验证（最多 4 实例） | eRDMA、显存容量受限(24G vs 80G) |
| 备选C：gn8v (H100) | ecs.gn8v.4xlarge | H100 96G x1 | **支持** | 支持 eRDMA | **仅海外/需联系销售** | MIG + 更高性能 | 国内难以直接购买 |
| 备选D：单机多卡+CRIU | gn7e-c16g1.32xlarge (8卡) | A100 80G x8 | **支持** | **不支持** | 北京/乌兰察布（需配额） | MIG、CRIU 迁移逻辑、多卡并行 | **无法验证真实跨节点 eRDMA** |
| 备选E：gn9gc (B系列) | ecs.gn9gc | Blackwell 72G | **支持** | 360Gbps eRDMA | 需确认地域 | 最新一代全功能 | 新品库存不确定，价格高 |

### 各备选方案详细说明

**备选A (gn7i / A10) — 不推荐用于 M2 验证**
- A10 是 Ampere 架构但**不支持 MIG**，只支持 vGPU 和 MPS
- 如果仅需验证 CUDA 环境、Docker GPU 调度等基础能力可用
- 绝不能声称"已验证 MIG 功能"

**备选B (gn7s / A30) — 部分验证 MIG**
- A30 支持 MIG，最多切分为 4 个实例（A100 可切 7 个）
- 显存仅 24GB（A100 为 80GB），大模型场景受限
- 需提交工单才能开通，流程类似配额申请

**备选D (单机8卡+CRIU) — M3 降级验证**
- 若实在无法获得两台 ebmgn7ex，可用一台 8 卡 gn7e 验证：
  - MIG 分片逻辑（正常验证）
  - CRIU 进程迁移逻辑（在同机不同 GPU 间模拟）
  - 多卡 NCCL 通信
- **无法验证的**：真实 eRDMA 160Gbps 跨节点 RDMA 通信
- **诚实结论**：这只能验证"迁移逻辑正确性"，不能验证"跨节点 eRDMA 性能"

---

## 5. 给用户的行动建议

### 立即执行（今天）

1. **第一步：确认购买地域**
   - 登录阿里云控制台，切换地域到「华北2（北京）」
   - 尝试创建 ECS → 选择 GPU 计算型 → 看是否出现 gn7e
   - 如果北京没有，切到「华北6（乌兰察布）」再试

2. **第二步：检查并申请配额**
   - 无论能否在购买页看到 gn7e，都去配额中心检查：
     - 配额中心 → ECS 规格配额 → 选地域 → 搜 `gn7e`
     - 查看 `q_ecs_gn7e_prepay_g` 和 `q_ecs_gn7e_postpay_g`
   - 如果配额 = 0，立即提交申请
   - **同时申请两个地域**（北京 + 乌兰察布），哪个先批就用哪个

3. **第三步：M2 和 M3 配额一起申请**
   - gn7e 和 ebmgn7ex **共用 `q_ecs_gn7e_*_g` 配额**
   - 申请 GPU 卡数：至少 17 张（M2 用 1 张 + M3 用 16 张 = 17）
   - 建议申请 20 张留余量

### 等待期间（1-5 个工作日）

4. **备选方案并行准备**
   - 如果急需验证 MIG 且等不了，先申请 gn7s（A30）工单
   - A30 也支持 MIG，可以提前跑通 MIG 分片逻辑
   - **注意**：gn7s 需要单独提交工单，不是配额申请

### 配额审批通过后

5. **下单 M2**
   - 规格：`ecs.gn7e-c16g1.4xlarge`（16vCPU / 125GiB / 1xA100 80G）
   - 地域：北京或乌兰察布（哪个先批就用哪个）
   - 付费：按量付费（灵活，测试完可释放）
   - 无需和深圳的 M5 SGX 同地域

6. **下单 M3（两台）**
   - 规格：`ecs.ebmgn7ex.32xlarge`（128vCPU / 1024GiB / 8xA100 80G / eRDMA 160Gbps）
   - 地域：**两台必须同地域同可用区**（eRDMA 同可用区延迟最低）
   - 付费：按量付费
   - 购买时勾选「eRDMA」选项

### 关键原则

| 原则 | 说明 |
|------|------|
| M2/M3 不必和 M5 同地域 | 各机器独立验证各自能力即可 |
| M3 两台必须同地域同可用区 | eRDMA 跨可用区延迟大幅上升，失去意义 |
| 配额是按地域的 | 申请北京配额不等于上海也有配额 |
| 按量付费更灵活 | 测试完释放不浪费钱 |

---

## 6. 数据来源汇总

| # | 来源 | URL | 访问日期 | 用途 |
|---|------|-----|---------|------|
| 1 | 阿里云 EGS 地域和可用区 | https://help.aliyun.com/zh/egs/regions-and-zones-1 | 2026-08-20 | 地域总表 |
| 2 | 阿里云 GPU 计算型实例规格族(gn/ebm/scc) | https://help.aliyun.com/zh/egs/gpu-accelerated-compute-optimized-instance-families | 2026-08-20 | gn7e/ebmgn7ex/gn8v 规格详情、地域限制 |
| 3 | 阿里云 GPU 云服务器(gn/vgn/sgn系列) | https://help.aliyun.com/zh/ecs/user-guide/gpu-accelerated-compute-optimized-and-vgpu-accelerated-instance-families-1 | 2026-08-20 | 规格表补充 |
| 4 | ECS 资源使用限制与配额 | https://help.aliyun.com/zh/ecs/user-guide/limitations | 2026-08-20 | 配额 ID 对应关系 |
| 5 | 管理 ECS 资源配额 | https://help.aliyun.com/zh/ecs/user-guide/quota-management | 2026-08-20 | 配额申请操作步骤 |
| 6 | InfoQ - ebmgn7ex 发布 | https://xie.infoq.cn/article/a4b39328bb41cb323703eee91 | 2026-08-20 | ebmgn7ex 可部署所有 AZ |
| 7 | NVIDIA GPU Operator 平台支持 | https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/platform-support.html | 2026-08-20 | MIG 支持 GPU 列表 |
| 8 | 配额中心控制台 | https://quotas.console.aliyun.com/ | — | 实操入口 |
| 9 | ECS 实例可购买地域工具 | https://ecs-buy.aliyun.com/instanceTypes | — | 实时查询规格售卖地域 |

---

## 附录：关键配额 ID 速查

| 配额 ID | 含义 | 覆盖规格 |
|---------|------|---------|
| `q_ecs_gn7e_prepay_g` | 包年包月 gn7e/ebmgn7ex/ebmgn7e/sccgn7ex GPU 卡数上限 | gn7e + ebmgn7ex + ebmgn7e + sccgn7ex |
| `q_ecs_gn7e_postpay_g` | 按量付费 gn7e/ebmgn7ex/ebmgn7e/sccgn7ex GPU 卡数上限 | 同上 |
| `q_ecs_gn7i_prepay_g` | 包年包月 gn7i/ebmgn7ix GPU 卡数上限 | gn7i + ebmgn7ix（A10） |
| `q_ecs_gn7i_postpay_g` | 按量付费 gn7i/ebmgn7ix GPU 卡数上限 | 同上 |

---

*文档结束*
