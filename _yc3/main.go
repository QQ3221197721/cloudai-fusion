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
	var d map[string]any
	if err := yaml.Unmarshal(b, &d); err != nil {
		fmt.Println("PARSE ERR:", err)
		os.Exit(1)
	}
	p, _ := d["paths"].(map[string]any)
	_, ok := p["/api/v1/soc/detect"]
	fmt.Printf("OK paths=%d soc/detect=%v\n", len(p), ok)
}
