package tee

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// ============================================================================
// Bound Attestation — nonce 新鲜 + payload 绑定 + 可独立验证的证明
// ============================================================================
// 解决的真实问题：裸 attestation quote 可被重放、且不绑定"被保护对象"。
// 本层把三件成熟基元组合成【默认正确的用法】：
//   1. Nonce 绑定      —— 调用方给挑战值，抵抗重放
//   2. Payload 绑定    —— 把负载/模型/配置的哈希绑进报告（对应 SGX REPORTDATA 思想）
//   3. 签名 + 独立验证 —— Ed25519 签名，任何第三方无需 attestor 即可离线核验
//
// 诚实边界（重要）：
//   - Ed25519 签名只保证"报告由该 attestor 实例产生且未被篡改"，
//     它【不】替代硬件信任；硬件级可信仍仅来自底层 AttestationReport.Trusted。
//   - 仿真/不可用模式下仍会产出绑定报告，但 Trusted=false；
//     验证策略 RequireTrusted=true 时会拒绝这类报告。
// ============================================================================

// BoundAttestationRequest 调用方希望绑定进证明的内容
type BoundAttestationRequest struct {
	EnclaveID   string // 目标 enclave
	Nonce       []byte // 调用方挑战值（必填，抗重放）
	PayloadHash []byte // 被背书的负载/模型/配置的哈希（可空）
}

// BoundAttestationReport 自描述、可独立验证的绑定证明报告
type BoundAttestationReport struct {
	Report       AttestationReport `json:"report"`          // 底层诚实报告（Mode/Trusted/Measurement...）
	Nonce        []byte            `json:"nonce"`           // 回填的挑战值
	PayloadHash  []byte            `json:"payload_hash,omitempty"` // 回填的负载哈希
	Binding      []byte            `json:"binding"`         // SHA256(规范化字段)，防篡改
	Signature    []byte            `json:"signature"`       // Ed25519(Binding)
	SignerPubKey []byte            `json:"signer_pubkey"`   // attestor 公钥（用于独立验证）
}

// BoundAttestor 在 Attestor 之上提供绑定式证明；持有一对签名密钥。
type BoundAttestor struct {
	inner   *Attestor
	signKey ed25519.PrivateKey
	signPub ed25519.PublicKey
}

// NewBoundAttestor 创建绑定式证明器（自动生成一对 Ed25519 签名密钥）。
// 任何平台可安全调用；无 SGX 时底层会走 unavailable/simulation。
func NewBoundAttestor(opts ...AttestorOption) (*BoundAttestor, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate-signing-key: %w", err)
	}
	return &BoundAttestor{
		inner:   NewAttestor(opts...),
		signKey: priv,
		signPub: pub,
	}, nil
}

// NewBoundAttestorWithKey 使用调用方提供的签名私钥创建（便于跨实例/持久身份）。
func NewBoundAttestorWithKey(priv ed25519.PrivateKey, opts ...AttestorOption) (*BoundAttestor, error) {
	if l := len(priv); l != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid-ed25519-private-key-size: %d", l)
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("cannot-derive-public-key")
	}
	return &BoundAttestor{inner: NewAttestor(opts...), signKey: priv, signPub: pub}, nil
}

// SignerPublicKey 返回本 attestor 的签名公钥（供验证方登记/校验）。
func (b *BoundAttestor) SignerPublicKey() ed25519.PublicKey {
	out := make(ed25519.PublicKey, len(b.signPub))
	copy(out, b.signPub)
	return out
}

// Capability 透传底层能力探测结果。
func (b *BoundAttestor) Capability() SGXCapability { return b.inner.Capability() }

// AttestBound 执行绑定式证明：先做底层证明，再绑定 nonce+payload 并签名。
// nonce 必填（抗重放的前提）；底层 unavailable 时同时返回其错误，但仍产出可读报告。
func (b *BoundAttestor) AttestBound(ctx context.Context, req BoundAttestationRequest) (*BoundAttestationReport, error) {
	if len(req.Nonce) == 0 {
		return nil, errors.New("nonce-required-for-bound-attestation")
	}

	rep, attErr := b.inner.Attest(ctx, req.EnclaveID)

	binding := computeBinding(rep, req.Nonce, req.PayloadHash)
	sig := ed25519.Sign(b.signKey, binding)

	out := &BoundAttestationReport{
		Report:       *rep,
		Nonce:        cloneBytes(req.Nonce),
		PayloadHash:  cloneBytes(req.PayloadHash),
		Binding:      binding,
		Signature:    sig,
		SignerPubKey: b.SignerPublicKey(),
	}
	// 即便底层不可用（attErr 非空），也返回已签名的诚实报告，让调用方自行按策略处理。
	return out, attErr
}

