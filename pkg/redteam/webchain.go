package redteam

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// webchain.go is the M3 web exploit-chain engine (OSWE-class). A chain is an
// ordered set of HTTP steps where later steps consume values extracted from
// earlier responses (e.g. an auth token). This is real HTTP against an authorized
// target; each step's request and response are fingerprinted (SHA-256) so the
// chain is fully reproducible from the signed evidence WITHOUT storing secrets.

// Extractor pulls a value out of a step's response into a chain variable that
// later steps can reference via {{var}} substitution.
type Extractor struct {
	Var    string `json:"var"`
	Source string `json:"source"` // "header" | "body-regex" | "status"
	Header string `json:"header,omitempty"`
	Regex  string `json:"regex,omitempty"` // capture group 1 for body-regex
}

// WebStep is one HTTP request in a chain.
type WebStep struct {
	Name            string            `json:"name"`
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body,omitempty"`
	Extract         []Extractor       `json:"extract,omitempty"`
	SuccessStatus   int               `json:"success_status,omitempty"` // 0 = any 2xx
	SuccessContains string            `json:"success_contains,omitempty"`
}

// WebChain is a named, technique-tagged sequence of steps against a base URL.
type WebChain struct {
	Name      string    `json:"name"`
	Technique string    `json:"technique"` // e.g. T1190
	BaseURL   string    `json:"base_url"`
	Steps     []WebStep `json:"steps"`
}

// WebStepResult is the recorded (non-secret) outcome of one step.
type WebStepResult struct {
	Name         string   `json:"name"`
	Status       int      `json:"status"`
	RequestHash  string   `json:"request_hash"`
	ResponseHash string   `json:"response_hash"`
	Extracted    []string `json:"extracted,omitempty"` // variable NAMES only
	OK           bool     `json:"ok"`
}

// WebChainResult is the outcome of running a chain.
type WebChainResult struct {
	Steps   []WebStepResult `json:"steps"`
	Success bool            `json:"success"`
	vars    map[string]string
}

// RunWebChain executes the chain over real HTTP, substituting extracted variables
// into later steps. It never stores raw secrets - only request/response hashes and
// extracted variable names - so the transcript is reproducible and verifiable.
func RunWebChain(ctx context.Context, client *http.Client, chain WebChain) (*WebChainResult, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	vars := map[string]string{}
	res := &WebChainResult{vars: vars}

	for _, step := range chain.Steps {
		method := step.Method
		if method == "" {
			method = http.MethodGet
		}
		path := substVars(step.Path, vars)
		body := substVars(step.Body, vars)
		headers := make(map[string]string, len(step.Headers))
		for k, v := range step.Headers {
			headers[k] = substVars(v, vars)
		}
		reqHash := requestFingerprint(method, path, headers, body)

		req, err := http.NewRequestWithContext(ctx, method, chain.BaseURL+path, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			res.Steps = append(res.Steps, WebStepResult{Name: step.Name, RequestHash: reqHash, OK: false})
			res.Success = false
			return res, nil
		}
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		respHash := sha256Hex(append([]byte(strconv.Itoa(resp.StatusCode)+"\n"), respBody...))

		ok := statusOK(resp.StatusCode, step.SuccessStatus) &&
			(step.SuccessContains == "" || strings.Contains(string(respBody), step.SuccessContains))

		var names []string
		for _, ex := range step.Extract {
			if val := extractValue(ex, resp, respBody); val != "" {
				vars[ex.Var] = val
				names = append(names, ex.Var)
			}
		}
		res.Steps = append(res.Steps, WebStepResult{
			Name: step.Name, Status: resp.StatusCode, RequestHash: reqHash,
			ResponseHash: respHash, Extracted: names, OK: ok,
		})
		if !ok {
			res.Success = false
			return res, nil
		}
	}
	res.Success = true
	return res, nil
}

// substVars replaces {{var}} placeholders with extracted values.
func substVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

// statusOK reports whether a status matches the step's success condition.
func statusOK(status, want int) bool {
	if want > 0 {
		return status == want
	}
	return status >= 200 && status < 300
}

// extractValue pulls a value from a response per the extractor spec.
func extractValue(ex Extractor, resp *http.Response, body []byte) string {
	switch strings.ToLower(ex.Source) {
	case "header":
		return resp.Header.Get(ex.Header)
	case "status":
		return strconv.Itoa(resp.StatusCode)
	case "body-regex":
		re, err := regexp.Compile(ex.Regex)
		if err != nil {
			return ""
		}
		if m := re.FindSubmatch(body); len(m) >= 2 {
			return string(m[1])
		}
	}
	return ""
}

// requestFingerprint returns a deterministic SHA-256 over the (substituted)
// method, path, sorted headers, and body - enough to reproduce the request.
func requestFingerprint(method, path string, headers map[string]string, body string) string {
	var b strings.Builder
	b.WriteString(method)
	b.WriteByte('\n')
	b.WriteString(path)
	b.WriteByte('\n')
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, headers[k])
	}
	b.WriteByte('\n')
	b.WriteString(body)
	return sha256Hex([]byte(b.String()))
}
