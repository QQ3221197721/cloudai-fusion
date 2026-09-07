package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	b, err := os.ReadFile("api/openapi.yaml")
	if err != nil {
		fmt.Println("read err:", err)
		os.Exit(1)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		fmt.Println("YAML PARSE ERROR:", err)
		os.Exit(1)
	}
	paths, _ := doc["paths"].(map[string]any)
	soc, rng, bench := 0, 0, 0
	for p := range paths {
		switch {
		case len(p) >= 12 && p[:12] == "/api/v1/soc/":
			soc++
		}
		if contains(p, "ranges") {
			rng++
		}
		if contains(p, "benchmark") {
			bench++
		}
	}
	fmt.Printf("OK paths=%d soc=%d ranges=%d benchmark=%d\n", len(paths), soc, rng, bench)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
