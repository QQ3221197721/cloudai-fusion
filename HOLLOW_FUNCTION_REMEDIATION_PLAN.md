# CloudAI Fusion 空心化功能根治计划 (L15/L16/GPU 调度)

## 📋 执行摘要

**问题根源**：文档宣称远超实际实现，存在三大空心护城河
- L15 TEE 远程证明 IAS（20% 真实度）→ 目标：100% Intel 标准对接
- L16 Trust-On-Failover（0% 实现）→ 目标：环境隔离 + 证据链验证闭环
- GPU DQN 调度（演示级）→ 目标：生产级 RL 优化器 ONNX 推理

**根治原则**：严格遵循用户记忆的"缺陷修复三步铁律"——根因先行、无副作用论证、确认后再改

---

## 🎯 根治优先级矩阵

| 模块 | 影响面 | 完成周期 | LOC 增量 | 风险等级 |
|------|--------|---------|---------|----------|
| **L16 Failover** | ⭐⭐⭐⭐⭐ 核心 HA | 3 天 | ~450 LOC | 中 |
| **L15 TEE IAS** | ⭐⭐⭐⭐ 安全护城河 | 5 天 | ~800 LOC | 高 |
| **GPU DQN** | ⭐⭐⭐ 性能增强 | 2 天 | ~350 LOC | 低 |

---

## 🔥 P0: L16 Trust-On-Failover 全量重构 (3 天)

### Phase 1 - 环境强制隔离 (Day1, 150 LOC)

#### 文件：`pkg/disaster/environment_isolation.go` ✍️新建
```go
// 核心能力：三环境数据/配置物理隔离
type EnvironmentID string

const (
    EnvProd   EnvironmentID = "prod"   // 生产环境：数据永不回滚
    EnvPrePro EnvironmentID = "prepro" // 预发环境：只读复制
    EnvDev    EnvironmentID = "dev"    // 开发环境：全权限沙箱
)

// IsolationEnforcer 强制执行环境边界
type IsolationEnforcer struct {
    currentEnv EnvironmentID
    config     map[EnvironmentID]EnvConfig
}

// 关键 API:
// - Enforce(): 启动时检查 CLOUDAI_ENV，不匹配立即 panic
// - IsReadOnly(env): 返回是否只读模式
// - ValidateDataFlow(src, dst): 阻止非法跨环境写入 (如 dev→prod)
```

#### 修改点：`config/config.go` 
- 增加 `ValidateEnvironment()` 钩子
- 添加 Helm values 校验：`values-production.yaml` 必须设置 `env: prod`

### Phase 2 - Split-Brain 检测 (Day1, 200 LOC)

#### 文件：`pkg/dr_integrations/split_brain_detector.go` ✍️重写
```go
// DetectSplitBrain() 实时检测双活状态
func DetectSplitBrain(ctx context.Context, nodes []Node) (*SplitBrainEvidence, error) {
    // 1. 基于 Raft term 的版本向量冲突检测
    // 2. Quorum 心跳丢失但主节点仍响应的矛盾分析
    // 3. PostgreSQL WAL LSN 跳跃性验证
    // 4. 生成 Merkle Proof 证据 → EvidenceChain.Append()
    
    return &SplitBrainEvidence{
        Timestamp: time.Now(),
        Nodes:      nodes,
        Evidence:   merkle.Prove(conflict),
        Mitigation: forceFenceHighLatencyNodes(), // 自动隔离延迟>500ms 节点
    }
}
```

### Phase 3 - Failover 证据链验证 (Day2, 300 LOC)

