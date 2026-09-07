package modelregistry

import (
	"os"
	"path/filepath"
	"strings"
)

// Benchmark helpers to avoid generating temporary directories that pollute the workspace.
// All temp files go to E:\tmp\modelregistry-benchmarks by default, unless overridden.

const DefaultBenchTempDir = `E:\tmp\modelregistry-benchmarks`

func init() {
	_ = os.MkdirAll(DefaultBenchTempDir, 0o755)
}

// writeTempFile creates a small file in the benchmark temp directory instead of
// TempDir(). This keeps benchmarks isolated and avoids transient disk churn under
// the project root. The path is safely inside DefaultBenchTempDir.
func writeTempFile(name string, data []byte) (string, error) {
	dir := DefaultBenchTempDir
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	// Sanitize the result: ensure it starts with dir + separator
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, dir+string(filepath.Separator)) {
		return "", ErrNotFound // reuse sentinel as "path escapes" signal
	}
	return abs, nil
}
