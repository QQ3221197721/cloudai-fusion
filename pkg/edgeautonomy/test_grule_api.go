//go:build ignore
// +build ignore

package edgeautonomy

import (
	"context"
	"fmt"

	"github.com/hyperjumptech/grule-rule-engine/engine"
	"github.com/hyperjumptech/grule-rule-engine/ast"
)

func main() {
	ctx := context.Background()
	
	// Create Grule engine
	engine := engine.NewGruleEngine()
	
	// Create knowledge base with a simple rule
	kb := ast.NewKnowledgeLibrary()
	err := kb.Add(
		ast.NewRule("RULE TestHighUtil WHEN GPUUtil > 80 THEN ScaleDown=-1 END_RULE"),
	)
	if err != nil {
		panic(fmt.Sprintf("Failed to add rule: %v", err))
	}
	
	// Create data context and set variables
	dataCtx := ast.NewDataContext()
	err = dataCtx.Prepare(ast.NewExpressionLibrary(), map[string]interface{}{
		"GPUUtil":      85.0,
		"MemoryPercent": 92.0,
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to setup data context: %v", err))
	}
	
	// Execute rules
	err = engine.ExecuteWithContext(ctx, dataCtx, kb.KnowledgeBase)
	if err != nil {
		fmt.Printf("Execution error: %v\n", err)
	}
	
	// Get results
	scaleDown, _ := dataCtx.GetVariable("ScaleDown")
	fmt.Printf("ScaleDown variable: %v\n", scaleDown)
}
