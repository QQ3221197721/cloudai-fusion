package disaster

import (
	"testing"
	"time"
)

// ============================================================================
// L16 Trust-On-Failover — 真实、可通过的单元测试
// ============================================================================
// 说明（诚实边界）：
//   - 这些测试断言的是代码"实际"的行为，不是宣传口径。
//   - 环境隔离部分：注意默认配置下 dev.AllowCrossEnv=true，因此 dev 可写向任意
//     环境（含 prod）。真正被硬阻断的是 prod 作为源、以及 prepro→非(prod/prepro)。
//   - 证据链使用自签名密钥，只能自证完整性/防篡改，无法向第三方证明身份。
// ============================================================================

// ---------------------------------------------------------------------------
// 环境隔离：断言真实的 IsAllowedToWriteTo 行为
// ---------------------------------------------------------------------------

func newTestEnforcer(t *testing.T, env EnvironmentID) *IsolationEnforcer {
	t.Helper()
	e, err := NewIsolationEnforcer(env, DefaultEnvironmentConfigs(), &NullAuditLogger{}, nil)
	if err != nil {
		t.Fatalf("NewIsolationEnforcer(%s) failed: %v", env, err)
	}
	return e
}

func TestL16Env_ProdCannotWriteOut(t *testing.T) {
	e := newTestEnforcer(t, EnvProd)
	if err := e.EnforceWriteAccess(EnvDev, "failover"); err == nil {
		t.Fatal("expected prod->dev write to be blocked, got nil")
	}
	if err := e.EnforceWriteAccess(EnvPrePro, "failover"); err == nil {
		t.Fatal("expected prod->prepro write to be blocked, got nil")
	}
}

func TestL16Env_PreproWriteRules(t *testing.T) {
	e := newTestEnforcer(t, EnvPrePro)
	// prepro -> prod 允许
	if err := e.EnforceWriteAccess(EnvProd, "sync"); err != nil {
		t.Fatalf("expected prepro->prod allowed, got %v", err)
	}
	// prepro -> dev 阻断
	if err := e.EnforceWriteAccess(EnvDev, "sync"); err == nil {
		t.Fatal("expected prepro->dev blocked, got nil")
	}
}

func TestL16Env_DevCrossEnvIsAllowedByDefault(t *testing.T) {
	// 诚实记录：默认 dev.AllowCrossEnv=true，dev 可写向 prod。
	// 这与"100% 阻止 dev->prod"的旧说法相反，此测试固化真实行为。
	e := newTestEnforcer(t, EnvDev)
	if err := e.EnforceWriteAccess(EnvProd, "write"); err != nil {
		t.Fatalf("with default config dev->prod is allowed (AllowCrossEnv=true), got %v", err)
	}
}

func TestL16Env_ViolationHandlerFires(t *testing.T) {
	var fired bool
	e, err := NewIsolationEnforcer(EnvProd, DefaultEnvironmentConfigs(), &NullAuditLogger{},
		func(v *EnvironmentViolation) { fired = true })
	if err != nil {
		t.Fatalf("enforcer init failed: %v", err)
	}
	if err := e.EnforceWriteAccess(EnvDev, "failover"); err == nil {
		t.Fatal("expected block")
	}
	if !fired {
		t.Fatal("expected onViolation handler to fire")
	}
}

// ---------------------------------------------------------------------------
// Split-Brain 启发式检测：断言各判据在给定输入下的输出
// ---------------------------------------------------------------------------

func TestL16SB_DualPrimary(t *testing.T) {
	d := &SplitBrainDetector{}
	v := d.runDetectionAlgorithms([]*NodeStatus{
		{ID: "a", IsPrimary: true},
		{ID: "b", IsPrimary: true},
	})
	if !containsString(v, "dual-primary") {
		t.Fatalf("expected dual-primary, got %v", v)
	}
}

func TestL16SB_RaftTermMismatch(t *testing.T) {
	d := &SplitBrainDetector{}
	v := d.runDetectionAlgorithms([]*NodeStatus{
		{ID: "a", RaftTerm: 100},
		{ID: "b", RaftTerm: 101},
	})
	if !containsString(v, "raft-term-mismatch") {
		t.Fatalf("expected raft-term-mismatch, got %v", v)
	}
}

func TestL16SB_NetworkPartition(t *testing.T) {
	d := &SplitBrainDetector{}
	v := d.runDetectionAlgorithms([]*NodeStatus{
		{ID: "a", NetworkLatency: 100 * time.Millisecond},
		{ID: "b", NetworkLatency: 900 * time.Millisecond},
	})
	if !containsString(v, "network-partition-suspected") {
		t.Fatalf("expected network-partition-suspected, got %v", v)
	}
}

func TestL16SB_HealthyNoViolation(t *testing.T) {
	d := &SplitBrainDetector{}
	v := d.runDetectionAlgorithms([]*NodeStatus{
		{ID: "a", IsPrimary: true, RaftTerm: 5, NetworkLatency: 20 * time.Millisecond},
		{ID: "b", IsPrimary: false, RaftTerm: 5, NetworkLatency: 30 * time.Millisecond},
	})
	if len(v) != 0 {
		t.Fatalf("expected no violations for healthy cluster, got %v", v)
	}
}

// ---------------------------------------------------------------------------
// 证据链：新增 + 验证 + 篡改检测（自签名，仅证完整性）
// ---------------------------------------------------------------------------

func TestL16Evidence_AddAndVerify(t *testing.T) {
	ec := NewEvidenceChain()
	if err := ec.AddEvidence("n1", []byte("payload-1")); err != nil {
		t.Fatalf("AddEvidence failed: %v", err)
	}
	if err := ec.AddEvidence("n2", []byte("payload-2")); err != nil {
		t.Fatalf("AddEvidence failed: %v", err)
	}
	if !ec.Verify() {
		t.Fatal("expected freshly-built chain to verify")
	}
	if len(ec.GetRootHash()) == 0 {
		t.Fatal("expected non-empty root hash")
	}
}

func TestL16Evidence_TamperDetected(t *testing.T) {
	ec := NewEvidenceChain()
	_ = ec.AddEvidence("n1", []byte("payload-1"))
	_ = ec.AddEvidence("n2", []byte("payload-2"))
	if !ec.Verify() {
		t.Fatal("precondition: chain should verify before tamper")
	}
	// 篡改首个节点的证据内容 → 签名校验应失败
	ec.nodes[0].EvidenceData = []byte("tampered")
	if ec.Verify() {
		t.Fatal("expected tamper to be detected (Verify should return false)")
	}
}

func TestL16Evidence_EmptyChainIsNotValid(t *testing.T) {
	ec := NewEvidenceChain()
	if ec.Verify() {
		t.Fatal("empty chain should not verify")
	}
}
