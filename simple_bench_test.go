package main

import "testing"

func SimpleBench(b *testing.B) {
  for i := 0; i < b.N; i++ {
    _ = i + 1
  }
}

func TestMain(m *testing.M) { m.Run() }
