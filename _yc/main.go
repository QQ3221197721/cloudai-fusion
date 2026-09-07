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
	_, hasMit := paths["/api/v1/soc/mitigations"]
	fmt.Printf("OK paths=%d soc/mitigations=%v\n", len(paths), hasMit)
}
