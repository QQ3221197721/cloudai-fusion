//go:build ignore

package detect

import (
	"fmt"
	"math/rand"
)

func init() {
	rng := rand.New(rand.NewSource(42))
	benchPatternTokens := []string{
		"powershell.exe", "cmd.exe", "system32", "admin", "windows",
		"net user", "lsass.exe", "mimikatz", "base64", "encoded",
	}

	for i := range 50 {
		fieldIdx := rng.Intn(5) // Image, CommandLine, FileName, User, ProcessName
		fieldNames := []string{"Image", "CommandLine", "FileName", "User", "ProcessName"}
		valIdx := rng.Intn(10)
		
		fmt.Printf("Rule %d: Field=%s, Value=%s\n", i, fieldNames[fieldIdx], benchPatternTokens[valIdx])
	}
	
	fmt.Println("\n\nEvent sample:")
	tok := benchPatternTokens[rng.Intn(len(benchPatternTokens))]
	fmt.Printf("Spliced token into Image: ...\n%s...\n", tok)
}
