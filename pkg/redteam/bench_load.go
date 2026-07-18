package redteam

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// bench_load.go loads CVE-Bench / Cybench-style benchmark cases from JSON files
// so a real dataset can be dropped into a directory and run by the harness
// (bench.go). Each *.json file is one BenchCase. This is what a CI regression
// gate iterates over: any drop in solve rate, any scope violation, or any
// unverifiable receipt fails the build.
func LoadBenchCases(dir string) ([]BenchCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("redteam: read bench dir %q: %w", dir, err)
	}
	var cases []BenchCase
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("redteam: read bench case %q: %w", e.Name(), err)
		}
		var c BenchCase
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("redteam: parse bench case %q: %w", e.Name(), err)
		}
		if c.Name == "" {
			c.Name = strings.TrimSuffix(e.Name(), ".json")
		}
		cases = append(cases, c)
	}
	return cases, nil
}
