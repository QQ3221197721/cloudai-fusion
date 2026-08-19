package qa

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// qa_bench_test.go provides >=6 deterministic, measurable benchmarks for the four analyzers:
// 1) Coverage parser throughput (coverage_parse_throughput)
// 2) Coverage gate decision latency (coverage_gate_latency)
// 3) Regression comparison speed (regression_compare_latency)
// 4) Lint YAML load & parse (lint_yaml_load)
// 5) Lint AST static pass performance (lint_ast_pass)
// 6) BenchDB save/load/compare (benchdb_store_and_read)

const benchSrc = `package foo
func Parse() int { return 0 }
func Marshal(v interface{}) ([]byte, error) { return nil, nil }
func Unmarshal(data []byte, v interface{}) error { return nil }
func Validate(req Request) Response { return Response{} }
func Filter(items []Item) []Item { out := make([]Item,0,len(items)); for _,i:=range items{if i.Valid{out=append(out,i)}}; return out }
func Encode(buf []byte,m map[string]int){for k,v:=range m{buf=append(buf,[]byte(k+fmt.Sprint(v))...)}}
func Decode(s string) map[string]int{out:=make(map[string]int);return out}
`

func BenchmarkCoverageParseThroughput(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := ""
		for j := 0; j < 100; j++ { input += fmt.Sprintf("github.com/acme/pkg/file%d.go:10: Func_%d 85.7%%\n", j, j) }
		input += "total:\t(statements)\t90.0%%\n"
		_, _ = ParseFuncCoverage(strings.NewReader(input))
	}
	b.ReportAllocs()
}

func BenchmarkCoverageGateLatency(b *testing.B) {
	report := &CoverageReport{Total: 90.0, Packages: map[string]float64{"github.com/acme": 95.0}, Funcs: []FuncCoverage{{Function:"F",Percent:95}}}
	cfg := CoverageThreshold{MinTotal:80, MinPackage:map[string]float64{"github.com/acme":85}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Gate(report, cfg)
	}
}

func BenchmarkRegressionCompareLatency(b *testing.B) {
	base := &BenchRun{Samples: make([]BenchSample, 100)}
	for i := range base.Samples { base.Samples[i] = BenchSample{Name:fmt.Sprintf("Bench/%d",i),NsPerOp:float64(1000+i),BytesPerOp:int64(100+i),AllocsPerOp:int64(5+i)} }
	cur := &BenchRun{Samples: make([]BenchSample, 100)}
	for i := range cur.Samples { cur.Samples[i] = BenchSample{Name:base.Samples[i].Name, NsPerOp:base.Samples[i].NsPerOp*1.05, BytesPerOp:int64(float64(base.Samples[i].BytesPerOp)*1.02), AllocsPerOp:base.Samples[i].AllocsPerOp} }
	cfg := RegressConfig{MaxTimePct:10, MaxAllocBytesPct:5, MaxAllocOpsPct:5}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Regress(base, cur, cfg)
	}
}

func BenchmarkLintYamlLoad(b *testing.B) {
	yaml := "forbidden_imports: [\"unsafe\",\"github.com/vendor/*\"]\nrequire_no_alloc:[\"Parse\",\"Marshal\"]\n"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = LoadStringConfig(yaml)
	}
}

func BenchmarkLintAstPass(b *testing.B) {
	dir := b.TempDir()
	path := dir + "/sample.go"
	os.WriteFile(path, []byte(benchSrc), 0o644)
	cfg := &LintConfig{ForbiddenImports:[]string{"unsafe"}, RequireNoAlloc:[]string{"Filter","Encode"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = LintDir(cfg, dir)
	}
}

func BenchmarkBenchDbStoreAndRead(b *testing.B) {
	tmp := b.TempDir() + "/benchdb"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db,_ := NewBenchDB(tmp+"/run")
		run := BenchRun{Seq:int64(i),Label:"run",Samples:[]BenchSample{{Name:"Test",NsPerOp:1234.0}}}
		_, _ = db.Save(run)
		_, _ = db.Latest()
		_ = db.Len()
	}
}
