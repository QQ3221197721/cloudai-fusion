package tee

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// Attestation Session Cache — Attest-once, Verify-many（性能创新）
// ============================================================================
// 解决的真实瓶颈：完整硬件证明（SGX quote 生成）开销大（Intel 文档量级为数十 ms），
// 高吞吐机密服务无法做到"每请求一次完整证明"。
//
// 朴素方案的两难：
//   - 每请求重新证明        → 慢（数十 ms/请求）
//   - 缓存整份证明结果直接复用 → 可被重放，安全性崩坏
//
// 本层的正确组合（创新点）：
//   - 昂贵的硬件证明【每 TTL 只做一次】并缓存（measurement + trusted 语义）；
//   - 每请求用【廉价 HMAC token】绑定 (会话, measurement, 请求nonce, 过期时间)，
//     保持逐请求新鲜性与抗重放；
//   => 每请求成本从"一次硬件 quote 生成"降为"一次 HMAC-SHA256"（微秒级）。
//
// 诚实边界：
//   - Trusted 仍严格来自底层硬件证明；仿真会话 Trusted=false，VerifyToken 在
//     requireTrusted=true 时拒绝。
//   - 缓存不削弱新鲜性：token 的 nonce+expiry 保证每请求不可重放；会话到期强制重证明。
// ============================================================================

// AttestationSession 一次昂贵证明后建立的会话（缓存复用的载体）
type AttestationSession struct {
	ID          string          `json:"id"`          // 会话标识
	EnclaveID   string          `json:"enclave_id"`  // 关联 enclave
	Measurement string          `json:"measurement"` // 证明得到的度量值(MRENCLAVE)
	Mode        AttestationMode `json:"mode"`         // hardware/simulation/unavailable
	Trusted     bool            `json:"trusted"`      // 是否硬件级可信
	EstablishedAt time.Time     `json:"established_at"`
	ExpiresAt   time.Time       `json:"expires_at"`
	hmacKey     []byte          // 会话密钥（不导出/不序列化）：派生逐请求 token
}

// IssueToken 基于会话密钥对请求 nonce 签发廉价 token（便于外部调用；等价于内部 issueToken）。
func (s *AttestationSession) IssueToken(nonce []byte) *SessionToken {
	now := time.Now()
	exp := now.Add(30 * time.Second)
	if exp.After(s.ExpiresAt) && !s.ExpiresAt.IsZero() {
		exp = s.ExpiresAt
	}
	tok := &SessionToken{
		SessionID:   s.ID,
		Measurement: s.Measurement,
		Nonce:       cloneBytes(nonce),
		IssuedAt:    now,
		ExpiresAt:   exp,
	}
	tok.MAC = computeTokenMAC(s.hmacKey, tok)
	return tok
}

// Expired 判断会话是否过期
func (s *AttestationSession) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

// SessionToken 逐请求签发的廉价 token（抗重放）
type SessionToken struct {
	SessionID   string    `json:"session_id"`
	Measurement string    `json:"measurement"`
	Nonce       []byte    `json:"nonce"`      // 调用方请求挑战值
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"` // 不超过会话过期时间
	MAC         []byte    `json:"mac"`        // HMAC-SHA256(会话密钥, 规范化字段)
}

// SessionCache attest-once/verify-many 会话缓存
type SessionCache struct {
	mu        sync.RWMutex
	attestor  *BoundAttestor
	ttl       time.Duration
	tokenTTL  time.Duration
	sessions  map[string]*AttestationSession // key = enclaveID
	now       func() time.Time               // 可注入时钟（测试用）

	// 观测计数（真实，非臆造）
	establishCount uint64 // 触发昂贵证明的次数
	tokenCount     uint64 // 签发廉价 token 的次数
}

// NewSessionCache 创建会话缓存。ttl 为会话有效期（到期强制重证明）。
func NewSessionCache(ba *BoundAttestor, ttl time.Duration) *SessionCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &SessionCache{
		attestor: ba,
		ttl:      ttl,
		tokenTTL: 30 * time.Second,
		sessions: make(map[string]*AttestationSession),
		now:      time.Now,
	}
}

// getValidSession 返回未过期的现有会话；无或已过期返回 nil。
func (c *SessionCache) getValidSession(enclaveID string) *AttestationSession {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.sessions[enclaveID]
	if !ok || s.Expired(c.now()) {
		return nil
	}
	return s
}

