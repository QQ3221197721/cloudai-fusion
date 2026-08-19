package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// middleware.go turns the Evidence-Native contract into something every HTTP
// endpoint gets for free: a Gin middleware that hashes the request, lets the
// handler run, hashes the response, and emits a signed Receipt. The receipt ID
// and signature are surfaced on response headers so a client can independently
// verify that the exact bytes it received were attested by our key.

// Evidence receipt response header names.
const (
	HeaderEvidenceReceiptID = "X-Evidence-Receipt-ID"
	HeaderEvidenceSignature = "X-Evidence-Signature"
	HeaderEvidenceSigner    = "X-Evidence-Signer"
)

// EvidenceMiddleware automatically generates a Receipt for each API call.
// This means every HTTP endpoint in the platform produces verifiable evidence.
//
// The response body is buffered so the evidence headers can be attached before
// the status line and body are flushed to the client — otherwise headers set
// after the handler wrote its body would be silently dropped.
func EvidenceMiddleware(builder *ReceiptBuilder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if builder == nil {
			c.Next()
			return
		}

		// Capture the request hash before the handler consumes the body.
		inputHash := hashRequest(c.Request)

		// Buffer the response so we can compute its hash and set headers first.
		bw := &bufferedResponseWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = bw

		// Execute handler.
		c.Next()

		// Generate a receipt over the produced response.
		outputHash := sha256.Sum256(bw.body.Bytes())
		receipt, err := builder.BuildRaw(c.FullPath(), inputHash, outputHash)
		if err == nil {
			c.Header(HeaderEvidenceReceiptID, receipt.ID)
			c.Header(HeaderEvidenceSignature, base64.StdEncoding.EncodeToString(receipt.Signature))
			c.Header(HeaderEvidenceSigner, base64.StdEncoding.EncodeToString(receipt.SignerPublicKey))
		}

		// Flush the buffered body through to the real client, now that headers
		// (including the evidence headers) are set.
		bw.flush()
	}
}

// hashRequest computes a SHA-256 over the request method, path, and body. The
// body is fully read and then restored so downstream handlers still see it.
func hashRequest(r *http.Request) [32]byte {
	h := sha256.New()
	if r != nil {
		h.Write([]byte(r.Method))
		if r.URL != nil {
			h.Write([]byte(r.URL.Path))
			h.Write([]byte(r.URL.RawQuery))
		}
		if r.Body != nil {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				_ = r.Body.Close()
				r.Body = io.NopCloser(bytes.NewReader(body))
				h.Write(body)
			}
		}
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// bufferedResponseWriter defers writes to the underlying gin.ResponseWriter so
// the middleware can inspect/attest the full body and inject headers before the
// status line is committed.
type bufferedResponseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write buffers the bytes instead of writing them straight to the client.
func (w *bufferedResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

// WriteString buffers the string, matching gin's ResponseWriter contract.
func (w *bufferedResponseWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

// WriteHeaderNow is intentionally a no-op: it prevents gin's renderers from
// prematurely flushing the status line + headers before evidence headers are added.
func (w *bufferedResponseWriter) WriteHeaderNow() {}

// flush commits the buffered status line, headers, and body to the real client.
func (w *bufferedResponseWriter) flush() {
	w.ResponseWriter.WriteHeaderNow()
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
	}
}
