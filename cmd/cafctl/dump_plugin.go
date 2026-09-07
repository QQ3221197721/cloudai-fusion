package main

import (
	"fmt"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin/contrib"
)

func main() {
	m := contrib.GetPluginManifests()
	fmt.Printf("First plugin name: %s\n", m[0].Metadata.Name)
	fmt.Printf("Has 'collector' in name: %v\n", contains(m[0].Metadata.Name, "collector"))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