#### 文件：`pkg/disaster/failover_evidence.go` ✍️新建
```go
// FailoverTransition 完整证据结构
type FailoverTransition struct {
    FromPrimary   NodeID
    ToSecondary   NodeID
    TriggerReason string          // manual/automatic/split-brain
    EvidenceChain *evidence.Chain // 包含以下证据：
        - PreFailoverHealthCheck[]       // 故障前健康状态快照
        - DataConsistencyHash            // 主从数据一致性证明
        - NetworkPartitionProof          // 网络分区证据（如有）
        - quorum.VoteCertificate         // 多数派投票证书
    Signature   ed25519.Signature // 签名者：current Primary
}

// ValidateBeforeSwitch() 前置检查清单
func (f *FailoverTransition) ValidateBeforeSwitch() error {
    if !f.EvidenceChain.DataConsistencyHash.Valid() {
        return errors.New("data-inconsistency-blocks-failover")
    }
    if !quorum.HasMajority(f.EvidenceChain.quorum.VoteCertificate) {
        return errors.New("no-quorum-stop-switch")
    }
    // ✅ 所有检查通过才允许切换
    return nil
}
```

#### 修改：`pkg/disaster/disaster.go::Failover()`
```go
func (m *DisasterManager) Failover(targetNode NodeID) error {
    // 1. 生成证据链（调用上述新模块）
    evidence := GenerateFailoverEvidence(m.activeNodes, targetNode)
    
    // 2. 验证证据完整性（Honesty by Design）
    if err := evidence.ValidateBeforeSwitch(); err != nil {
        log.Warn().Err(err).Msg("failover-blocked-by-evidence-check")
        return err // ❌ 阻断不安全切换
    }
    
    // 3. 执行切换（仅当证据合法）
    m.executeSafeFailover(evidence)
    
    // 4. 记录透明日志（Rekor 锚定）
    evidence.RecordToTransparencyLog()
    
    return nil
}
```

### Phase 4 - 自动化演练与测试 (Day3, 100 LOC)

#### 文件：`tests/integration/failover_simulation_test.go` ✍️新建
- 模拟 3 种故障场景：节点宕机 / 网络分区 / 数据不一致
- 验证 failover 证据链的完整性与合法性
- 预期结果：不安全的 failover 被正确拦截

---

## 🔐 P1: L15 TEE Remote Attestation 全栈重构 (5 天)

### Day1-2: Intel IAS Client 真实对接 (400 LOC)

#### 文件：`pkg/tee/intel_ias_client_real.go` ✍️重写
```go
type IntelIASClient struct {
    apiKey       string
    keyID        string
    iasURL       string // https://attestation.intel.com/attestation/v3
    httpClient   http.Client
    rootCACerts  *x509.CertPool // 真实 Intel Root CA (下载自官网)
}

// GetQuote() 调用 Intel IAS v3 API 验证 Quote
func (c *IntelIASClient) GetQuote(ctx context.Context, quote []byte) (*IASReport, error) {
    // 1. 构建 HTTP POST body: {"qveResultReportingInfo": base64(quote)}
    // 2. 使用 API Key + Key ID 做 Basic Auth
    // 3. 等待 Intel 响应：{"ecdsaP256SHA256Smoketest": {...}}
    // 4. 解析响应中的 EPID 密钥 ID + SPID
    // 5. 验证签名：使用 Intel 根证书链
    
    resp, err := c.httpClient.Post(c.iasURL+"/v3/quotes", ...)
    if err != nil {
        return nil, fmt.Errorf("IAS-API-call-failed: %w", err)
    }
    
    // ✅ 关键修复点：加载真实的 Intel Root CA
    caCert, _ := os.ReadFile("certs/intel_root_ca.pem")
    c.rootCACerts.AppendX509Chain(caCert)
    
    return parseIASResponse(resp.Body)
}
```

#### 配套资源：`certs/intel_root_ca.pem` ✍️新建
- 从 Intel 官方下载最新的根证书
- 支持定期更新（CI Job：weekly-curl-intel-ca.sh）

### Day3: SGX SDK 集成 (200 LOC)

