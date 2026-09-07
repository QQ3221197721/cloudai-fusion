//go:build compbench

package security

import (
	"testing"
)

func TestDiagStateCount(t *testing.T) {
	patterns := buildPatternSet()
	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(ACPattern{Pattern: p, ID: p})
	}
	ac.Build()
	numStates := len(ac.stateOut)
	tableBytes := len(ac.gotoTable) * 4
	t.Logf("numStates:        %d", numStates)
	t.Logf("gotoTable entries:%d", len(ac.gotoTable))
	t.Logf("gotoTable bytes:  %d (%.2f MB)", tableBytes, float64(tableBytes)/(1024*1024))

	// Count distinct live bytes (bytes that appear in at least one pattern)
	var live [256]bool
	for _, p := range patterns {
		for i := 0; i < len(p); i++ {
			live[acLowerByte(p[i])] = true
		}
	}
	k := 0
	for _, v := range live {
		if v {
			k++
		}
	}
	t.Logf("live alphabet K:  %d", k)
	t.Logf("reduced table:    %.2f MB (width=%d)", float64(numStates*(k+1)*4)/(1024*1024), k+1)
}
