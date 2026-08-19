package tutorial

// validator.go implements the step completion checkers. Every validator does
// real work — no simulated results:
//
//   - FileExistsValidator stats a real filesystem path.
//   - CommandOutputValidator executes a real process via os/exec and matches its
//     combined output against a compiled regular expression.
//   - AlwaysPassValidator satisfies pure-reading steps.
//
// NewValidator resolves a step's declared ValidatorType + params into a concrete
// Validator, so tutorial definitions stay pure data (JSON) while validation
// stays real code.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Validator decides whether a tutorial step has been satisfied. It returns
// whether the check passed, a human-readable message shown to the learner, and
// an error only when the check itself could not be performed.
type Validator interface {
	Validate(ctx context.Context) (bool, string, error)
}

// AlwaysPassValidator satisfies steps that only require reading, so the learner
// advances by acknowledging the instruction.
type AlwaysPassValidator struct {
	// Message overrides the default acknowledgement text when non-empty.
	Message string
}

// Validate always passes.
func (v *AlwaysPassValidator) Validate(ctx context.Context) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "validation cancelled", err
	}
	msg := v.Message
	if msg == "" {
		msg = "reading step acknowledged"
	}
	return true, msg, nil
}

// FileExistsValidator passes when Path exists on the filesystem. When Dir is
// true the path must additionally be a directory.
type FileExistsValidator struct {
	Path string
	Dir  bool
}

// Validate stats the configured path. A missing path is a failed check (not an
// error); an unexpected stat failure (e.g. permission denied) is an error.
func (v *FileExistsValidator) Validate(ctx context.Context) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "validation cancelled", err
	}
	if v.Path == "" {
		return false, "", fmt.Errorf("tutorial: file_exists validator requires a path")
	}
	info, err := os.Stat(v.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Sprintf("%s does not exist yet", v.Path), nil
		}
		return false, "", fmt.Errorf("tutorial: stat %s: %w", v.Path, err)
	}
	if v.Dir && !info.IsDir() {
		return false, fmt.Sprintf("%s exists but is not a directory", v.Path), nil
	}
	if !v.Dir && info.IsDir() {
		return false, fmt.Sprintf("%s exists but is a directory", v.Path), nil
	}
	return true, fmt.Sprintf("%s found", v.Path), nil
}

// CommandOutputValidator runs a command and passes when the combined
// stdout+stderr matches Pattern. It is how "run this and show me it works"
// steps are graded.
type CommandOutputValidator struct {
	// Command is the executable to run; Args are passed verbatim.
	Command string
	Args    []string
	// Pattern is a Go regular expression matched against the combined output.
	Pattern string
	// Dir optionally sets the working directory.
	Dir string
	// Timeout bounds the execution; defaults to DefaultCommandTimeout.
	Timeout time.Duration
	// RequireZeroExit additionally demands a zero exit status.
	RequireZeroExit bool
}

// DefaultCommandTimeout bounds command validators that do not set a Timeout.
const DefaultCommandTimeout = 30 * time.Second

// Validate executes the command and matches its output. A non-matching output or
// non-zero exit (when required) is a failed check; only setup problems such as an
// uncompilable pattern or a missing executable surface as errors.
func (v *CommandOutputValidator) Validate(ctx context.Context) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "validation cancelled", err
	}
	if v.Command == "" {
		return false, "", fmt.Errorf("tutorial: command_output validator requires a command")
	}
	re, err := regexp.Compile(v.Pattern)
	if err != nil {
		return false, "", fmt.Errorf("tutorial: invalid pattern %q: %w", v.Pattern, err)
	}

	timeout := v.Timeout
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, v.Command, v.Args...)
	cmd.Dir = v.Dir
	out, runErr := cmd.CombinedOutput()

	if runErr != nil {
		// An ExitError means the process ran and failed — that is a validation
		// outcome. Anything else (binary not found, context deadline) is an error.
		var exitErr *exec.ExitError
		if !asExitError(runErr, &exitErr) {
			return false, "", fmt.Errorf("tutorial: run %s: %w", v.Command, runErr)
		}
		if v.RequireZeroExit {
			return false, fmt.Sprintf("command exited %d: %s", exitErr.ExitCode(), trimOutput(out)), nil
		}
	}

	if !re.Match(out) {
		return false, fmt.Sprintf("output did not match %q: %s", v.Pattern, trimOutput(out)), nil
	}
	return true, fmt.Sprintf("output matched %q", v.Pattern), nil
}

// asExitError reports whether err is (or wraps) an *exec.ExitError, storing it
// in target when it is.
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// trimOutput shortens command output for display in validator messages.
func trimOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	const max = 200
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// NewValidator resolves a step's declared validator type and params into a
// concrete Validator. Recognized params:
//
//	file_exists:    path (required), dir ("true" to require a directory)
//	command_output: command (required), args (space-separated), pattern, dir,
//	                require_zero_exit ("true")
//	always_pass:    message
func NewValidator(s Step) (Validator, error) {
	get := func(k string) string { return s.ValidatorParams[k] }

	switch s.ValidatorType {
	case ValidatorAlwaysPass, "":
		return &AlwaysPassValidator{Message: get("message")}, nil

	case ValidatorFileExists:
		path := get("path")
		if path == "" {
			return nil, fmt.Errorf("tutorial: step %q: file_exists requires param %q", s.ID, "path")
		}
		return &FileExistsValidator{Path: path, Dir: get("dir") == "true"}, nil

	case ValidatorCommandOutput:
		command := get("command")
		if command == "" {
			return nil, fmt.Errorf("tutorial: step %q: command_output requires param %q", s.ID, "command")
		}
		v := &CommandOutputValidator{
			Command:         command,
			Pattern:         get("pattern"),
			Dir:             get("dir"),
			RequireZeroExit: get("require_zero_exit") == "true",
		}
		if raw := get("args"); raw != "" {
			v.Args = strings.Fields(raw)
		}
		if raw := get("timeout"); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return nil, fmt.Errorf("tutorial: step %q: invalid timeout %q: %w", s.ID, raw, err)
			}
			v.Timeout = d
		}
		return v, nil

	default:
		return nil, fmt.Errorf("tutorial: step %q: unknown validator type %q", s.ID, s.ValidatorType)
	}
}

// ValidateStep resolves and runs the validator for a step of the tutorial, and
// marks the step Completed in p when the check passes. Prerequisite gating is
// enforced by Progress.Complete, so a passing validator on a blocked step still
// fails loudly rather than skipping ahead.
func ValidateStep(ctx context.Context, p *Progress, stepID string) (bool, string, error) {
	if p == nil {
		return false, "", fmt.Errorf("tutorial: nil progress")
	}
	step, ok := p.Tutorial().StepByID(stepID)
	if !ok {
		return false, "", fmt.Errorf("tutorial: unknown step %q", stepID)
	}
	v, err := NewValidator(step)
	if err != nil {
		return false, "", err
	}
	passed, msg, err := v.Validate(ctx)
	if err != nil {
		return false, msg, err
	}
	if !passed {
		return false, msg, nil
	}
	if err := p.Complete(stepID); err != nil {
		return false, msg, err
	}
	return true, msg, nil
}