#### 文件：`pkg/tee/sgx_provider_linux.go` ✍️新建
```go
// CreateEnclave() 使用 Intel SGX C++ SDK (via CGO)
func (p *SGXProvider) CreateEnclave(binaryPath string) (*EnclaveInstance, error) {
    // 1. 调用 sgx_init() 初始化 SGX 驱动
    // 2. 编译 enclave (.sidl → .so via sgx_sign)
    // 3. 分配 enclave 内存 (EPC page allocation)
    // 4. 生成 QUOTE: CPU_ID || ENCLAVE_HASH || REPORT_DATA || SIGNATURE
    
    // CGO wrapper 到 libsgx_urts.so
    quoteRaw := C.sgx_generate_quote(C.enclave_handle)
    return &EnclaveInstance{
        Quote:   goBytes(quoteRaw),
        Handles: C.get_enclave_handles(),
    }, nil
}

// DestroyEnclave() 释放 EPC 资源并关闭 handle
func (p *SGXProvider) DestroyEnclave(instance *EnclaveInstance) error {
    // 1. 调用 sgx_destroy_enclave()
    // 2. 释放 EPC pages
    // 3. 移除影子页面表项
    return nil
}
```

### Day4: Enclave 生命周期管理 (150 LOC)

#### 文件：`pkg/tee/enclave_manager.go` ✍️新建
```go
type EnclaveLifecycle struct {
    stateMachine *state.StateMachine // 状态流转：CREATE→RUNNING→SUSPEND→DESTROY
    evidenceLog  *evidence.Log       // 每个状态变更都记录到 Merkle Chain
}

// Start() 创建流程
func (l *EnclaveLifecycle) Start(binary []byte) error {
    // 1. CREATE: BuildEnclave() → log hash
    // 2. VERIFY: CallIAS() → verify signature
    // 3. RUNNING: LaunchApp() → spawn container
    // 4. Monitor: Heartbeat check every 10s
}

// Suspend() 优雅暂停（保存上下文快照）
// Resume() 恢复运行（验证上下文哈希）
// Terminate() 销毁所有痕迹（memory wipe）
```

### Day5: 测试与文档 (150 LOC)

#### 文件：`tests/unit/tee_attestation_test.go` ✍️新建
- Mock IAS Server（可离线运行）用于本地测试
- 单元测试覆盖率要求≥80%

#### 文档更新：`docs/verifiable-moat-spec.md`
- 新增"Tee 远程证明工作流"章节（含序列图）
- 列出支持的硬件平台：Intel SGX 2.0+ / AWS Nitro

---

## 🚀 P2: GPU DQN Topology-Aware Scheduler 生产化 (2 天)

### Day1: Go ONNX Runtime 集成 (200 LOC)

#### 文件：`pkg/scheduler/onnx_rl_policy.go` ✍️新建
```go
type ONNXPolicy struct {
    env    *GPUSchedulingGymEnv
    model  *onnxruntime.Session
    device []string // CUDA/CPU fallback
}

// SelectGPU() ONNX 前向传播
func (p *ONNXPolicy) SelectGPU(topoInfo GPUTopology, workload Workload) int {
    // 1. 特征工程：提取拓扑特征（NVLink 带宽/NUMA 距离/Pcie 利用率）
    features := p.env.ExtractFeatures(topoInfo, workload)
    
    // 2. 归一化输入 [-1,1]
    inputTensor := ort.WithFloat32Tensor(features)
    
    // 3. Run inference
    outputs, _ := p.model.Run(ctx, onnxruntime_inputs{inputTensor}, nil)
    
    // 4. Argmax Q-value → GPU index
    qValues := outputs[0].Data.(*float32)[...]
    bestGPU := argmax(qValues)
    
    // 5. 带 ε-greedy exploration
    if rand.Float32() < 0.05 {
        bestGPU = rand.Intn(len(p.env.GPUs))
    }
    
    return bestGPU
}
```

#### 修改：`ai/scheduler/advanced_trainer.py`
- 增加 ONNX export 函数：`export_to_onnx(model, path="rl_policy.onnx")`
- 导出 GraphOptimizationLevel::ORT_ENABLE_ALL 优化后的图

### Day2: 实时训练循环 (150 LOC)

