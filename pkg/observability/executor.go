package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// StepExecutor executes a single runbook step and returns its combined output.
// A non-nil error marks the step as failed, except for the sentinel errors
// below which mark steps that were intentionally not run (skipped) or that
// require manual operator action.
type StepExecutor interface {
	Execute(ctx context.Context, step RunbookStep) (output string, err error)
}

// Sentinel errors distinguishing non-failure outcomes from real failures.
var (
	// ErrStepSkipped indicates the step was intentionally not executed, e.g.
	// host command execution is disabled by policy.
	ErrStepSkipped = errors.New("step skipped")
	// ErrStepManual indicates the step requires manual operator action.
	ErrStepManual = errors.New("manual step")
)

// defaultStepExecutor executes runbook steps against real endpoints and, when
// permitted, the local host shell.
type defaultStepExecutor struct {
	httpClient   *http.Client
	allowCommand bool
}

// newDefaultStepExecutor builds the production step executor. Host command
// execution is only performed when allowCommand is true.
func newDefaultStepExecutor(allowCommand bool) *defaultStepExecutor {
	return &defaultStepExecutor{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		allowCommand: allowCommand,
	}
}

// Execute dispatches a step to the appropriate handler based on its type.
func (e *defaultStepExecutor) Execute(ctx context.Context, step RunbookStep) (string, error) {
	switch step.Type {
	case StepTypeHTTP, StepTypeWebhook:
		return e.executeHTTP(ctx, step)
	case StepTypeCommand, StepTypeScript, StepTypeKubectl:
		return e.executeCommand(ctx, step)
	case StepTypeManual:
		return "", ErrStepManual
	case StepTypeCondition:
		return e.evaluateCondition(step)
	default:
		return "", fmt.Errorf("unsupported step type %q", step.Type)
	}
}

// executeHTTP performs a real HTTP request to the step's endpoint and treats a
// non-2xx/3xx response as a failure.
func (e *defaultStepExecutor) executeHTTP(ctx context.Context, step RunbookStep) (string, error) {
	url := strings.TrimSpace(step.Webhook)
	if url == "" {
		return "", fmt.Errorf("step %q: no webhook/URL configured", step.Name)
	}
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	method := http.MethodGet
	if step.Type == StepTypeWebhook {
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(reqCtx, method, url, nil)
	if err != nil {
		return "", fmt.Errorf("step %q: build request: %w", step.Name, err)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("step %q: request failed: %w", step.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	summary := strings.TrimSpace(fmt.Sprintf("HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body))))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return summary, fmt.Errorf("step %q: unexpected status %d", step.Name, resp.StatusCode)
	}
	return summary, nil
}

// executeCommand runs a command/script/kubectl step on the host shell when
// command execution is enabled; otherwise the step is skipped (never faked).
func (e *defaultStepExecutor) executeCommand(ctx context.Context, step RunbookStep) (string, error) {
	if !e.allowCommand {
		return "", ErrStepSkipped
	}
	cmdline := step.Command
	if step.Type == StepTypeScript && step.Script != "" {
		cmdline = step.Script
	}
	if strings.TrimSpace(cmdline) == "" {
		return "", fmt.Errorf("step %q: empty command", step.Name)
	}
	timeout := step.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	name, args := shellInvocation(cmdline)
	out, err := exec.CommandContext(runCtx, name, args...).CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("step %q: command failed: %w", step.Name, err)
	}
	return output, nil
}

// evaluateCondition performs a truthiness check on the step's "condition" param.
func (e *defaultStepExecutor) evaluateCondition(step RunbookStep) (string, error) {
	cond := strings.TrimSpace(step.Params["condition"])
	switch strings.ToLower(cond) {
	case "", "true", "1", "yes":
		return fmt.Sprintf("condition satisfied: %q", cond), nil
	default:
		return "", fmt.Errorf("step %q: condition not satisfied (%q)", step.Name, cond)
	}
}

// shellInvocation returns the OS-appropriate shell wrapper for a command line.
func shellInvocation(cmdline string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", cmdline}
	}
	return "sh", []string{"-c", cmdline}
}