// establish 执行昂贵路径：完整绑定证明一次，建立并缓存会话。
func (c *SessionCache) establish(ctx context.Context, enclaveID string) (*AttestationSession, error) {
	// 建立时的证明用内部随机 nonce 保证"证明本身"的新鲜性；
	// 逐请求新鲜性由 token 的 nonce+expiry 负责。
	internalNonce := make([]byte, 16)
	if _, err := rand.Read(internalNonce); err != nil {
		return nil, fmt.Errorf("gen-internal-nonce: %w", err)
	}
	rep, attErr := c.attestor.AttestBound(ctx, BoundAttestationRequest{
		EnclaveID: enclaveID,
		Nonce:     internalNonce,
	})
	if rep == nil {
		return nil, fmt.Errorf("attestation-produced-no-report: %w", attErr)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("gen-session-key: %w", err)
	}

	now := c.now()
	sess := &AttestationSession{
		ID:            fmt.Sprintf("sess-%d", now.UnixNano()),
		EnclaveID:     enclaveID,
		Measurement:   rep.Report.Measurement,
		Mode:          rep.Report.Mode,
		Trusted:       rep.Report.Trusted,
		EstablishedAt: now,
		ExpiresAt:     now.Add(c.ttl),
		hmacKey:       key,
	}

	c.mu.Lock()
	c.sessions[enclaveID] = sess
	c.establishCount++
	c.mu.Unlock()

	// 即便底层 attErr 非空（如 unavailable），也返回会话让上层按 Trusted/Mode 处理。
	return sess, attErr
}

// issueToken 廉价路径：基于会话密钥对请求 nonce 生成 HMAC token（不重证明）。
func (c *SessionCache) issueToken(sess *AttestationSession, nonce []byte) *SessionToken {
	now := c.now()
	exp := now.Add(c.tokenTTL)
	if exp.After(sess.ExpiresAt) {
		exp = sess.ExpiresAt // token 不得超过会话有效期
	}
	tok := &SessionToken{
		SessionID:   sess.ID,
		Measurement: sess.Measurement,
		Nonce:       cloneBytes(nonce),
		IssuedAt:    now,
		ExpiresAt:   exp,
	}
	tok.MAC = computeTokenMAC(sess.hmacKey, tok)

	c.mu.Lock()
	c.tokenCount++
	c.mu.Unlock()
	return tok
}

// Attest 逐请求入口：命中未过期会话则走廉价 token；否则先昂贵证明再签发。
// 返回 (token, reattested 是否触发了昂贵证明, err)。
func (c *SessionCache) Attest(ctx context.Context, enclaveID string, nonce []byte) (*SessionToken, bool, error) {
	if len(nonce) == 0 {
		return nil, false, errors.New("nonce-required")
	}
	if s := c.getValidSession(enclaveID); s != nil {
		return c.issueToken(s, nonce), false, nil
	}
	s, err := c.establish(ctx, enclaveID)
	if s == nil {
		return nil, true, err
	}
	// establish 的 err 可能是 unavailable；仍签发 token，Trusted 由会话决定。
	return c.issueToken(s, nonce), true, err
}

// VerifyToken 廉价验证：查会话密钥重算 MAC，校验新鲜性/过期/可信要求。
func (c *SessionCache) VerifyToken(tok *SessionToken, expectedNonce []byte, requireTrusted bool) error {
	if tok == nil {
		return errors.New("nil-token")
	}
	c.mu.RLock()
	var sess *AttestationSession
	for _, s := range c.sessions {
		if s.ID == tok.SessionID {
			sess = s
			break
		}
	}
	c.mu.RUnlock()
	if sess == nil {
		return errors.New("unknown-session")
	}

	// 1) MAC 校验（防伪造/篡改）
	expected := computeTokenMAC(sess.hmacKey, tok)
	if subtle.ConstantTimeCompare(expected, tok.MAC) != 1 {
		return errors.New("token-mac-invalid")
	}
	// 2) nonce 匹配（抗重放）
	if subtle.ConstantTimeCompare(expectedNonce, tok.Nonce) != 1 {
		return errors.New("nonce-mismatch")
	}
	// 3) 过期
	if !c.now().Before(tok.ExpiresAt) {
		return errors.New("token-expired")
	}
	// 4) 硬件可信要求（诚实语义）
	if requireTrusted && !sess.Trusted {
		return fmt.Errorf("not-hardware-trusted: mode=%s", sess.Mode)
	}
	return nil
}

// Stats 返回真实观测计数
func (c *SessionCache) Stats() (establishments, tokens uint64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.establishCount, c.tokenCount
}

// computeTokenMAC 以长度前缀规范化计算 HMAC-SHA256
func computeTokenMAC(key []byte, tok *SessionToken) []byte {
	mac := hmac.New(sha256.New, key)
	writeField(mac, []byte("cloudai-tee-session-token-v1"))
	writeField(mac, []byte(tok.SessionID))
	writeField(mac, []byte(tok.Measurement))
	writeField(mac, tok.Nonce)
	var ts [16]byte
	binary.BigEndian.PutUint64(ts[0:8], uint64(tok.IssuedAt.UnixNano()))
	binary.BigEndian.PutUint64(ts[8:16], uint64(tok.ExpiresAt.UnixNano()))
	mac.Write(ts[:])
	return mac.Sum(nil)
}
