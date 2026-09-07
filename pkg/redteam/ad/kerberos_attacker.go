// Package ad provides SIMULATION-GRADE models of well-documented Active Directory
// attack techniques for the CloudAI Fusion red-team subsystem.
//
// ============================================================================
// SAFETY / HONESTY BOUNDARY (read before use)
// ============================================================================
//   - This package is intended ONLY for AUTHORIZED penetration testing, security
//     research, and defensive detection engineering against MOCK / lab domains.
//   - It DOES NOT contact real Key Distribution Centers or domain controllers,
//     DOES NOT perform real network Kerberos exploitation, and DOES NOT crack
//     real credentials. It contains no weaponized attack or cracking code.
//   - It models the STRUCTURE of publicly documented techniques (MITRE ATT&CK:
//     T1558.003 Kerberoasting, T1003.006 DCSync, T1558.001 Golden Ticket,
//     T1550 Pass-the-Hash/Ticket) so the platform can exercise its own
//     detection/response paths and integration tests deterministically.
//   - All returned artifacts are opaque, deterministic simulations derived from
//     the caller-provided inputs; they are NOT usable against real systems.
// ============================================================================
package ad

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// KerberosAttacker is a simulation harness bound to a (mock) domain context.
// It exposes technique entry points used by the red-team integration tests.
type KerberosAttacker struct {
	domainController string
	domain           string
	spn              string
	password         string
	keytab           string
}

// NewKerberosAttacker constructs a simulation attacker bound to a mock domain.
// dc and domain are required; spn/password/keytab are optional depending on the
// technique being exercised.
func NewKerberosAttacker(dc, domain, spn, password, keytab string) (*KerberosAttacker, error) {
	if dc == "" || domain == "" {
		return nil, errors.New("domain controller and domain are required")
	}
	return &KerberosAttacker{
		domainController: dc,
		domain:           domain,
		spn:              spn,
		password:         password,
		keytab:           keytab,
	}, nil
}

// simArtifact produces a deterministic, opaque simulated artifact for a given
// technique + inputs. Domain-separated so different techniques never collide.
// This is NOT a real credential/ticket and cannot be used against real systems.
func simArtifact(technique string, parts ...string) []byte {
	h := sha256.New()
	h.Write([]byte("cloudai-redteam-sim-v1|" + technique))
	for _, p := range parts {
		h.Write([]byte{0x1f})
		h.Write([]byte(p))
	}
	sum := h.Sum(nil)
	// Return a hex-encoded, technique-labeled blob (well over 8 bytes).
	return []byte(fmt.Sprintf("$sim-%s$%s", technique, hex.EncodeToString(sum)))
}

// Kerberoasting simulates requesting an RC4-HMAC (etype 23) service ticket for
// the given SPN and returns a SIMULATED crackable-hash artifact (deterministic).
// Real Kerberoasting would request a TGS from a KDC; this performs no network I/O.
func (a *KerberosAttacker) Kerberoasting(spn string) ([]byte, error) {
	if spn == "" {
		return nil, errors.New("spn is required for kerberoasting")
	}
	return simArtifact("krb5tgs23", a.domain, spn), nil
}

// DCSync simulates a replication-based secret extraction for targetUser and
// returns a SIMULATED NTLM-hash artifact. No DRSUAPI/replication call is made.
func (a *KerberosAttacker) DCSync(targetUser string) ([]byte, error) {
	if targetUser == "" {
		return nil, errors.New("target user is required for dcsync")
	}
	return simArtifact("ntlm", a.domain, targetUser), nil
}

// GoldenTicket simulates forging a TGT from a krbtgt secret and returns a
// SIMULATED ticket artifact. No real Kerberos PAC/ticket is produced.
func (a *KerberosAttacker) GoldenTicket(krbtgtHash []byte, domainSID, user string) ([]byte, error) {
	if len(krbtgtHash) == 0 {
		return nil, errors.New("krbtgt hash is required for golden ticket")
	}
	if domainSID == "" || user == "" {
		return nil, errors.New("domain SID and user are required for golden ticket")
	}
	return simArtifact("golden", a.domain, domainSID, user, hex.EncodeToString(krbtgtHash)), nil
}

// PassTheHash simulates authenticating with an NTLM hash instead of a password
// and returns a new attacker context representing the (simulated) session.
// No real NTLM/SMB authentication is performed.
func (a *KerberosAttacker) PassTheHash(ntlmHash string) (*KerberosAttacker, error) {
	if len(ntlmHash) < 8 {
		return nil, errors.New("invalid ntlm hash for pass-the-hash")
	}
	moved := *a
	moved.password = "" // authenticated via hash, not password
	return &moved, nil
}
