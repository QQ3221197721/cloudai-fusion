package main

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	// Files to process with their min/max replacements
	files := map[string][]string{
		"pkg/redteam/types_core_modules.go":            {"min(score, 100)", "helpers.MinInt(score, 100)"},
		"pkg/redteam/m34_types.go":                     {"min(5,", "helpers.MinInt(5,"},
		"pkg/redteam/cross_patent_coordination.go":     []string{"min(10, len(", "helpers.MinInt(10, len("),
		"pkg/redteam/intelligence/detection_rules.go":  []string{"max(float64", "helpers.MaxFloat64("},
		"pkg/redteam/patent/helpers.go":                nil, // Remove entire file
		"pkg/redteam/ad_kerberos/native/crypto/kerberos_crypto.go": nil, // Remove entire file
	}

	for filePath, replacers := range files {
		if err := fixFile(filePath, replacers); err != nil {
			log.Printf("Error fixing %s: %v", filePath, err)
		} else {
			log.Printf("Fixed: %s", filePath)
		}
	}

	// Also remove some entire files
	removeFiles := []string{
		"pkg/redteam/patent/helpers.go",
		"pkg/redteam/ad_kerberos/native/crypto/kerberos_crypto.go",
	}

	for _, f := range removeFiles {
		if err := os.Remove(f); err == nil {
			log.Printf("Removed: %s", f)
		}
	}
}

func fixFile(path string, replacers interface{}) error {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	strContent := string(content)

	switch v := replacers.(type) {
	case []string:
		for i := 0; i < len(v)-1; i += 2 {
			strContent = strings.ReplaceAll(strContent, v[i], v[i+1])
		}
	case string:
		// Single replacement
	default:
		return nil
	}

	return ioutil.WriteFile(path, []byte(strContent), 0644)
}

var _ = filepath.Join