// ============================================================================
// 独立验证（无需 attestor，任何第三方可执行）
// ============================================================================

// BoundVerificationPolicy 验证策略
type BoundVerificationPolicy struct {
	ExpectedNonce       []byte          // 必须与报告 nonce 逐字节相等
	ExpectedPayloadHash []byte          // 非空时必须匹配（为空表示不校验 payload）
	RequireTrusted      bool            // 是否要求硬件级可信（Trusted==true）
	MaxAge              time.Duration   // 报告最大有效期（0 表示不校验时效）
	AllowedSignerKeys   []ed25519.PublicKey // 非空时，签名公钥必须在此白名单内
}

// VerifyBoundReport 独立验证绑定报告。全部检查通过返回 nil，否则返回首个失败原因。
func VerifyBoundReport(r *BoundAttestationReport, policy BoundVerificationPolicy, now time.Time) error {
	if r == nil {
		return errors.New("nil-report")
	}
	// 1) 重算 binding，防止字段被篡改
	expected := computeBinding(&r.Report, r.Nonce, r.PayloadHash)
	if subtle.ConstantTimeCompare(expected, r.Binding) != 1 {
		return errors.New("binding-mismatch: report fields tampered")
	}
	// 2) 验证签名
	if l := len(r.SignerPubKey); l != ed25519.PublicKeySize {
		return fmt.Errorf("invalid-signer-pubkey-size: %d", l)
	}
	if !ed25519.Verify(ed25519.PublicKey(r.SignerPubKey), r.Binding, r.Signature) {
		return errors.New("signature-invalid")
	}
	// 3) 签名者白名单（可选）
	if len(policy.AllowedSignerKeys) > 0 && !signerAllowed(r.SignerPubKey, policy.AllowedSignerKeys) {
		return errors.New("signer-not-in-allowlist")
	}
	// 4) nonce 必须匹配（抗重放）
	if subtle.ConstantTimeCompare(policy.ExpectedNonce, r.Nonce) != 1 {
		return errors.New("nonce-mismatch: possible replay or wrong challenge")
	}
	// 5) payload 绑定（可选）
	if len(policy.ExpectedPayloadHash) > 0 {
		if subtle.ConstantTimeCompare(policy.ExpectedPayloadHash, r.PayloadHash) != 1 {
			return errors.New("payload-hash-mismatch: attestation not bound to expected payload")
		}
	}
	// 6) 硬件可信要求（诚实语义：仿真/不可用一律拒绝）
	if policy.RequireTrusted && !r.Report.Trusted {
		return fmt.Errorf("not-hardware-trusted: mode=%s", r.Report.Mode)
	}
	// 7) 时效性
	if policy.MaxAge > 0 {
		age := now.Sub(r.Report.VerifiedAt)
		if age < 0 {
			return errors.New("report-timestamp-in-future")
		}
		if age > policy.MaxAge {
			return fmt.Errorf("report-expired: age=%s > maxAge=%s", age, policy.MaxAge)
		}
	}
	return nil
}

// ============================================================================
// 内部：规范化绑定计算
// ============================================================================

// computeBinding 以长度前缀的规范化方式对关键字段做 SHA256，避免字段拼接歧义。
func computeBinding(rep *AttestationReport, nonce, payloadHash []byte) []byte {
	h := sha256.New()
	writeField(h, []byte("cloudai-tee-bound-v1")) // 域分隔符，防跨用途碰撞
	writeField(h, []byte(rep.EnclaveID))
	writeField(h, []byte(rep.Mode))
	if rep.Trusted {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	writeField(h, []byte(rep.Measurement))
	writeField(h, nonce)
	writeField(h, payloadHash)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(rep.VerifiedAt.UnixNano()))
	h.Write(ts[:])
	return h.Sum(nil)
}

// writeField 写入 4 字节大端长度前缀 + 数据
func writeField(h interface{ Write([]byte) (int, error) }, data []byte) {
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(data)))
	_, _ = h.Write(l[:])
	_, _ = h.Write(data)
}

func signerAllowed(key []byte, allow []ed25519.PublicKey) bool {
	for _, k := range allow {
		if subtle.ConstantTimeCompare(key, k) == 1 {
			return true
		}
	}
	return false
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
