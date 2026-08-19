package tutorial

// validator_test.go unit-tests the validator interface and all three
// implementations.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAlwaysPassValidator(t *testing.T) {
	v := &AlwaysPassValidator{}
	ok, msg, err := v.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("always pass should pass")
	}
	if msg == "" {
		t.Error("expected a message")
	}
}

func TestAlwaysPassValidator_CustomMessage(t *testing.T) {
	v := &AlwaysPassValidator{Message: "custom"}
	ok, msg, _ := v.Validate(context.Background())
	if !ok || msg != "custom" {
		t.Errorf("got ok=%v msg=%q", ok, msg)
	}
}

func TestAlwaysPassValidator_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := &AlwaysPassValidator{}
	ok, _, err := v.Validate(ctx)
	if ok || err == nil {
		t.Error("should fail on cancelled context")
	}
}

func TestFileExistsValidator_Exists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(path, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	v := &FileExistsValidator{Path: path}
	ok, msg, err := v.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Errorf("should pass: %s", msg)
	}
}

func TestFileExistsValidator_Missing(t *testing.T) {
	v := &FileExistsValidator{Path: filepath.Join(t.TempDir(), "does-not-exist.txt")}
	ok, _, err := v.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("should fail for missing file")
	}
}

func TestFileExistsValidator_Dir(t *testing.T) {
	dir := t.TempDir()
	v := &FileExistsValidator{Path: dir, Dir: true}
	ok, _, err := v.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("should pass for existing directory")
	}
}

func TestFileExistsValidator_DirButFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	_ = os.WriteFile(path, nil, 0644)
	v := &FileExistsValidator{Path: path, Dir: true}
	ok, _, err := v.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("should fail: path is a file, not a directory")
	}
}

func TestFileExistsValidator_EmptyPath(t *testing.T) {
	v := &FileExistsValidator{Path: ""}
	_, _, err := v.Validate(context.Background())
	if err == nil {
		t.Error("expected error for empty path")
	}
}

func TestCommandOutputValidator_Match(t *testing.T) {
	v := &CommandOutputValidator{
		Command: "cmd",
		Args:    []string{"/c", "echo", "hello world"},
		Pattern: "hello",
	}
	ok, msg, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !ok {
		t.Errorf("should pass: %s", msg)
	}
}

func TestCommandOutputValidator_NoMatch(t *testing.T) {
	v := &CommandOutputValidator{
		Command: "cmd",
		Args:    []string{"/c", "echo", "foo"},
		Pattern: "^bar$",
	}
	ok, _, err := v.Validate(context.Background())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if ok {
		t.Error("should not match")
	}
}

func TestCommandOutputValidator_InvalidPattern(t *testing.T) {
	v := &CommandOutputValidator{
		Command: "echo",
		Args:    []string{"x"},
		Pattern: "[invalid",
	}
	_, _, err := v.Validate(context.Background())
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestCommandOutputValidator_EmptyCommand(t *testing.T) {
	v := &CommandOutputValidator{Pattern: "x"}
	_, _, err := v.Validate(context.Background())
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestNewValidator(t *testing.T) {
	tests := []struct {
		name    string
		step    Step
		wantErr bool
	}{
		{"always-pass", Step{ID: "a", ValidatorType: ValidatorAlwaysPass}, false},
		{"empty-type-defaults-to-always-pass", Step{ID: "b", ValidatorType: ""}, false},
		{"file-exists-ok", Step{ID: "c", ValidatorType: ValidatorFileExists, ValidatorParams: map[string]string{"path": "/tmp"}}, false},
		{"file-exists-no-path", Step{ID: "d", ValidatorType: ValidatorFileExists}, true},
		{"command-ok", Step{ID: "e", ValidatorType: ValidatorCommandOutput, ValidatorParams: map[string]string{"command": "echo"}}, false},
		{"command-no-cmd", Step{ID: "f", ValidatorType: ValidatorCommandOutput}, true},
		{"unknown-type", Step{ID: "g", ValidatorType: "xyz"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewValidator(tc.step)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}
