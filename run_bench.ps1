cd d:\IdeaProjects\untitled\cloudai-fusion
go test ./pkg/capability -bench="BenchmarkM1_VersusCompetitors_Concurrent128" -benchmem -count=5 -cpu=1,2,4,8,16,32,64,128 > capability\benchmark_results_128.txt 2>&1
cat capability\benchmark_results_128.txt
