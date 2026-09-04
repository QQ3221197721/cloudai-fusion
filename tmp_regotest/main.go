package main

import (
	"context"
	"fmt"

	regolib "github.com/open-policy-agent/opa/rego"
)

const policy = `package compliance.rego

import rego.v1

default gdpr_art32 := false
gdpr_art32 if {
	count([r | some r in input.resources; r.encrypted == true]) > 0
	input.network_config.ssl_required == true
	input.network_config.requires_vpn == true
}

report["GDPR-Art32"] := gdpr_art32
`

func main() {
	ctx := context.Background()
	tr := regolib.New(
		regolib.Query("data.compliance.rego.report"),
		regolib.Module("compliance.rego", policy),
	)
	pq, err := tr.PrepareForEval(ctx)
	if err != nil {
		fmt.Println("PREPARE ERROR:", err)
		return
	}
	input := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{"encrypted": true},
		},
		"network_config": map[string]interface{}{"ssl_required": true, "requires_vpn": true},
	}
	rs, err := pq.Eval(ctx, regolib.EvalInput(input))
	if err != nil {
		fmt.Println("EVAL ERROR:", err)
		return
	}
	fmt.Printf("RESULT: %+v\n", rs[0].Expressions[0].Value)
}
