package main

import (
	"bytes"
	"testing"
)

func TestWireCmdDebug(t *testing.T) {
	parent := newAutoscaleCmd()
	subCmd, _, err := parent.Find([]string{"policy-add"})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	
	buf := &bytes.Buffer{}
	subCmd.SetOut(buf)
	subCmd.SetErr(buf)
	
	subCmd.SetArgs([]string{"--name", "test", "--store", t.TempDir()})
	err = subCmd.Execute()
	t.Logf("output: %q", buf.String())
	t.Logf("err: %v", err)
}
