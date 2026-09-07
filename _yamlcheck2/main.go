package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// throwaway validator: confirm the CI workflow YAML parses and the new keys exist.
func main() {
	for _, p := range []string{".github/workflows/ci.yml"} {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Println("read err:", err)
			os.Exit(1)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			fmt.Printf("YAML PARSE ERROR in %s: %v\n", p, err)
			os.Exit(1)
		}
		fmt.Printf("OK %s parsed; top-level keys=%d\n", p, len(doc))
	}
}