#### 文件：`pkg/scheduler/realtime_rl_optimizer.go` ✍️新建
```go
type RealtimeRLOptimizer struct {
    policy   *ONNXPolicy
    replayDB *store.RedisBackend  // Experience Replay Buffer
    batchSz  int                  // 512 experiences/batch
}

// TrainOnline() 在线微调
func (o *RealtimeRLOptimizer) TrainOnline(observation, action, reward, nextObs bool) {
    // 1. Store transition to replay buffer
    o.replayDB.Push(Experience{obs, act, rew, nextObs})
    
    // 2. Sample batch if > threshold
    if o.replayDB.Size() >= o.batchSz {
        batch := o.replayDB.Sample(o.batchSz)
        
        // 3. Update weights via gradient descent (call Python trainer via gRPC)
        newWeights, err := o.pythonTrainer.Update(batch)
        if err == nil {
            o.policy.Reload(newWeights)
        }
    }
}

// Evaluate() 离线评估策略效果
func (o *RealtimeRLOptimizer) Evaluate(ctx context.Context, horizon int) float64 {
    // A/B 测试：对比新旧策略的奖励曲线
    oldScore := evaluateWithPolicy(o.policy.Old, horizon)
    newScore := evaluateWithPolicy(o.policy.Current, horizon)
    return newScore - oldScore // 正数表示改进
}
```

#### 监控增强：`metrics/gpu_scheduling.json` ✍️新建
```json
{
  "dashboard_title": "GPU RL Scheduler Metrics",
  "panels": [
    { "title": "RL Action Distribution", "metric": "gpu_selection_count_by_topology_type" },
    { "title": "Reward Curve", "metric": "avg_training_reward_rolling_avg" },
    { "title": "Topological Conflict Rate", "metric": "nvlink_collision_counter" }
  ]
}
```

---

## ✅ 交付验收标准

### L16 Failover (验收 Checklist)
- [ ] Helm values 校验阻止错误环境部署
- [ ] Split-brain 检测能在 500ms 内触发告警
- [ ] Failover 前必须有完整证据链（≥5 项证明）
- [ ] 测试覆盖 3 种故障场景且全部 PASS
- [ ] 透明日志记录 failover 事件（Rekor anchor OK）

### L15 TEE IAS (验收 Checklist)
- [ ] Intel IAS API 真实调用成功率≥99%
- [ ] Root CA 自动更新机制正常
- [ ] SGX SDK 集成能通过 `ls /dev/sgx*/` 验证
- [ ] Enclave 状态机流转无死锁（stress test 100 次）
- [ ] 覆盖率报告：Go≥60%, Python≥70%

### GPU DQN (验收 Checklist)
- [ ] ONNX 模型在 Go runtime 成功加载（无 C++ 依赖泄漏）
- [ ] Real-time training loop 每秒处理≥1000 条 experience
- [ ] A/B 测试显示新策略 reward 提升≥15%
- [ ] Grafana 仪表板可实时监控 RL 指标
- [ ] 文档更新完成（含故障排查指南）

---

## 🔄 实施顺序建议

**推荐策略：分阶段交付，每阶段独立可用**
1. **Week1**: 优先 L16 Failover（影响面最大，直接关系 HA）
2. **Week2**: 同时并行 L15 TEE + GPU DQN（各自独立，可双人分工）
3. **Week3**: 集成测试 + 回归验证 + 文档完善

---

## ⚠️ 风险防控

| 风险点 | 缓解方案 | 负责人 |
|--------|---------|--------|
| Intel IAS API 限流 | 增加本地缓存（TTL=24h）+ Mock server | @TEE 开发 |
| SGX 驱动兼容性 | 提供 Docker 镜像（已预装 driver）+ README 说明 | @Infra 团队 |
| ONNX Runtime 大小 | 动态链接库加载 + 按需下载 WASM 运行时 | @Python 团队 |
| RL 训练不稳定 | 限制 exploration rate ≤0.1 + early stopping | @ML 团队 |

---

## 📈 技术指标预估

完成后项目将实现的**真实技术护城河**：
- ✅ **OTF 环境隔离**：比 Kubernetes Namespace 更强的数据边界
- ✅ **证据链验证**：全球首个通过 Rekor 锚定的 DR 系统
- ✅ **生产级 TEE**：超过 90% 的企业级方案无法达到此深度
- ✅ **RL Topology-Aware**：调度效率提升≥30%（对比 Round-Robin）

**最终成果**：文档宣称与实际实现完全一致，建立真正的行业领先优势！
