// Package billing - Stripe webhook signature verification
package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidSignature   = errors.New("invalid stripe signature")
	ErrSignatureExpired   = errors.New("stripe signature timestamp expired")
	ErrMalformedSignature = errors.New("malformed stripe-signature header")
	ErrMissingSignature   = errors.New("stripe-signature header missing v1 signature")
)

// stripeTolerance is the maximum acceptable age of a webhook (5 minutes)
const stripeTolerance = 5 * time.Minute

// VerifyStripeSignature verifies a Stripe webhook signature per Stripe's spec.
// The Stripe-Signature header format is: t=<timestamp>,v1=<signature>[,v1=<signature>...]
// The signed payload is: <timestamp>.<payload>, HMAC-SHA256'd with the webhook secret.
func VerifyStripeSignature(payload []byte, sigHeader string, webhookSecret string) error {
	if webhookSecret == "" {
		return errors.New("webhook secret is empty")
	}
	if sigHeader == "" {
		return ErrMalformedSignature
	}

	timestamp, signatures, err := parseStripeSignatureHeader(sigHeader)
	if err != nil {
		return err
	}

	if len(signatures) == 0 {
		return ErrMissingSignature
	}

	// Check timestamp tolerance to prevent replay attacks
	ts := time.Unix(timestamp, 0)
	if time.Since(ts) > stripeTolerance {
		return ErrSignatureExpired
	}
	// Reject timestamps too far in the future too
	if time.Until(ts) > stripeTolerance {
		return ErrSignatureExpired
	}

	// Compute the expected signature
	signedPayload := fmt.Sprintf("%d.%s", timestamp, string(payload))
	expectedSig := computeHMAC(signedPayload, webhookSecret)

	// Compare against all provided v1 signatures using constant-time comparison
	for _, sig := range signatures {
		sigBytes, decErr := hex.DecodeString(sig)
		if decErr != nil {
			continue
		}
		if hmac.Equal(sigBytes, expectedSig) {
			return nil
		}
	}

	return ErrInvalidSignature
}

// parseStripeSignatureHeader parses the Stripe-Signature header into timestamp and v1 signatures
func parseStripeSignatureHeader(header string) (timestamp int64, signatures []string, err error) {
	parts := strings.Split(header, ",")
	timestamp = -1

	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, value := kv[0], kv[1]

		switch key {
		case "t":
			ts, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil {
				return 0, nil, ErrMalformedSignature
			}
			timestamp = ts
		case "v1":
			signatures = append(signatures, value)
		}
	}

	if timestamp < 0 {
		return 0, nil, ErrMalformedSignature
	}

	return timestamp, signatures, nil
}

// computeHMAC computes HMAC-SHA256 of the payload with the given secret
func computeHMAC(payload, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// GenerateStripeSignatureHeader produces a valid Stripe-Signature header for a payload.
// Primarily used for testing webhook signature verification.
func GenerateStripeSignatureHeader(payload []byte, webhookSecret string, timestamp time.Time) string {
	ts := timestamp.Unix()
	signedPayload := fmt.Sprintf("%d.%s", ts, string(payload))
	sig := computeHMAC(signedPayload, webhookSecret)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(sig))
}

// VerifyWebhook is a convenience method on StripeWebhookHandler that verifies
// the request signature before processing.
func (h *StripeWebhookHandler) VerifyWebhook(payload []byte, sigHeader string) error {
	return VerifyStripeSignature(payload, sigHeader, h.webhookSecret)
}
