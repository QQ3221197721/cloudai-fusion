package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Println("read error:", err)
		os.Exit(1)
	}
	var d map[string]any
	if err := yaml.Unmarshal(b, &d); err != nil {
		fmt.Println("YAML ERROR:", err)
		os.Exit(1)
	}
	paths, _ := d["paths"].(map[string]any)
	_, rt := paths["/api/v1/redteam/engagements"]
	_, rtRep := paths["/api/v1/redteam/engagements/{id}/report"]
	_, ev := paths["/api/v1/evidence/pubkey"]
	fmt.Printf("YAML_OK paths=%d redteam=%v report=%v evidence=%v\n", len(paths), rt, rtRep, ev)
}
