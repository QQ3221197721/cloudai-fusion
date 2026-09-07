package tee

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"
)

// 便捷：生成 n 字节随机
func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return b
}

// 便捷：在仿真模式下产出一份绑定报告（无 SGX 的机器上也可运行）
func newSimBoundReport(t *testing.T, nonce, payload []byte) (*BoundAttestationReport, *BoundAttestor) {
	t.Helper()
	setupFakeDevRoot(t) // 保证探测不到 SGX，走 simulation
	ba, err := NewBoundAttestor(WithSimulation())
	if err != nil {
		t.Fatalf("NewBoundAttestor: %v", err)
	}
	rep, _ := ba.AttestBound(context.Background(), BoundAttestationRequest{
		EnclaveID:   "enc-bound",
		Nonce:       nonce,
		PayloadHash: payload,
	})
	if rep == nil {
		t.Fatal("报告不应为 nil")
	}
	return rep, ba
}

func TestBound_NonceRequired(t *testing.T) {
	setupFakeDevRoot(t)
	ba, _ := NewBoundAttestor(WithSimulation())
	_, err := ba.AttestBound(context.Background(), BoundAttestationRequest{EnclaveID: "e", Nonce: nil})
	if err == nil {
		t.Error("缺少 nonce 应报错")
	}
}

func TestBound_SimulationVerifiesButNotTrusted(t *testing.T) {
	nonce := randBytes(t, 16)
	payloadHash := sha256.Sum256([]byte("model-v1-weights"))
	rep, _ := newSimBoundReport(t, nonce, payloadHash[:])

	if rep.Report.Capability.Available {
		t.Skip("本机真实存在 SGX，跳过 simulation 断言")
	}
	// 基础绑定验证（不要求硬件可信）应通过
	err := VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce:       nonce,
		ExpectedPayloadHash: payloadHash[:],
	}, time.Now())
	if err != nil {
		t.Fatalf("仿真报告的基础绑定验证应通过，得到 %v", err)
	}
	// 要求硬件可信时，仿真报告必须被拒绝（诚实语义）
	err = VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce:  nonce,
		RequireTrusted: true,
	}, time.Now())
	if err == nil {
		t.Error("RequireTrusted 下仿真报告必须被拒绝")
	}
}

func TestBound_WrongNonceRejected(t *testing.T) {
	nonce := randBytes(t, 16)
	rep, _ := newSimBoundReport(t, nonce, nil)
	if rep.Report.Capability.Available {
		t.Skip("本机存在 SGX，跳过")
	}
	// 用不同的 nonce 验证 → 抗重放应触发
	err := VerifyBoundReport(rep, BoundVerificationPolicy{ExpectedNonce: randBytes(t, 16)}, time.Now())
	if err == nil {
		t.Error("nonce 不匹配必须被拒绝（抗重放）")
	}
}

func TestBound_PayloadMismatchRejected(t *testing.T) {
	nonce := randBytes(t, 16)
	good := sha256.Sum256([]byte("payload-A"))
	rep, _ := newSimBoundReport(t, nonce, good[:])
	if rep.Report.Capability.Available {
		t.Skip("本机存在 SGX，跳过")
	}
	bad := sha256.Sum256([]byte("payload-B"))
	err := VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce:       nonce,
		ExpectedPayloadHash: bad[:],
	}, time.Now())
	if err == nil {
		t.Error("payload 哈希不匹配必须被拒绝")
	}
}

func TestBound_TamperDetected(t *testing.T) {
	nonce := randBytes(t, 16)
	rep, _ := newSimBoundReport(t, nonce, nil)
	if rep.Report.Capability.Available {
		t.Skip("本机存在 SGX，跳过")
	}
	// 篡改底层报告字段（把 Trusted 改成 true 试图伪装）
	rep.Report.Trusted = true
	err := VerifyBoundReport(rep, BoundVerificationPolicy{ExpectedNonce: nonce}, time.Now())
	if err == nil {
		t.Error("篡改字段后 binding 应不匹配、验证必须失败")
	}
}

func TestBound_SignatureTamperDetected(t *testing.T) {
	nonce := randBytes(t, 16)
	rep, _ := newSimBoundReport(t, nonce, nil)
	if rep.Report.Capability.Available {
		t.Skip("本机存在 SGX，跳过")
	}
	// 篡改签名
	if len(rep.Signature) > 0 {
		rep.Signature[0] ^= 0xFF
	}
	err := VerifyBoundReport(rep, BoundVerificationPolicy{ExpectedNonce: nonce}, time.Now())
	if err == nil {
		t.Error("签名被篡改后验证必须失败")
	}
}

func TestBound_SignerAllowlist(t *testing.T) {
	nonce := randBytes(t, 16)
	rep, ba := newSimBoundReport(t, nonce, nil)
	if rep.Report.Capability.Available {
		t.Skip("本机存在 SGX，跳过")
	}
	// 正确签名者在白名单 → 通过
	if err := VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce:     nonce,
		AllowedSignerKeys: []ed25519.PublicKey{ba.SignerPublicKey()},
	}, time.Now()); err != nil {
		t.Fatalf("白名单内签名者应通过，得到 %v", err)
	}
	// 陌生签名者不在白名单 → 拒绝
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce:     nonce,
		AllowedSignerKeys: []ed25519.PublicKey{otherPub},
	}, time.Now()); err == nil {
		t.Error("不在白名单的签名者必须被拒绝")
	}
}

func TestBound_FreshnessExpiry(t *testing.T) {
	nonce := randBytes(t, 16)
	rep, _ := newSimBoundReport(t, nonce, nil)
	if rep.Report.Capability.Available {
		t.Skip("本机存在 SGX，跳过")
	}
	// 用"未来 1 小时后"的 now 校验 + MaxAge=1 分钟 → 应过期
	future := rep.Report.VerifiedAt.Add(1 * time.Hour)
	err := VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce: nonce,
		MaxAge:        1 * time.Minute,
	}, future)
	if err == nil {
		t.Error("超过 MaxAge 的报告必须被判定过期")
	}
	// 在有效期内 → 通过
	if err := VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce: nonce,
		MaxAge:        10 * time.Minute,
	}, rep.Report.VerifiedAt.Add(1*time.Second)); err != nil {
		t.Errorf("有效期内应通过，得到 %v", err)
	}
}

func TestBound_HardwareTrustedPassesRequireTrusted(t *testing.T) {
	// 用注入的硬件后端构造"真实可信"路径，验证 RequireTrusted 能通过
	inner := &Attestor{
		cap:     SGXCapability{Available: true, Driver: DriverDCAPKernel, DCAPReady: true, OS: "linux"},
		backend: &fakeHWBackend{measurement: "MRENCLAVE-xyz"},
	}
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	ba := &BoundAttestor{inner: inner, signKey: priv, signPub: pub}

	nonce := randBytes(t, 16)
	rep, err := ba.AttestBound(context.Background(), BoundAttestationRequest{EnclaveID: "e-hw", Nonce: nonce})
	if err != nil {
		t.Fatalf("硬件可信路径不应报错: %v", err)
	}
	if !rep.Report.Trusted || rep.Report.Mode != ModeHardware {
		t.Fatalf("期望硬件可信报告，得到 mode=%s trusted=%v", rep.Report.Mode, rep.Report.Trusted)
	}
	if err := VerifyBoundReport(rep, BoundVerificationPolicy{
		ExpectedNonce:  nonce,
		RequireTrusted: true,
	}, time.Now()); err != nil {
		t.Errorf("硬件可信报告在 RequireTrusted 下应通过，得到 %v", err)
	}
}
